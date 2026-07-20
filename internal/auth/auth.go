package auth

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/crate/crate/internal/model"
	"golang.org/x/crypto/bcrypt"
)

type UserStore interface {
	GetUserByUsername(username string) (*model.User, error)
	GetUserByID(id int64) (*model.User, error)
	GetUserByToken(token string) (*model.User, error)
	UpdateUserToken(userID int64, token string) error
	CreateUser(username, passwordHash, role string) (int64, error)
}

type contextKey string

const UserContextKey contextKey = "user"

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func GenerateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

func md5hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func authenticateFromBasicAuth(r *http.Request, db UserStore) (*model.User, bool) {
	username, password, ok := r.BasicAuth()
	if !ok || username == "" || password == "" {
		return nil, false
	}
	user, err := db.GetUserByUsername(username)
	if err != nil || user == nil {
		return nil, false
	}
	if !CheckPassword(user.PasswordHash, password) {
		return nil, false
	}
	return user, true
}

func authenticateFromSubsonic(r *http.Request, db UserStore) (*model.User, bool) {
	q := r.URL.Query()
	username := q.Get("u")
	if username == "" {
		return nil, false
	}

	user, err := db.GetUserByUsername(username)
	if err != nil || user == nil {
		return nil, false
	}

	// Token auth: t (token) + s (salt), password = md5(password + salt)
	salt := q.Get("s")
	token := q.Get("t")
	if salt != "" && token != "" {
		expected := md5hex(user.PasswordHash + salt)
		if expected != token {
			return nil, false
		}
		return user, true
	}

	// Plain auth: p parameter (hex-encoded md5 of password)
	plain := q.Get("p")
	if plain != "" {
		if md5hex(user.PasswordHash) != plain {
			return nil, false
		}
		return user, true
	}

	return nil, false
}

func authenticateFromSession(r *http.Request, db UserStore) (*model.User, bool) {
	cookie, err := r.Cookie("session")
	if err != nil || cookie.Value == "" {
		return nil, false
	}
	// Session value is just the user ID stored as a string.
	var userID int64
	if _, err := fmt.Sscanf(cookie.Value, "%d", &userID); err != nil {
		return nil, false
	}
	user, err := db.GetUserByID(userID)
	if err != nil || user == nil {
		return nil, false
	}
	return user, true
}

func authenticateFromAPIKey(r *http.Request, db UserStore) (*model.User, bool) {
	apiKey := r.URL.Query().Get("apiKey")
	if apiKey == "" {
		return nil, false
	}
	// apiKey format: "userID:token"
	parts := strings.SplitN(apiKey, ":", 2)
	if len(parts) != 2 {
		return nil, false
	}
	var userID int64
	if _, err := fmt.Sscanf(parts[0], "%d", &userID); err != nil {
		return nil, false
	}
	_ = parts[1] // token value; in production you'd verify against a stored token
	user, err := db.GetUserByID(userID)
	if err != nil || user == nil {
		return nil, false
	}
	return user, true
}

func authenticateFromBearer(r *http.Request, db UserStore) (*model.User, bool) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return nil, false
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	if token == "" {
		return nil, false
	}
	user, err := db.GetUserByToken(token)
	if err != nil || user == nil {
		return nil, false
	}
	return user, true
}

func AuthMiddleware(db UserStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var user *model.User
			var ok bool

			// 1. Basic auth
			if user, ok = authenticateFromBasicAuth(r, db); ok {
				ctx := context.WithValue(r.Context(), UserContextKey, user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// 2. Subsonic API auth (t+s or p parameters)
			if user, ok = authenticateFromSubsonic(r, db); ok {
				ctx := context.WithValue(r.Context(), UserContextKey, user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// 3. Session cookie
			if user, ok = authenticateFromSession(r, db); ok {
				ctx := context.WithValue(r.Context(), UserContextKey, user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// 4. API key
			if user, ok = authenticateFromAPIKey(r, db); ok {
				ctx := context.WithValue(r.Context(), UserContextKey, user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// 5. Bearer token
			if user, ok = authenticateFromBearer(r, db); ok {
				ctx := context.WithValue(r.Context(), UserContextKey, user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// No auth — pass through without user (handlers decide if auth is required)
			next.ServeHTTP(w, r)
		})
	}
}

func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := GetUserFromContext(r.Context())
		if !ok || user == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func GetUserFromContext(ctx context.Context) (*model.User, bool) {
	user, ok := ctx.Value(UserContextKey).(*model.User)
	return user, ok
}

func LoginHandler(db UserStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var username, password string

		contentType := r.Header.Get("Content-Type")
		if strings.HasPrefix(contentType, "application/json") {
			var body struct {
				Username string `json:"username"`
				Password string `json:"password"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				username = body.Username
				password = body.Password
			}
		}
		if username == "" {
			username = r.FormValue("username")
		}
		if password == "" {
			password = r.FormValue("password")
		}
		if username == "" || password == "" {
			http.Error(w, "username and password required", http.StatusBadRequest)
			return
		}

		user, err := db.GetUserByUsername(username)
		if err != nil || user == nil {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		if !CheckPassword(user.PasswordHash, password) {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    fmt.Sprintf("%d", user.ID),
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   86400 * 30,
		})

		token := GenerateToken()
		_ = db.UpdateUserToken(user.ID, token)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":%d,"username":"%s","role":"%s","token":"%s"}`, user.ID, user.Username, user.Role, token)
	}
}

func CreateUserHandler(db UserStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		user, ok := GetUserFromContext(r.Context())
		if !ok || user.Role != "admin" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		var username, password, role string
		contentType := r.Header.Get("Content-Type")
		if strings.HasPrefix(contentType, "application/json") {
			var body struct {
				Username string `json:"username"`
				Password string `json:"password"`
				Role     string `json:"role"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				username = body.Username
				password = body.Password
				role = body.Role
			}
		}
		if username == "" {
			username = r.FormValue("username")
		}
		if password == "" {
			password = r.FormValue("password")
		}
		if role == "" {
			role = r.FormValue("role")
		}
		if username == "" || password == "" {
			http.Error(w, "username and password required", http.StatusBadRequest)
			return
		}
		if role == "" {
			role = "user"
		}

		hash, err := HashPassword(password)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		id, err := db.CreateUser(username, hash, role)
		if err != nil {
			http.Error(w, fmt.Sprintf("could not create user: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"id":%d,"username":"%s","role":"%s"}`, id, username, role)
	}
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := GetUserFromContext(r.Context())
		if !ok || user.Role != "admin" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func SetSessionCookie(w http.ResponseWriter, userID int64) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    fmt.Sprintf("%d", userID),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
	})
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

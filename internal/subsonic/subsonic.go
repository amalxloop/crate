package subsonic

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/crate/crate/internal/auth"
	"github.com/crate/crate/internal/db"
	"github.com/crate/crate/internal/model"
	"github.com/crate/crate/internal/scanner"
	"github.com/crate/crate/internal/transcode"
	"github.com/gorilla/mux"
)

type API struct {
	db    *db.DB
	sc    *scanner.Scanner
	tc    *transcode.Transcoder
	base  string
}

type subsonicResponse struct {
	XMLName  xml.Name    `xml:"subsonic-response" json:"-"`
	Response interface{} `xml:"children" json:"subsonic-response"`
}

type subsonicJSONResponse struct {
	Status    string      `json:"status"`
	Version   string      `json:"version"`
	Type      string      `json:"serverType,omitempty"`
	Children  interface{} `json:"children,omitempty"`
	Error     *subsonicError `json:"error,omitempty"`
}

type subsonicError struct {
	Code    int    `xml:"code,attr" json:"code"`
	Message string `xml:"message,attr" json:"message"`
}

type artistsID3 struct {
	Index []artistIndex `xml:"index" json:"index"`
}

type artistIndex struct {
	Name    string          `xml:"name,attr" json:"name"`
	Artists []artistID3Item `xml:"artist" json:"artist"`
}

type artistID3Item struct {
	ID       int64  `xml:"id,attr" json:"id"`
	Name     string `xml:"name,attr" json:"name"`
	AlbumCount int  `xml:"albumCount,attr" json:"albumCount"`
}

type albumID3 struct {
	ID        int64  `xml:"id,attr" json:"id"`
	ArtistID  int64  `xml:"artistId,attr" json:"artistId"`
	Title     string `xml:"name,attr" json:"name"`
	Year      int    `xml:"year,attr" json:"year"`
	CoverArt  string `xml:"coverArt,attr" json:"coverArt,omitempty"`
}

type childID3 struct {
	ID          int64  `xml:"id,attr" json:"id"`
	ParentID    int64  `xml:"parent,attr" json:"parent,omitempty"`
	Title       string `xml:"title,attr" json:"title"`
	Track       int    `xml:"track,attr" json:"track,omitempty"`
	Duration    int    `xml:"duration,attr" json:"duration,omitempty"`
	Artist      string `xml:"artist,attr" json:"artist,omitempty"`
	Album       string `xml:"album,attr" json:"album,omitempty"`
	AlbumID     int64  `xml:"albumId,attr" json:"albumId,omitempty"`
	ContentType string `xml:"contentType,attr" json:"contentType,omitempty"`
	CoverArt    string `xml:"coverArt,attr" json:"coverArt,omitempty"`
	Year        int    `xml:"year,attr" json:"year,omitempty"`
	Genre       string `xml:"genre,attr" json:"genre,omitempty"`
}

func New(database *db.DB, sc *scanner.Scanner, tc *transcode.Transcoder, baseURL string) *API {
	return &API{
		db:   database,
		sc:   sc,
		tc:   tc,
		base: baseURL,
	}
}

func (a *API) Handle(w http.ResponseWriter, r *http.Request) {
	action := mux.Vars(r)["action"]
	if action == "" {
		action = r.URL.Query().Get("f")
	}

	user, _ := auth.GetUserFromContext(r.Context())
	format := r.URL.Query().Get("f")
	if format == "" {
		format = "json"
	}

	switch action {
	case "ping":
		a.writeResponse(w, format, map[string]interface{}{})
	case "getartists":
		a.handleGetArtists(w, r, format)
	case "getalbumlist", "getalbumlist2":
		a.handleGetAlbumList(w, r, format)
	case "getalbum":
		a.handleGetAlbum(w, r, format)
	case "getsong":
		a.handleGetSong(w, r, format)
	case "search2", "search3":
		a.handleSearch(w, r, format)
	case "stream":
		a.handleStream(w, r)
	case "download":
		a.handleDownload(w, r)
	case "getcoverart":
		a.handleGetCoverArt(w, r)
	case "scrobble":
		a.handleScrobble(w, r, format, user)
	case "star":
		a.handleStar(w, r, format, user)
	case "unstar":
		a.handleUnstar(w, r, format, user)
	case "getplaylists":
		a.handleGetPlaylists(w, r, format, user)
	case "createplaylist":
		a.handleCreatePlaylist(w, r, format, user)
	case "deleteplaylist":
		a.handleDeletePlaylist(w, r, format)
	case "getplaylist":
		a.handleGetPlaylist(w, r, format)
	default:
		a.writeError(w, format, 0, fmt.Sprintf("Unknown action: %s", action))
	}
}

func (a *API) writeResponse(w http.ResponseWriter, format string, content interface{}) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"subsonic-response": map[string]interface{}{
			"status":  "ok",
			"version": "1.16.1",
			"type":    "crate",
			"children": content,
		},
	}
	json.NewEncoder(w).Encode(resp)
}

func (a *API) writeError(w http.ResponseWriter, format string, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"subsonic-response": map[string]interface{}{
			"status":  "failed",
			"version": "1.16.1",
			"error": map[string]interface{}{
				"code":    code,
				"message": message,
			},
		},
	}
	json.NewEncoder(w).Encode(resp)
}

func (a *API) handleGetArtists(w http.ResponseWriter, r *http.Request, format string) {
	artists, err := a.db.GetAllArtists()
	if err != nil {
		a.writeError(w, format, 0, err.Error())
		return
	}

	indexMap := make(map[string][]artistID3Item)
	for _, art := range artists {
		initial := strings.ToUpper(string([]rune(art.Name)[0:1]))
		if initial == "" {
			initial = "#"
		}
		indexMap[initial] = append(indexMap[initial], artistID3Item{
			ID:       art.ID,
			Name:     art.Name,
		})
	}

	var indices []artistIndex
	for name, items := range indexMap {
		indices = append(indices, artistIndex{Name: name, Artists: items})
	}

	a.writeResponse(w, format, artistsID3{Index: indices})
}

func (a *API) handleGetAlbumList(w http.ResponseWriter, r *http.Request, format string) {
	albums, err := a.db.GetAllAlbums()
	if err != nil {
		a.writeError(w, format, 0, err.Error())
		return
	}

	var albumList []albumID3
	for _, al := range albums {
		albumList = append(albumList, albumID3{
			ID:       al.ID,
			ArtistID: al.ArtistID,
			Title:    al.Title,
			Year:     al.Year,
		})
	}

	a.writeResponse(w, format, map[string]interface{}{
		"album": albumList,
	})
}

func (a *API) handleGetAlbum(w http.ResponseWriter, r *http.Request, format string) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.writeError(w, format, 0, "missing or invalid id")
		return
	}

	al, err := a.db.GetAlbumByID(id)
	if err != nil {
		a.writeError(w, format, 0, err.Error())
		return
	}

	tracks, err := a.db.GetTracksByAlbum(id)
	if err != nil {
		a.writeError(w, format, 0, err.Error())
		return
	}

	artist, _ := a.db.GetArtistByID(al.ArtistID)
	artistName := ""
	if artist != nil {
		artistName = artist.Name
	}

	var children []childID3
	for _, t := range tracks {
		children = append(children, childID3{
			ID:          t.ID,
			ParentID:    id,
			Title:       t.Title,
			Track:       t.TrackNumber,
			Duration:    t.Duration,
			Artist:      artistName,
			Album:       al.Title,
			AlbumID:     al.ID,
			ContentType: mimeForFormat(t.Format),
			CoverArt:    fmt.Sprintf("%d", id),
			Year:        t.Year,
			Genre:       t.Genre,
		})
	}

	a.writeResponse(w, format, map[string]interface{}{
		"id":          al.ID,
		"name":        al.Title,
		"artist":      artistName,
		"artistId":    al.ArtistID,
		"year":        al.Year,
		"coverArt":    fmt.Sprintf("%d", al.ID),
		"song":        children,
	})
}

func (a *API) handleGetSong(w http.ResponseWriter, r *http.Request, format string) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.writeError(w, format, 0, "missing or invalid id")
		return
	}

	t, err := a.db.GetTrackByID(id)
	if err != nil {
		a.writeError(w, format, 0, err.Error())
		return
	}

	a.writeResponse(w, format, childID3{
		ID:          t.ID,
		Title:       t.Title,
		Track:       t.TrackNumber,
		Duration:    t.Duration,
		AlbumID:     t.AlbumID,
		ContentType: mimeForFormat(t.Format),
		Year:        t.Year,
		Genre:       t.Genre,
	})
}

func (a *API) handleSearch(w http.ResponseWriter, r *http.Request, format string) {
	query := r.URL.Query().Get("query")
	if query == "" {
		a.writeError(w, format, 0, "missing query parameter")
		return
	}

	limit := 20
	if l, err := strconv.Atoi(r.URL.Query().Get("artistCount")); err == nil {
		limit = l
	}

	artists, _ := a.db.SearchArtists(query, limit)
	albums, _ := a.db.SearchAlbums(query, limit)
	tracks, _ := a.db.SearchTracks(query, limit)

	a.writeResponse(w, format, map[string]interface{}{
		"artist": artists,
		"album":  albums,
		"song":   tracks,
	})
}

func (a *API) handleStream(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	t, err := a.db.GetTrackByID(id)
	if err != nil {
		http.Error(w, "track not found", http.StatusNotFound)
		return
	}

	estFmt := "mp3"
	if bitrate := r.URL.Query().Get("maxBitRate"); bitrate != "" {
		if maxBR, err := strconv.Atoi(bitrate); err == nil && maxBR > 0 {
			reader, err := a.tc.Transcode(r.Context(), t.FilePath, estFmt, maxBR)
			if err != nil {
				http.Error(w, "transcode error", http.StatusInternalServerError)
				return
			}
			defer reader.Close()
			w.Header().Set("Content-Type", "audio/mpeg")
			io.Copy(w, reader)
			return
		}
	}

	f, err := os.Open(t.FilePath)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "stat error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", mimeForFormat(t.Format))
	http.ServeContent(w, r, t.FilePath, stat.ModTime(), f)
}

func (a *API) handleDownload(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	t, err := a.db.GetTrackByID(id)
	if err != nil {
		http.Error(w, "track not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(t.FilePath)))
	http.ServeFile(w, r, t.FilePath)
}

func (a *API) handleGetCoverArt(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	al, err := a.db.GetAlbumByID(id)
	if err != nil || al.CoverArtPath == "" {
		http.Error(w, "cover art not found", http.StatusNotFound)
		return
	}

	http.ServeFile(w, r, al.CoverArtPath)
}

func (a *API) handleScrobble(w http.ResponseWriter, r *http.Request, format string, user *model.User) {
	if user == nil {
		a.writeError(w, format, 10, "not authenticated")
		return
	}
	a.writeResponse(w, format, map[string]interface{}{})
}

func (a *API) handleStar(w http.ResponseWriter, r *http.Request, format string, user *model.User) {
	if user == nil {
		a.writeError(w, format, 10, "not authenticated")
		return
	}
	idStr := r.URL.Query().Get("id")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	if err := a.db.SetFavorite(user.ID, "track", id); err != nil {
		a.writeError(w, format, 0, err.Error())
		return
	}
	a.writeResponse(w, format, map[string]interface{}{})
}

func (a *API) handleUnstar(w http.ResponseWriter, r *http.Request, format string, user *model.User) {
	if user == nil {
		a.writeError(w, format, 10, "not authenticated")
		return
	}
	idStr := r.URL.Query().Get("id")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	if err := a.db.RemoveFavorite(user.ID, "track", id); err != nil {
		a.writeError(w, format, 0, err.Error())
		return
	}
	a.writeResponse(w, format, map[string]interface{}{})
}

func (a *API) handleGetPlaylists(w http.ResponseWriter, r *http.Request, format string, user *model.User) {
	if user == nil {
		a.writeError(w, format, 10, "not authenticated")
		return
	}
	playlists, err := a.db.GetUserPlaylists(user.ID)
	if err != nil {
		a.writeError(w, format, 0, err.Error())
		return
	}
	a.writeResponse(w, format, map[string]interface{}{
		"playlist": playlists,
	})
}

func (a *API) handleCreatePlaylist(w http.ResponseWriter, r *http.Request, format string, user *model.User) {
	if user == nil {
		a.writeError(w, format, 10, "not authenticated")
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		a.writeError(w, format, 0, "missing playlist name")
		return
	}
	_, err := a.db.CreatePlaylist(user.ID, name, "", false, false)
	if err != nil {
		a.writeError(w, format, 0, err.Error())
		return
	}
	a.writeResponse(w, format, map[string]interface{}{})
}

func (a *API) handleDeletePlaylist(w http.ResponseWriter, r *http.Request, format string) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.writeError(w, format, 0, "missing or invalid id")
		return
	}
	if err := a.db.DeletePlaylist(id); err != nil {
		a.writeError(w, format, 0, err.Error())
		return
	}
	a.writeResponse(w, format, map[string]interface{}{})
}

func (a *API) handleGetPlaylist(w http.ResponseWriter, r *http.Request, format string) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.writeError(w, format, 0, "missing or invalid id")
		return
	}
	p, err := a.db.GetPlaylistByID(id)
	if err != nil {
		a.writeError(w, format, 0, err.Error())
		return
	}
	tracks, err := a.db.GetPlaylistTracks(id)
	if err != nil {
		a.writeError(w, format, 0, err.Error())
		return
	}
	a.writeResponse(w, format, map[string]interface{}{
		"id":          p.ID,
		"name":        p.Name,
		"description": p.Description,
		"song":        tracks,
	})
}

func mimeForFormat(format string) string {
	switch strings.ToLower(format) {
	case "mp3":
		return "audio/mpeg"
	case "flac":
		return "audio/flac"
	case "ogg":
		return "audio/ogg"
	case "opus":
		return "audio/opus"
	case "aac", "m4a":
		return "audio/aac"
	case "wav":
		return "audio/wav"
	case "alac":
		return "audio/mp4"
	default:
		return "audio/mpeg"
	}
}

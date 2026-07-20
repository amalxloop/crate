package scrobble

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/crate/crate/internal/config"
)

const (
	lastfmAPIBase    = "https://ws.audioscrobbler.com/2.0/"
	listenbrainzAPI  = "https://api.listenbrainz.org/1/submit-listens"
	authCallbackPath = "/api/scrobble/lastfm/callback"
)

type Scrobbler struct {
	lastfm       *LastFMClient
	listenbrainz *ListenBrainzClient
}

type LastFMClient struct {
	apiKey    string
	apiSecret string
	enabled   bool
	sessions  map[int64]string
	mu        sync.RWMutex
}

type ListenBrainzClient struct {
	baseURL string
	enabled bool
	tokens  map[int64]string
	mu      sync.RWMutex
}

func New(cfg config.ScrobbleConfig) *Scrobbler {
	s := &Scrobbler{
		lastfm: &LastFMClient{
			apiKey:    cfg.LastFM.APIKey,
			apiSecret: cfg.LastFM.APISecret,
			enabled:   cfg.LastFM.Enabled,
			sessions:  make(map[int64]string),
		},
		listenbrainz: &ListenBrainzClient{
			baseURL: cfg.ListenBrainz.BaseURL,
			enabled: cfg.ListenBrainz.Enabled,
			tokens:  make(map[int64]string),
		},
	}
	if s.listenbrainz.baseURL == "" {
		s.listenbrainz.baseURL = listenbrainzAPI
	}
	return s
}

func (s *Scrobbler) Scrobble(userID int64, artist, track, album string, duration, timestamp int64) {
	if s.lastfm.enabled {
		go s.scrobbleLastFM(userID, artist, track, album, duration, timestamp)
	}
	if s.listenbrainz.enabled {
		go s.scrobbleListenBrainz(userID, artist, track, album, duration, timestamp)
	}
}

func (s *Scrobbler) NowPlaying(userID int64, artist, track, album string, duration int64) {
	if s.lastfm.enabled {
		go s.nowPlayingLastFM(userID, artist, track, album, duration)
	}
	if s.listenbrainz.enabled {
		go s.nowPlayingListenBrainz(userID, artist, track, album)
	}
}

func (s *Scrobbler) SetLastFMSession(userID int64, sessionKey string) {
	s.lastfm.mu.Lock()
	defer s.lastfm.mu.Unlock()
	s.lastfm.sessions[userID] = sessionKey
}

func (s *Scrobbler) GetLastFMSession(userID int64) string {
	s.lastfm.mu.RLock()
	defer s.lastfm.mu.RUnlock()
	return s.lastfm.sessions[userID]
}

func (s *Scrobbler) SetListenBrainzToken(userID int64, token string) {
	s.listenbrainz.mu.Lock()
	defer s.listenbrainz.mu.Unlock()
	s.listenbrainz.tokens[userID] = token
}

func (s *Scrobbler) GetListenBrainzToken(userID int64) string {
	s.listenbrainz.mu.RLock()
	defer s.listenbrainz.mu.RUnlock()
	return s.listenbrainz.tokens[userID]
}

func (s *Scrobbler) IsLastFMEnabled() bool {
	return s.lastfm.enabled
}

func (s *Scrobbler) IsListenBrainzEnabled() bool {
	return s.listenbrainz.enabled
}

func (s *Scrobbler) scrobbleLastFM(userID int64, artist, track, album string, duration, timestamp int64) {
	session := s.GetLastFMSession(userID)
	if session == "" {
		log.Printf("scrobble: no Last.fm session for user %d", userID)
		return
	}

	params := url.Values{}
	params.Set("method", "track.scrobble")
	params.Set("api_key", s.lastfm.apiKey)
	params.Set("sk", session)
	params.Set("artist", artist)
	params.Set("track", track)
	params.Set("timestamp", strconv.FormatInt(timestamp, 10))
	if album != "" {
		params.Set("album", album)
	}
	if duration > 0 {
		params.Set("duration", strconv.FormatInt(duration, 10))
	}

	apiSig := s.lastfmSign(params)
	params.Set("api_sig", apiSig)
	params.Set("format", "json")

	resp, err := http.PostForm(lastfmAPIBase, params)
	if err != nil {
		log.Printf("scrobble: last.fm request failed for user %d: %v", userID, err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("scrobble: last.fm returned %d for user %d: %s", resp.StatusCode, userID, string(body))
		return
	}

	var result struct {
		Scrobbles struct {
			Scrobble struct {
				AcceptedMessages struct {
					IgnoredMessage struct {
						Code int    `json:"code"`
						Text string `json:"#text"`
					} `json:"ignoredMessage"`
				} `json:"acceptedMessages"`
			} `json:"@attr"`
		} `json:"scrobbles"`
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("scrobble: failed to parse last.fm response for user %d: %v", userID, err)
		return
	}
	if result.Error != 0 {
		log.Printf("scrobble: last.fm error for user %d: %d - %s", userID, result.Error, result.Message)
		return
	}

	log.Printf("scrobble: last.fm scrobble accepted for user %d: %s - %s", userID, artist, track)
}

func (s *Scrobbler) nowPlayingLastFM(userID int64, artist, track, album string, duration int64) {
	session := s.GetLastFMSession(userID)
	if session == "" {
		return
	}

	params := url.Values{}
	params.Set("method", "track.updateNowPlaying")
	params.Set("api_key", s.lastfm.apiKey)
	params.Set("sk", session)
	params.Set("artist", artist)
	params.Set("track", track)
	if album != "" {
		params.Set("album", album)
	}
	if duration > 0 {
		params.Set("duration", strconv.FormatInt(duration, 10))
	}

	apiSig := s.lastfmSign(params)
	params.Set("api_sig", apiSig)
	params.Set("format", "json")

	resp, err := http.PostForm(lastfmAPIBase, params)
	if err != nil {
		log.Printf("nowplaying: last.fm request failed for user %d: %v", userID, err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("nowplaying: last.fm returned %d for user %d: %s", resp.StatusCode, userID, string(body))
	}
}

func (s *Scrobbler) lastfmSign(params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "format" || k == "callback" || k == "api_sig" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf strings.Builder
	buf.WriteString(s.lastfm.apiKey)
	for _, k := range keys {
		buf.WriteString(k)
		buf.WriteString(params.Get(k))
	}
	buf.WriteString(s.lastfm.apiSecret)

	h := md5.Sum([]byte(buf.String()))
	return hex.EncodeToString(h[:])
}

func (s *Scrobbler) scrobbleListenBrainz(userID int64, artist, track, album string, duration, timestamp int64) {
	token := s.GetListenBrainzToken(userID)
	if token == "" {
		log.Printf("scrobble: no ListenBrainz token for user %d", userID)
		return
	}

	payload := map[string]interface{}{
		"listen_type": "single",
		"payload": []map[string]interface{}{
			{
				"listened_at": timestamp,
				"track_metadata": map[string]string{
					"artist_name":  artist,
					"track_name":   track,
					"release_name": album,
				},
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("scrobble: listenbrainz marshal failed for user %d: %v", userID, err)
		return
	}

	apiURL := s.listenbrainz.baseURL
	if apiURL == "" {
		apiURL = listenbrainzAPI
	}

	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(data))
	if err != nil {
		log.Printf("scrobble: listenbrainz request failed for user %d: %v", userID, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("scrobble: listenbrainz request failed for user %d: %v", userID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("scrobble: listenbrainz returned %d for user %d: %s", resp.StatusCode, userID, string(body))
		return
	}

	log.Printf("scrobble: listenbrainz scrobble submitted for user %d: %s - %s", userID, artist, track)
}

func (s *Scrobbler) nowPlayingListenBrainz(userID int64, artist, track, album string) {
	token := s.GetListenBrainzToken(userID)
	if token == "" {
		return
	}

	payload := map[string]interface{}{
		"listen_type": "playing_now",
		"payload": []map[string]interface{}{
			{
				"track_metadata": map[string]string{
					"artist_name":  artist,
					"track_name":   track,
					"release_name": album,
				},
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	apiURL := s.listenbrainz.baseURL
	if apiURL == "" {
		apiURL = listenbrainzAPI
	}

	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("nowplaying: listenbrainz request failed for user %d: %v", userID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("nowplaying: listenbrainz returned %d for user %d: %s", resp.StatusCode, userID, string(body))
	}
}

func (s *Scrobbler) LastfmAuthURL(callbackURL string) string {
	params := url.Values{}
	params.Set("api_key", s.lastfm.apiKey)
	params.Set("cb", callbackURL)
	return "https://www.last.fm/api/auth/?" + params.Encode()
}

func (s *Scrobbler) LastfmGetSession(token string) (string, error) {
	params := url.Values{}
	params.Set("method", "auth.getSession")
	params.Set("api_key", s.lastfm.apiKey)
	params.Set("token", token)

	apiSig := s.lastfmSign(params)
	params.Set("api_sig", apiSig)
	params.Set("format", "json")

	resp, err := http.PostForm(lastfmAPIBase, params)
	if err != nil {
		return "", fmt.Errorf("last.fm auth.getSession request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read last.fm response: %w", err)
	}

	var result struct {
		Session struct {
			Name       string `json:"name"`
			Subscriber int    `json:"subscriber"`
			Key        string `json:"key"`
		} `json:"session"`
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse last.fm response: %w", err)
	}
	if result.Error != 0 {
		return "", fmt.Errorf("last.fm error %d: %s", result.Error, result.Message)
	}

	return result.Session.Key, nil
}

func (s *Scrobbler) LastfmRequestToken() (string, error) {
	params := url.Values{}
	params.Set("method", "auth.getToken")
	params.Set("api_key", s.lastfm.apiKey)

	apiSig := s.lastfmSign(params)
	params.Set("api_sig", apiSig)
	params.Set("format", "json")

	resp, err := http.PostForm(lastfmAPIBase, params)
	if err != nil {
		return "", fmt.Errorf("last.fm auth.getToken request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read last.fm response: %w", err)
	}

	var result struct {
		Token string `json:"token"`
		Error int    `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse last.fm response: %w", err)
	}
	if result.Error != 0 {
		return "", fmt.Errorf("last.fm error %d", result.Error)
	}

	return result.Token, nil
}

func (s *Scrobbler) LastfmAuthCallbackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "missing token parameter", http.StatusBadRequest)
			return
		}

		sessionKey, err := s.LastfmGetSession(token)
		if err != nil {
			log.Printf("lastfm callback: failed to get session: %v", err)
			http.Error(w, "failed to authenticate with Last.fm", http.StatusInternalServerError)
			return
		}

		userIDStr := r.URL.Query().Get("user_id")
		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid user_id", http.StatusBadRequest)
			return
		}

		s.SetLastFMSession(userID, sessionKey)
		log.Printf("lastfm callback: session set for user %d", userID)

		http.Redirect(w, r, "/settings?lastfm=connected", http.StatusSeeOther)
	}
}

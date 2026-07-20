package scanner

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dhowden/tag"
	"github.com/crate/crate/internal/model"
	"github.com/fsnotify/fsnotify"
)

var audioExtensions = map[string]bool{
	".flac": true,
	".mp3":  true,
	".m4a":  true,
	".aac":  true,
	".ogg":  true,
	".opus": true,
	".wav":  true,
	".alac": true,
}

type Store interface {
	GetAllTrackPaths() (map[string]time.Time, error)
	GetOrCreateArtist(name, sortName, mbid string) (int64, error)
	GetOrCreateAlbum(artistID int64, title, sortTitle, mbid string, year int, coverArt string) (int64, error)
	UpsertTrack(t *model.Track) error
	RemoveTrackByPath(path string) error
	SetScanInProgress(inProgress bool, filesFound, filesIndexed int) error
	SetScanComplete(filesIndexed int) error
	CleanupOrphans() (int64, error)
}

type Scanner struct {
	store       Store
	dirs        []string
	coverDir    string
	interval    time.Duration
	stopCh      chan struct{}
	doneCh      chan struct{}
	mu          sync.Mutex
	inProgress  bool
	filesFound  int
	filesIndexed int
	watcher     *fsnotify.Watcher
}

type ScanResult struct {
	FilesFound   int
	FilesIndexed int
	FilesSkipped int
	Errors       []string
	Duration     time.Duration
}

func New(store Store, dirs []string, coverDir string, interval time.Duration) *Scanner {
	return &Scanner{
		store:    store,
		dirs:     dirs,
		coverDir: coverDir,
		interval: interval,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

func (s *Scanner) Start() {
	go s.run()
	s.startWatcher()
}

func (s *Scanner) Stop() {
	s.mu.Lock()
	if !s.inProgress {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	close(s.stopCh)
	<-s.doneCh
	s.stopWatcher()
}

func (s *Scanner) IsScanning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inProgress
}

func (s *Scanner) TriggerScan() {
	go s.scan()
}

func (s *Scanner) ScanDirectories() []string {
	return s.dirs
}

func (s *Scanner) run() {
	defer close(s.doneCh)

	if s.interval > 0 {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		s.scan()

		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.scan()
			}
		}
	} else {
		s.scan()
	}
}

func (s *Scanner) scan() {
	s.mu.Lock()
	if s.inProgress {
		s.mu.Unlock()
		return
	}
	s.inProgress = true
	s.mu.Unlock()

	result := s.executeScan()

	s.mu.Lock()
	s.inProgress = false
	s.mu.Unlock()

	if err := s.store.SetScanComplete(result.FilesIndexed); err != nil {
		log.Printf("scanner: failed to mark scan complete: %v", err)
	}

	if n, err := s.store.CleanupOrphans(); err != nil {
		log.Printf("scanner: cleanup orphans failed: %v", err)
	} else if n > 0 {
		log.Printf("scanner: cleaned up %d orphaned playlist entries", n)
	}

	log.Printf("scanner: scan completed in %s - found: %d, indexed: %d, skipped: %d, errors: %d",
		result.Duration.Round(time.Millisecond), result.FilesFound, result.FilesIndexed,
		result.FilesSkipped, len(result.Errors))

	for _, e := range result.Errors {
		log.Printf("scanner: error - %s", e)
	}
}

func (s *Scanner) executeScan() ScanResult {
	start := time.Now()
	result := ScanResult{}

	existingPaths, err := s.store.GetAllTrackPaths()
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to get existing tracks: %v", err))
		return result
	}

	var allFiles []string
	for _, dir := range s.dirs {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("walk error for %s: %v", path, err))
				return nil
			}
			if info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if audioExtensions[ext] {
				allFiles = append(allFiles, path)
			}
			return nil
		})
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to walk directory %s: %v", dir, err))
		}
	}

	result.FilesFound = len(allFiles)
	if err := s.store.SetScanInProgress(true, result.FilesFound, 0); err != nil {
		log.Printf("scanner: failed to set scan in progress: %v", err)
	}

	indexed := 0
	for _, path := range allFiles {
		select {
		case <-s.stopCh:
			result.Errors = append(result.Errors, "scan interrupted")
			return result
		default:
		}

		info, err := os.Stat(path)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("stat error for %s: %v", path, err))
			continue
		}

		if existingModTime, exists := existingPaths[path]; exists {
			if !info.ModTime().After(existingModTime) {
				indexed++
				continue
			}
		}

		track, err := s.parseFile(path, info)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("parse error for %s: %v", path, err))
			result.FilesSkipped++
			continue
		}

		// Fallback: extract artist/album from directory structure when tags are empty
		if track.artist == "" || track.album == "" {
			s.extractMetadataFromPath(path, track)
		}

		artistID, err := s.store.GetOrCreateArtist(
			track.artist,
			track.artistSort,
			track.mbid,
		)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("artist error for %s: %v", path, err))
			continue
		}

		coverArtPath := ""
		if track.coverArt != nil {
			coverArtPath, err = s.extractCoverArt(path, track.coverArt)
			if err != nil {
				log.Printf("scanner: warning - failed to extract cover art for %s: %v", path, err)
			}
		}

		// Fallback: fetch thumbnail from YouTube if no embedded cover
		if coverArtPath == "" && track.title != "" && track.artist != "Unknown Artist" {
			thumbData := fetchThumbnailBySearch(track.artist + " " + track.title)
			if thumbData != nil {
				coverArtPath, err = s.extractCoverArt(path, thumbData)
				if err != nil {
					log.Printf("scanner: warning - failed to save youtube cover for %s: %v", path, err)
				}
			}
		}

		albumID, err := s.store.GetOrCreateAlbum(
			artistID,
			track.album,
			track.albumSort,
			track.albumMBID,
			track.year,
			coverArtPath,
		)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("album error for %s: %v", path, err))
			continue
		}

		dbTrack := &model.Track{
			AlbumID:     albumID,
			ArtistID:    artistID,
			Title:       track.title,
			TrackNumber: track.trackNum,
			DiscNumber:  track.discNum,
			Duration:    track.duration,
			Year:        track.year,
			Genre:       track.genre,
			Format:      track.format,
			BitRate:     track.bitRate,
			FilePath:    path,
			FileSize:    info.Size(),
			ModTime:     info.ModTime(),
			MBID:        track.mbid,
			ReplayGain:  track.replayGain,
		}

		if err := s.store.UpsertTrack(dbTrack); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("upsert error for %s: %v", path, err))
			continue
		}

		indexed++
		result.FilesIndexed++
		if indexed%100 == 0 {
			log.Printf("scanner: progress %d/%d files", indexed, result.FilesFound)
			if err := s.store.SetScanInProgress(true, result.FilesFound, indexed); err != nil {
				log.Printf("scanner: failed to update progress: %v", err)
			}
		}
	}

	result.Duration = time.Since(start)
	return result
}

type audioMetadata struct {
	title      string
	artist     string
	artistSort string
	album      string
	albumSort  string
	genre      string
	year       int
	trackNum   int
	discNum    int
	mbid       string
	albumMBID  string
	format     string
	bitRate    int
	duration   int
	coverArt   []byte
	replayGain float64
}

func (s *Scanner) parseFile(path string, info os.FileInfo) (*audioMetadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		if err == tag.ErrNoTagsFound {
			return s.parseFileWithoutTags(path, info)
		}
		return nil, fmt.Errorf("read tags: %w", err)
	}

	meta := &audioMetadata{
		title:      m.Title(),
		artist:     m.Artist(),
		artistSort: m.AlbumArtist(),
		album:      m.Album(),
		albumSort:  m.Album(),
		genre:      m.Genre(),
		year:       m.Year(),
		format:     string(m.FileType()),
	}

	if meta.artistSort == "" {
		meta.artistSort = meta.artist
	}
	if meta.albumSort == "" {
		meta.albumSort = meta.album
	}

	trackNum, _ := m.Track()
	discNum, _ := m.Disc()
	meta.trackNum = trackNum
	meta.discNum = discNum

	raw := m.Raw()
	meta.mbid = extractMusicBrainzID(raw)

	if pic := m.Picture(); pic != nil {
		meta.coverArt = pic.Data
	}

	meta.bitRate = estimateBitRate(path, info.Size(), meta.format)
	meta.duration = probeDuration(path)
	if meta.duration == 0 {
		meta.duration = estimateDuration(path, info.Size(), meta.bitRate)
	}

	extractReplayGain(raw, meta)

	return meta, nil
}

func (s *Scanner) parseFileWithoutTags(path string, info os.FileInfo) (*audioMetadata, error) {
	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(path), "."))
	if ext == "" {
		ext = "UNKNOWN"
	}

	return &audioMetadata{
		format:   ext,
		bitRate:  estimateBitRate(path, info.Size(), ext),
		duration: probeDurationOrEstimate(path, info.Size(), ext),
	}, nil
}

func extractMusicBrainzID(raw map[string]interface{}) string {
	if raw == nil {
		return ""
	}

	if v, ok := raw["UFID"]; ok {
		if ufid, ok := v.(*tag.UFID); ok {
			if strings.Contains(ufid.Provider, "musicbrainz") {
				return string(ufid.Identifier)
			}
		}
	}

	if v, ok := raw["TXXX:MUSICBRAINZ_TRACKID"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}

	for _, key := range []string{"musicbrainz_trackid", "musicbrainz_albumid"} {
		if v, ok := raw[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}

	return ""
}

func extractReplayGain(raw map[string]interface{}, meta *audioMetadata) {
	if raw == nil {
		return
	}

	trackGainKeys := []string{
		"replaygain_track_gain",
		"REPLAYGAIN_TRACK_GAIN",
		"TXXX:REPLAYGAIN_TRACK_GAIN",
	}

	for _, key := range trackGainKeys {
		if v, ok := raw[key]; ok {
			if s, ok := v.(string); ok {
				var gain float64
				if _, err := fmt.Sscanf(s, "%f dB", &gain); err == nil {
					meta.replayGain = gain
					return
				}
				if _, err := fmt.Sscanf(s, "%f", &gain); err == nil {
					meta.replayGain = gain
					return
				}
			}
		}
	}
}

func estimateBitRate(path string, size int64, format string) int {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".flac":
		if size > 0 {
			return 1000
		}
	case ".mp3":
		return 320
	case ".m4a", ".aac":
		return 256
	case ".ogg", ".opus":
		return 192
	case ".wav":
		return 1411
	case ".alac":
		return 1000
	}
	return 320
}

func estimateDuration(path string, size int64, bitRate int) int {
	if bitRate > 0 {
		return int((size * 8) / int64(bitRate*1000))
	}
	return 0
}

func probeDuration(path string) int {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0
	}
	out, err := exec.Command(ffprobePath, "-v", "quiet", "-show_entries", "format=duration", "-of", "csv=p=0", path).Output()
	if err != nil {
		return 0
	}
	var seconds float64
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &seconds); err != nil {
		return 0
	}
	return int(seconds)
}

func probeDurationOrEstimate(path string, size int64, format string) int {
	if d := probeDuration(path); d > 0 {
		return d
	}
	return estimateDuration(path, size, estimateBitRate(path, size, format))
}

func (s *Scanner) extractCoverArt(filePath string, data []byte) (string, error) {
	if err := os.MkdirAll(s.coverDir, 0755); err != nil {
		return "", fmt.Errorf("create cover dir: %w", err)
	}

	ext := ".jpg"
	if len(data) > 3 && data[0] == 0x89 && data[1] == 0x50 {
		ext = ".png"
	}

	hash := fmt.Sprintf("%x", md5.Sum([]byte(filePath)))
	coverPath := filepath.Join(s.coverDir, hash+ext)

	if _, err := os.Stat(coverPath); err == nil {
		return coverPath, nil
	}

	tmpPath := coverPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return "", fmt.Errorf("write cover art: %w", err)
	}

	if err := os.Rename(tmpPath, coverPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("rename cover art: %w", err)
	}

	return coverPath, nil
}

func (s *Scanner) SetCoverDir(dir string) {
	s.coverDir = dir
}

func (s *Scanner) SetDirs(dirs []string) {
	s.dirs = dirs
}

func (s *Scanner) extractMetadataFromPath(path string, meta *audioMetadata) {
	// Try to extract YouTube video ID from filename and fetch metadata
	if vid := extractYouTubeVideoID(path); vid != "" {
		if ytMeta := fetchYouTubeMetadata(vid); ytMeta != nil {
			if meta.title == "" {
				meta.title = ytMeta.Title
			}
			if meta.artist == "" {
				meta.artist = ytMeta.Artist
				meta.artistSort = ytMeta.Artist
			}
			if meta.album == "" {
				meta.album = ytMeta.Album
				meta.albumSort = ytMeta.Album
			}
			if meta.year == 0 {
				meta.year = ytMeta.Year
			}
			if meta.genre == "" {
				meta.genre = ytMeta.Genre
			}
			if meta.coverArt == nil && ytMeta.ThumbnailData != nil {
				meta.coverArt = ytMeta.ThumbnailData
			}
			return
		}
	}

	// Try to extract artist/album from directory structure
	// Expected: /music/root/Artist/Album/track.ext
	for _, dir := range s.dirs {
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			continue
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) >= 3 {
			// parts[0] = artist, parts[1] = album, parts[2] = track file
			if meta.artist == "" {
				meta.artist = parts[0]
				meta.artistSort = parts[0]
			}
			if meta.album == "" {
				meta.album = parts[1]
				meta.albumSort = parts[1]
			}
			if meta.title == "" {
				meta.title = strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(parts[len(parts)-1]))
			}
			return
		} else if len(parts) == 2 {
			// parts[0] = album, parts[1] = track file (no artist dir)
			if meta.album == "" {
				meta.album = parts[0]
				meta.albumSort = parts[0]
			}
			if meta.artist == "" {
				meta.artist = "Unknown Artist"
				meta.artistSort = "Unknown Artist"
			}
			if meta.title == "" {
				meta.title = strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(parts[len(parts)-1]))
			}
			return
		} else if len(parts) == 1 {
			// Flat file directly in library root — each file is its own album
			if meta.album == "" {
				meta.album = strings.TrimSuffix(parts[0], filepath.Ext(parts[0]))
				meta.albumSort = meta.album
			}
			if meta.artist == "" {
				meta.artist = "Unknown Artist"
				meta.artistSort = "Unknown Artist"
			}
			if meta.title == "" {
				meta.title = strings.TrimSuffix(parts[0], filepath.Ext(parts[0]))
			}
			return
		}
	}

	// Fallback: use filename as title
	if meta.title == "" {
		meta.title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if meta.artist == "" {
		meta.artist = "Unknown Artist"
		meta.artistSort = "Unknown Artist"
	}
	if meta.album == "" {
		meta.album = "Unknown Album"
		meta.albumSort = "Unknown Album"
	}


}

func (s *Scanner) startWatcher() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("scanner: failed to create file watcher: %v", err)
		return
	}
	s.watcher = watcher

	for _, dir := range s.dirs {
		if err := watcher.Add(dir); err != nil {
			log.Printf("scanner: failed to watch directory %s: %v", dir, err)
		}
	}

	go func() {
		debounce := time.NewTimer(0)
		<-debounce.C
		pending := make(map[string]bool)
		mu := sync.Mutex{}

		for {
			select {
			case <-s.stopCh:
				watcher.Close()
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Create|fsnotify.Write) == 0 {
					continue
				}
				ext := strings.ToLower(filepath.Ext(event.Name))
				if !audioExtensions[ext] {
					continue
				}

				mu.Lock()
				pending[event.Name] = true
				mu.Unlock()

				if !debounce.Stop() {
					select {
					case <-debounce.C:
					default:
					}
				}
				debounce.Reset(2 * time.Second)

			case <-debounce.C:
				mu.Lock()
				files := make([]string, 0, len(pending))
				for f := range pending {
					files = append(files, f)
				}
				pending = make(map[string]bool)
				mu.Unlock()

				for _, f := range files {
					s.IndexFile(f)
				}
			}
		}
	}()
}

func (s *Scanner) stopWatcher() {
	if s.watcher != nil {
		s.watcher.Close()
	}
}

func (s *Scanner) IndexFile(path string) {
	ext := strings.ToLower(filepath.Ext(path))
	if !audioExtensions[ext] {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		log.Printf("scanner: watch stat error for %s: %v", path, err)
		return
	}

	// Skip if file is a directory or zero-size (still being written)
	if info.IsDir() || info.Size() == 0 {
		return
	}

	// Wait briefly for file to finish writing
	time.Sleep(500 * time.Millisecond)
	info, err = os.Stat(path)
	if err != nil || info.Size() == 0 {
		return
	}

	track, err := s.parseFile(path, info)
	if err != nil {
		log.Printf("scanner: watch parse error for %s: %v", path, err)
		return
	}

	if track.artist == "" || track.album == "" {
		s.extractMetadataFromPath(path, track)
	}

	artistID, err := s.store.GetOrCreateArtist(track.artist, track.artistSort, track.mbid)
	if err != nil {
		log.Printf("scanner: watch artist error for %s: %v", path, err)
		return
	}

	coverArtPath := ""
	if track.coverArt != nil {
		coverArtPath, err = s.extractCoverArt(path, track.coverArt)
		if err != nil {
			log.Printf("scanner: warning - failed to extract cover art for %s: %v", path, err)
		}
	}

	albumID, err := s.store.GetOrCreateAlbum(artistID, track.album, track.albumSort, track.albumMBID, track.year, coverArtPath)
	if err != nil {
		log.Printf("scanner: watch album error for %s: %v", path, err)
		return
	}

	dbTrack := &model.Track{
		AlbumID:     albumID,
		ArtistID:    artistID,
		Title:       track.title,
		TrackNumber: track.trackNum,
		DiscNumber:  track.discNum,
		Duration:    track.duration,
		Year:        track.year,
		Genre:       track.genre,
		Format:      track.format,
		BitRate:     track.bitRate,
		FilePath:    path,
		FileSize:    info.Size(),
		ModTime:     info.ModTime(),
		MBID:        track.mbid,
		ReplayGain:  track.replayGain,
	}

	if err := s.store.UpsertTrack(dbTrack); err != nil {
		log.Printf("scanner: watch upsert error for %s: %v", path, err)
		return
	}

	log.Printf("scanner: auto-indexed %s", filepath.Base(path))
}

var ytVideoIDRe = regexp.MustCompile(`\[([a-zA-Z0-9_-]{11})\]`)

type youtubeMeta struct {
	Title         string `json:"title"`
	Artist        string `json:"artist"`
	Uploader      string `json:"uploader"`
	Playlist      string `json:"playlist"`
	Album         string
	UploadDate    string `json:"upload_date"`
	Genre         string `json:"genre"`
	Thumbnail     string `json:"thumbnail"`
	ThumbnailData []byte
	Year          int
}

func extractYouTubeVideoID(path string) string {
	base := filepath.Base(path)
	m := ytVideoIDRe.FindStringSubmatch(base)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func fetchYouTubeMetadata(videoID string) *youtubeMeta {
	ytdlpPath, err := exec.LookPath("yt-dlp")
	if err != nil {
		return nil
	}

	cmd := exec.Command(ytdlpPath, "--dump-json", "--no-download", "--no-warnings",
		"https://www.youtube.com/watch?v="+videoID)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var raw struct {
		Title      string `json:"title"`
		Artist     string `json:"artist"`
		Uploader   string `json:"uploader"`
		Playlist   string `json:"playlist"`
		UploadDate string `json:"upload_date"`
		Genre      string `json:"genre"`
		Thumbnail  string `json:"thumbnail"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil
	}

	meta := &youtubeMeta{
		Title:    raw.Title,
		Artist:   raw.Artist,
		Uploader: raw.Uploader,
		Playlist: raw.Playlist,
		Genre:    raw.Genre,
	}

	// Prefer artist, fall back to uploader
	if meta.Artist == "" {
		meta.Artist = meta.Uploader
	}
	if meta.Artist == "" {
		meta.Artist = "Unknown Artist"
	}

	// Album = playlist name, or fall back to title
	if meta.Playlist != "" {
		meta.Album = meta.Playlist
	} else {
		meta.Album = meta.Title
	}
	if meta.Album == "" {
		meta.Album = "Unknown Album"
	}

	// Parse year from upload_date (YYYYMMDD)
	if len(raw.UploadDate) >= 4 {
		fmt.Sscanf(raw.UploadDate[:4], "%d", &meta.Year)
	}

	// Download thumbnail
	if raw.Thumbnail != "" {
	 thumbCmd := exec.Command("curl", "-sL", "-o", "/dev/stdout", raw.Thumbnail)
	 thumbData, err := thumbCmd.Output()
	 if err == nil && len(thumbData) > 100 {
		 meta.ThumbnailData = thumbData
	 }
	}

	return meta
}

func fetchThumbnailBySearch(query string) []byte {
	ytdlpPath, err := exec.LookPath("yt-dlp")
	if err != nil {
		return nil
	}

	cmd := exec.Command(ytdlpPath, "--dump-json", "--no-download", "--no-warnings",
		"ytsearch1:"+query)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var raw struct {
		Thumbnail string `json:"thumbnail"`
	}
	if err := json.Unmarshal(out, &raw); err != nil || raw.Thumbnail == "" {
		return nil
	}

	thumbCmd := exec.Command("curl", "-sL", "--max-time", "10", "-o", "/dev/stdout", raw.Thumbnail)
	thumbData, err := thumbCmd.Output()
	if err != nil || len(thumbData) < 100 {
		return nil
	}
	return thumbData
}



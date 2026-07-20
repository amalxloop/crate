package model

import "time"

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

type Artist struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	SortName     string    `json:"sort_name"`
	MBID         string    `json:"mbid,omitempty"`
	ImageURL     string    `json:"image_url,omitempty"`
	CoverURL     string    `json:"cover_url,omitempty"`
	Bio          string    `json:"bio,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type Album struct {
	ID           int64     `json:"id"`
	ArtistID     int64     `json:"artist_id"`
	ArtistName   string    `json:"artist_name,omitempty"`
	Title        string    `json:"title"`
	SortTitle    string    `json:"sort_title"`
	Year         int       `json:"year"`
	MBID         string    `json:"mbid,omitempty"`
	CoverArtPath string    `json:"cover_art_path,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type Track struct {
	ID           int64     `json:"id"`
	AlbumID      int64     `json:"album_id"`
	ArtistID     int64     `json:"artist_id"`
	ArtistName   string    `json:"artist_name,omitempty"`
	Title        string    `json:"title"`
	TrackNumber  int       `json:"track_number"`
	DiscNumber   int       `json:"disc_number"`
	Duration     int       `json:"duration"`
	Year         int       `json:"year"`
	Genre        string    `json:"genre,omitempty"`
	Format       string    `json:"format"`
	BitRate      int       `json:"bit_rate"`
	FilePath     string    `json:"file_path"`
	FileSize     int64     `json:"file_size"`
	ModTime      time.Time `json:"mod_time"`
	MBID         string    `json:"mbid,omitempty"`
	ReplayGain   float64   `json:"replay_gain,omitempty"`
	createdAt    time.Time `json:"-"`
}

type Playlist struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	IsSmart     bool      `json:"is_smart"`
	Rules       string    `json:"rules,omitempty"`
	Public      bool      `json:"public"`
	TrackCount  int       `json:"track_count,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type PlaylistTrack struct {
	PlaylistID int64 `json:"playlist_id"`
	TrackID    int64 `json:"track_id"`
	Position   int   `json:"position"`
}

type Favorite struct {
	UserID    int64     `json:"user_id"`
	TrackID   int64     `json:"track_id,omitempty"`
	AlbumID   int64     `json:"album_id,omitempty"`
	ArtistID  int64     `json:"artist_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Rating struct {
	UserID    int64   `json:"user_id"`
	TrackID   int64   `json:"track_id,omitempty"`
	AlbumID   int64   `json:"album_id,omitempty"`
	ArtistID  int64   `json:"artist_id,omitempty"`
	Rating    int     `json:"rating"`
}

type PlayHistory struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	TrackID   int64     `json:"track_id"`
	PlayedAt  time.Time `json:"played_at"`
	Duration  int       `json:"duration"`
}

type SmartPlaylistRule struct {
	ID          int64  `json:"id"`
	PlaylistID  int64  `json:"playlist_id"`
	Field       string `json:"field"`
	Operator    string `json:"operator"`
	Value       string `json:"value"`
	Modifier    string `json:"modifier,omitempty"`
	GroupID     int    `json:"group_id,omitempty"`
	GroupLogic  string `json:"group_logic,omitempty"`
}

package db

import (
	"database/sql"
	"time"

	"github.com/crate/crate/internal/model"
)

func (db *DB) GetUserByUsername(username string) (*model.User, error) {
	u := &model.User{}
	err := db.conn.QueryRow(
		"SELECT id, username, password_hash, role, created_at FROM users WHERE username = ?",
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (db *DB) GetUserByID(id int64) (*model.User, error) {
	u := &model.User{}
	err := db.conn.QueryRow(
		"SELECT id, username, password_hash, role, created_at FROM users WHERE id = ?",
		id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (db *DB) CreateUser(username, passwordHash, role string) (int64, error) {
	res, err := db.conn.Exec(
		"INSERT INTO users (username, password_hash, role) VALUES (?, ?, ?)",
		username, passwordHash, role,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) GetUserByToken(token string) (*model.User, error) {
	u := &model.User{}
	err := db.conn.QueryRow(
		"SELECT id, username, password_hash, role, created_at FROM users WHERE token = ? AND token != ''",
		token,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (db *DB) UpdateUserToken(userID int64, token string) error {
	_, err := db.conn.Exec("UPDATE users SET token = ? WHERE id = ?", token, userID)
	return err
}

func (db *DB) ListUsers() ([]model.User, error) {
	rows, err := db.conn.Query("SELECT id, username, role, created_at FROM users ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func (db *DB) DeleteUser(id int64) error {
	_, err := db.conn.Exec("DELETE FROM users WHERE id = ? AND username != 'admin'", id)
	return err
}

func (db *DB) GetOrCreateArtist(name, sortName, mbid string) (int64, error) {
	var id int64
	err := db.conn.QueryRow("SELECT id FROM artists WHERE name = ?", name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	res, err := db.conn.Exec(
		"INSERT INTO artists (name, sort_name, mbid) VALUES (?, ?, ?)",
		name, sortName, mbid,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) GetOrCreateAlbum(artistID int64, title, sortTitle, mbid string, year int, coverArt string) (int64, error) {
	var id int64
	err := db.conn.QueryRow(
		"SELECT id FROM albums WHERE artist_id = ? AND title = ?",
		artistID, title,
	).Scan(&id)
	if err == nil {
		if coverArt != "" {
			db.conn.Exec("UPDATE albums SET cover_art_path = ? WHERE id = ? AND cover_art_path = ''", coverArt, id)
		}
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	res, err := db.conn.Exec(
		"INSERT INTO albums (artist_id, title, sort_title, year, mbid, cover_art_path) VALUES (?, ?, ?, ?, ?, ?)",
		artistID, title, sortTitle, year, mbid, coverArt,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) UpsertTrack(t *model.Track) error {
	_, err := db.conn.Exec(`
		INSERT INTO tracks (album_id, artist_id, title, track_number, disc_number, duration, year, genre, format, bit_rate, file_path, file_size, mod_time, mbid, replay_gain)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_path) DO UPDATE SET
			album_id=excluded.album_id, artist_id=excluded.artist_id, title=excluded.title,
			track_number=excluded.track_number, disc_number=excluded.disc_number, duration=excluded.duration,
			year=excluded.year, genre=excluded.genre, format=excluded.format, bit_rate=excluded.bit_rate,
			file_size=excluded.file_size, mod_time=excluded.mod_time, mbid=excluded.mbid, replay_gain=excluded.replay_gain
	`,
		t.AlbumID, t.ArtistID, t.Title, t.TrackNumber, t.DiscNumber, t.Duration,
		t.Year, t.Genre, t.Format, t.BitRate, t.FilePath, t.FileSize,
		t.ModTime, t.MBID, t.ReplayGain,
	)
	return err
}

func (db *DB) RemoveTrackByPath(path string) error {
	_, err := db.conn.Exec("DELETE FROM tracks WHERE file_path = ?", path)
	return err
}

func (db *DB) GetTrackByID(id int64) (*model.Track, error) {
	t := &model.Track{}
	err := db.conn.QueryRow(`
		SELECT t.id, t.album_id, t.artist_id, COALESCE(ar.name, ''), t.title, t.track_number, t.disc_number, t.duration, t.year, t.genre,
			t.format, t.bit_rate, t.file_path, t.file_size, t.mod_time, t.mbid, t.replay_gain
		FROM tracks t LEFT JOIN artists ar ON t.artist_id = ar.id WHERE t.id = ?
	`, id).Scan(
		&t.ID, &t.AlbumID, &t.ArtistID, &t.ArtistName, &t.Title, &t.TrackNumber, &t.DiscNumber,
		&t.Duration, &t.Year, &t.Genre, &t.Format, &t.BitRate, &t.FilePath,
		&t.FileSize, &t.ModTime, &t.MBID, &t.ReplayGain,
	)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (db *DB) GetTracksByAlbum(albumID int64) ([]model.Track, error) {
	rows, err := db.conn.Query(`
		SELECT t.id, t.album_id, t.artist_id, COALESCE(ar.name, ''), t.title, t.track_number, t.disc_number, t.duration, t.year, t.genre,
			t.format, t.bit_rate, t.file_path, t.file_size, t.mod_time, t.mbid, t.replay_gain
		FROM tracks t LEFT JOIN artists ar ON t.artist_id = ar.id
		WHERE t.album_id = ? ORDER BY t.disc_number, t.track_number
	`, albumID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTracks(rows)
}

func (db *DB) GetTracksByArtist(artistID int64) ([]model.Track, error) {
	rows, err := db.conn.Query(`
		SELECT t.id, t.album_id, t.artist_id, COALESCE(ar.name, ''), t.title, t.track_number, t.disc_number, t.duration, t.year, t.genre,
			t.format, t.bit_rate, t.file_path, t.file_size, t.mod_time, t.mbid, t.replay_gain
		FROM tracks t LEFT JOIN artists ar ON t.artist_id = ar.id
		WHERE t.artist_id = ? ORDER BY t.year, t.album_id, t.disc_number, t.track_number
	`, artistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTracks(rows)
}

func (db *DB) SearchTracks(query string, limit int) ([]model.Track, error) {
	q := "%" + query + "%"
	rows, err := db.conn.Query(`
		SELECT t.id, t.album_id, t.artist_id, COALESCE(ar.name, ''), t.title, t.track_number, t.disc_number, t.duration, t.year, t.genre,
			t.format, t.bit_rate, t.file_path, t.file_size, t.mod_time, t.mbid, t.replay_gain
		FROM tracks t LEFT JOIN artists ar ON t.artist_id = ar.id
		WHERE t.title LIKE ? OR t.genre LIKE ? OR t.file_path LIKE ?
		ORDER BY t.title LIMIT ?
	`, q, q, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTracks(rows)
}

func (db *DB) SearchAlbums(query string, limit int) ([]model.Album, error) {
	q := "%" + query + "%"
	rows, err := db.conn.Query(`
		SELECT a.id, a.artist_id, COALESCE(ar.name, ''), a.title, a.sort_title, a.year, a.mbid, a.cover_art_path, a.created_at
		FROM albums a LEFT JOIN artists ar ON a.artist_id = ar.id WHERE a.title LIKE ?
		ORDER BY a.title LIMIT ?
	`, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAlbums(rows)
}

func (db *DB) SearchArtists(query string, limit int) ([]model.Artist, error) {
	q := "%" + query + "%"
	rows, err := db.conn.Query(`
		SELECT id, name, sort_name, mbid, image_url, bio, created_at
		FROM artists WHERE name LIKE ?
		ORDER BY name LIMIT ?
	`, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArtists(rows)
}

func (db *DB) GetAllArtists() ([]model.Artist, error) {
	rows, err := db.conn.Query("SELECT id, name, sort_name, mbid, image_url, bio, created_at FROM artists ORDER BY sort_name, name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArtists(rows)
}

func (db *DB) GetArtistByID(id int64) (*model.Artist, error) {
	a := &model.Artist{}
	err := db.conn.QueryRow(
		"SELECT id, name, sort_name, mbid, image_url, bio, created_at FROM artists WHERE id = ?", id,
	).Scan(&a.ID, &a.Name, &a.SortName, &a.MBID, &a.ImageURL, &a.Bio, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (db *DB) GetAlbumsByArtist(artistID int64) ([]model.Album, error) {
	rows, err := db.conn.Query(`
		SELECT a.id, a.artist_id, COALESCE(ar.name, ''), a.title, a.sort_title, a.year, a.mbid, a.cover_art_path, a.created_at
		FROM albums a LEFT JOIN artists ar ON a.artist_id = ar.id WHERE a.artist_id = ? ORDER BY a.year, a.title
	`, artistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAlbums(rows)
}

func (db *DB) GetAllAlbums() ([]model.Album, error) {
	rows, err := db.conn.Query(`
		SELECT a.id, a.artist_id, COALESCE(ar.name, ''), a.title, a.sort_title, a.year, a.mbid, a.cover_art_path, a.created_at
		FROM albums a LEFT JOIN artists ar ON a.artist_id = ar.id ORDER BY a.title
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAlbums(rows)
}

func (db *DB) GetAlbumByID(id int64) (*model.Album, error) {
	a := &model.Album{}
	err := db.conn.QueryRow(`
		SELECT a.id, a.artist_id, COALESCE(ar.name, ''), a.title, a.sort_title, a.year, a.mbid, a.cover_art_path, a.created_at
		FROM albums a LEFT JOIN artists ar ON a.artist_id = ar.id WHERE a.id = ?
	`, id).Scan(&a.ID, &a.ArtistID, &a.ArtistName, &a.Title, &a.SortTitle, &a.Year, &a.MBID, &a.CoverArtPath, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (db *DB) GetLibraryStats() (artists, albums, tracks int, duration int64, err error) {
	err = db.conn.QueryRow("SELECT COUNT(*) FROM artists").Scan(&artists)
	if err != nil {
		return
	}
	err = db.conn.QueryRow("SELECT COUNT(*) FROM albums").Scan(&albums)
	if err != nil {
		return
	}
	err = db.conn.QueryRow("SELECT COUNT(*), COALESCE(SUM(duration), 0) FROM tracks").Scan(&tracks, &duration)
	return
}

func (db *DB) GetRecentlyAdded(limit int) ([]model.Track, error) {
	rows, err := db.conn.Query(`
		SELECT t.id, t.album_id, t.artist_id, COALESCE(ar.name, ''), t.title, t.track_number, t.disc_number, t.duration, t.year, t.genre,
			t.format, t.bit_rate, t.file_path, t.file_size, t.mod_time, t.mbid, t.replay_gain
		FROM tracks t LEFT JOIN artists ar ON t.artist_id = ar.id ORDER BY t.created_at DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTracks(rows)
}

func (db *DB) GetRecentlyPlayed(userID int64, limit int) ([]model.Track, error) {
	rows, err := db.conn.Query(`
		SELECT t.id, t.album_id, t.artist_id, COALESCE(ar.name, ''), t.title, t.track_number, t.disc_number, t.duration, t.year, t.genre,
			t.format, t.bit_rate, t.file_path, t.file_size, t.mod_time, t.mbid, t.replay_gain
		FROM tracks t JOIN play_history ph ON t.id = ph.track_id LEFT JOIN artists ar ON t.artist_id = ar.id
		WHERE ph.user_id = ? ORDER BY ph.played_at DESC LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTracks(rows)
}

func (db *DB) GetMostPlayed(userID int64, limit int) ([]model.Track, error) {
	rows, err := db.conn.Query(`
		SELECT t.id, t.album_id, t.artist_id, COALESCE(ar.name, ''), t.title, t.track_number, t.disc_number, t.duration, t.year, t.genre,
			t.format, t.bit_rate, t.file_path, t.file_size, t.mod_time, t.mbid, t.replay_gain
		FROM tracks t
		JOIN (SELECT track_id, COUNT(*) as cnt FROM play_history WHERE user_id = ? GROUP BY track_id) ph
		ON t.id = ph.track_id LEFT JOIN artists ar ON t.artist_id = ar.id
		ORDER BY ph.cnt DESC LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTracks(rows)
}

func (db *DB) SetFavorite(userID int64, itemType string, itemID int64) error {
	_, err := db.conn.Exec(`
		INSERT OR REPLACE INTO favorites (user_id, item_type, item_id) VALUES (?, ?, ?)
	`, userID, itemType, itemID)
	return err
}

func (db *DB) RemoveFavorite(userID int64, itemType string, itemID int64) error {
	_, err := db.conn.Exec(`
		DELETE FROM favorites WHERE user_id = ? AND item_type = ? AND item_id = ?
	`, userID, itemType, itemID)
	return err
}

func (db *DB) SetRating(userID int64, itemType string, itemID int64, rating int) error {
	_, err := db.conn.Exec(`
		INSERT OR REPLACE INTO ratings (user_id, item_type, item_id, rating) VALUES (?, ?, ?, ?)
	`, userID, itemType, itemID, rating)
	return err
}

func (db *DB) GetStarredTracks(userID int64) ([]model.Track, error) {
	rows, err := db.conn.Query(`
		SELECT t.id, t.album_id, t.artist_id, COALESCE(ar.name, ''), t.title, t.track_number, t.disc_number, t.duration, t.year, t.genre,
			t.format, t.bit_rate, t.file_path, t.file_size, t.mod_time, t.mbid, t.replay_gain
		FROM tracks t JOIN favorites f ON t.id = f.item_id LEFT JOIN artists ar ON t.artist_id = ar.id
		WHERE f.user_id = ? AND f.item_type = 'track' ORDER BY f.created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTracks(rows)
}

func (db *DB) RecordPlay(userID, trackID int64, duration int) error {
	_, err := db.conn.Exec(
		"INSERT INTO play_history (user_id, track_id, duration) VALUES (?, ?, ?)",
		userID, trackID, duration,
	)
	return err
}

func (db *DB) GetPlayHistory(userID int64, limit int, offset int) ([]model.PlayHistory, error) {
	rows, err := db.conn.Query(`
		SELECT id, user_id, track_id, played_at, duration
		FROM play_history WHERE user_id = ? ORDER BY played_at DESC LIMIT ? OFFSET ?
	`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []model.PlayHistory
	for rows.Next() {
		var h model.PlayHistory
		if err := rows.Scan(&h.ID, &h.UserID, &h.TrackID, &h.PlayedAt, &h.Duration); err != nil {
			return nil, err
		}
		history = append(history, h)
	}
	return history, nil
}

func (db *DB) GetScanState() (lastScan time.Time, inProgress bool, filesFound, filesIndexed int, err error) {
	err = db.conn.QueryRow("SELECT last_scan, in_progress, files_found, files_indexed FROM scan_state WHERE id = 1").
		Scan(&lastScan, &inProgress, &filesFound, &filesIndexed)
	return
}

func (db *DB) SetScanInProgress(inProgress bool, filesFound, filesIndexed int) error {
	_, err := db.conn.Exec(
		"UPDATE scan_state SET in_progress = ?, files_found = ?, files_indexed = ? WHERE id = 1",
		boolToInt(inProgress), filesFound, filesIndexed,
	)
	return err
}

func (db *DB) SetScanComplete(filesIndexed int) error {
	_, err := db.conn.Exec(
		"UPDATE scan_state SET last_scan = ?, in_progress = 0, files_found = ?, files_indexed = ? WHERE id = 1",
		time.Now(), filesIndexed, filesIndexed,
	)
	return err
}

func (db *DB) GetAllTrackPaths() (map[string]time.Time, error) {
	rows, err := db.conn.Query("SELECT file_path, mod_time FROM tracks")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	paths := make(map[string]time.Time)
	for rows.Next() {
		var p string
		var mt time.Time
		if err := rows.Scan(&p, &mt); err != nil {
			return nil, err
		}
		paths[p] = mt
	}
	return paths, nil
}

func (db *DB) GetRandomTracks(limit int) ([]model.Track, error) {
	rows, err := db.conn.Query(`
		SELECT t.id, t.album_id, t.artist_id, COALESCE(ar.name, ''), t.title, t.track_number, t.disc_number, t.duration, t.year, t.genre,
			t.format, t.bit_rate, t.file_path, t.file_size, t.mod_time, t.mbid, t.replay_gain
		FROM tracks t LEFT JOIN artists ar ON t.artist_id = ar.id ORDER BY RANDOM() LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTracks(rows)
}

func (db *DB) GetTracksByGenre(genre string, limit int) ([]model.Track, error) {
	rows, err := db.conn.Query(`
		SELECT t.id, t.album_id, t.artist_id, COALESCE(ar.name, ''), t.title, t.track_number, t.disc_number, t.duration, t.year, t.genre,
			t.format, t.bit_rate, t.file_path, t.file_size, t.mod_time, t.mbid, t.replay_gain
		FROM tracks t LEFT JOIN artists ar ON t.artist_id = ar.id WHERE t.genre LIKE ? ORDER BY RANDOM() LIMIT ?
	`, "%"+genre+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTracks(rows)
}

func scanTracks(rows *sql.Rows) ([]model.Track, error) {
	var tracks []model.Track
	for rows.Next() {
		var t model.Track
		if err := rows.Scan(
			&t.ID, &t.AlbumID, &t.ArtistID, &t.ArtistName, &t.Title, &t.TrackNumber, &t.DiscNumber,
			&t.Duration, &t.Year, &t.Genre, &t.Format, &t.BitRate, &t.FilePath,
			&t.FileSize, &t.ModTime, &t.MBID, &t.ReplayGain,
		); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, nil
}

func scanAlbums(rows *sql.Rows) ([]model.Album, error) {
	var albums []model.Album
	for rows.Next() {
		var a model.Album
		if err := rows.Scan(&a.ID, &a.ArtistID, &a.ArtistName, &a.Title, &a.SortTitle, &a.Year, &a.MBID, &a.CoverArtPath, &a.CreatedAt); err != nil {
			return nil, err
		}
		albums = append(albums, a)
	}
	return albums, nil
}

func scanArtists(rows *sql.Rows) ([]model.Artist, error) {
	var artists []model.Artist
	for rows.Next() {
		var a model.Artist
		if err := rows.Scan(&a.ID, &a.Name, &a.SortName, &a.MBID, &a.ImageURL, &a.Bio, &a.CreatedAt); err != nil {
			return nil, err
		}
		artists = append(artists, a)
	}
	return artists, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

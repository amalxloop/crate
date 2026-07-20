package db

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL DEFAULT '',
		role TEXT NOT NULL DEFAULT 'user',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	`CREATE TABLE IF NOT EXISTS artists (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		sort_name TEXT NOT NULL DEFAULT '',
		mbid TEXT DEFAULT '',
		image_url TEXT DEFAULT '',
		bio TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	`CREATE TABLE IF NOT EXISTS albums (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		artist_id INTEGER NOT NULL REFERENCES artists(id),
		title TEXT NOT NULL,
		sort_title TEXT NOT NULL DEFAULT '',
		year INTEGER DEFAULT 0,
		mbid TEXT DEFAULT '',
		cover_art_path TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	`CREATE TABLE IF NOT EXISTS tracks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		album_id INTEGER NOT NULL REFERENCES albums(id),
		artist_id INTEGER NOT NULL REFERENCES artists(id),
		title TEXT NOT NULL,
		track_number INTEGER DEFAULT 0,
		disc_number INTEGER DEFAULT 1,
		duration INTEGER DEFAULT 0,
		year INTEGER DEFAULT 0,
		genre TEXT DEFAULT '',
		format TEXT DEFAULT '',
		bit_rate INTEGER DEFAULT 0,
		file_path TEXT UNIQUE NOT NULL,
		file_size INTEGER DEFAULT 0,
		mod_time DATETIME DEFAULT CURRENT_TIMESTAMP,
		mbid TEXT DEFAULT '',
		replay_gain REAL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	`CREATE INDEX IF NOT EXISTS idx_tracks_album ON tracks(album_id)`,
	`CREATE INDEX IF NOT EXISTS idx_tracks_artist ON tracks(artist_id)`,
	`CREATE INDEX IF NOT EXISTS idx_tracks_genre ON tracks(genre)`,
	`CREATE INDEX IF NOT EXISTS idx_tracks_year ON tracks(year)`,
	`CREATE INDEX IF NOT EXISTS idx_tracks_path ON tracks(file_path)`,

	`CREATE TABLE IF NOT EXISTS playlists (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id),
		name TEXT NOT NULL,
		description TEXT DEFAULT '',
		is_smart INTEGER DEFAULT 0,
		rules TEXT DEFAULT '',
		public INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	`CREATE TABLE IF NOT EXISTS playlist_tracks (
		playlist_id INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
		track_id INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
		position INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (playlist_id, track_id)
	)`,

	`CREATE TABLE IF NOT EXISTS smart_playlist_rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		playlist_id INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
		field TEXT NOT NULL,
		operator TEXT NOT NULL,
		value TEXT NOT NULL,
		modifier TEXT DEFAULT '',
		group_id INTEGER DEFAULT 0,
		group_logic TEXT DEFAULT 'and'
	)`,

	`CREATE TABLE IF NOT EXISTS favorites (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id),
		item_type TEXT NOT NULL DEFAULT 'track',
		item_id INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(user_id, item_type, item_id)
	)`,

	`CREATE TABLE IF NOT EXISTS ratings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id),
		item_type TEXT NOT NULL DEFAULT 'track',
		item_id INTEGER NOT NULL,
		rating INTEGER NOT NULL DEFAULT 0,
		UNIQUE(user_id, item_type, item_id)
	)`,

	`CREATE TABLE IF NOT EXISTS play_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id),
		track_id INTEGER NOT NULL REFERENCES tracks(id),
		played_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		duration INTEGER DEFAULT 0
	)`,

	`CREATE INDEX IF NOT EXISTS idx_play_history_user ON play_history(user_id, played_at DESC)`,

	`CREATE TABLE IF NOT EXISTS scan_state (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		last_scan DATETIME,
		in_progress INTEGER DEFAULT 0,
		files_found INTEGER DEFAULT 0,
		files_indexed INTEGER DEFAULT 0
	)`,

	`CREATE TABLE IF NOT EXISTS playlist_shares (
		playlist_id INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (playlist_id, user_id)
	)`,

	`CREATE TABLE IF NOT EXISTS api_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id),
		key TEXT UNIQUE NOT NULL,
		name TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME
	)`,

	`ALTER TABLE users ADD COLUMN token TEXT DEFAULT ''`,

	`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_token ON users(token) WHERE token != ''`,
}

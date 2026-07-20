package db

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	conn *sql.DB
}

func New(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	conn.SetMaxOpenConns(1)

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) Conn() *sql.DB {
	return db.conn
}

func (db *DB) migrate() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for i, m := range migrations {
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", i).Scan(&count); err != nil {
			return fmt.Errorf("check migration %d: %w", i, err)
		}
		if count > 0 {
			continue
		}
		if _, err := tx.Exec(m); err != nil {
			return fmt.Errorf("migration %d: %w", i, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", i); err != nil {
			return fmt.Errorf("record migration %d: %w", i, err)
		}
	}

	return tx.Commit()
}

func (db *DB) InsertDefaultAdmin(username, passwordHash string) error {
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	_, err = db.conn.Exec(
		"INSERT INTO users (username, password_hash, role) VALUES (?, ?, 'admin')",
		username, passwordHash,
	)
	if err != nil {
		return err
	}

	_, err = db.conn.Exec("INSERT OR IGNORE INTO scan_state (id) VALUES (1)")

	log.Printf("Created default admin user: %s", username)
	return err
}

func (db *DB) CleanupOrphans() (int64, error) {
	result, err := db.conn.Exec(`
		DELETE FROM playlist_tracks WHERE track_id NOT IN (SELECT id FROM tracks);
	`)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()

	db.conn.Exec(`DELETE FROM favorites WHERE track_id NOT IN (SELECT id FROM tracks);`)
	db.conn.Exec(`DELETE FROM play_history WHERE track_id NOT IN (SELECT id FROM tracks);`)
	db.conn.Exec(`DELETE FROM ratings WHERE track_id NOT IN (SELECT id FROM tracks);`)
	db.conn.Exec(`DELETE FROM scan_state WHERE id NOT IN (SELECT id FROM scan_state);`)

	return n, nil
}

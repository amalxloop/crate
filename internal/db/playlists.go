package db

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/crate/crate/internal/model"
)

func (db *DB) CreatePlaylist(userID int64, name, description string, isSmart bool, public bool) (int64, error) {
	res, err := db.conn.Exec(
		`INSERT INTO playlists (user_id, name, description, is_smart, public) VALUES (?, ?, ?, ?, ?)`,
		userID, name, description, boolToInt(isSmart), boolToInt(public),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) UpdatePlaylist(id int64, name, description string, public bool) error {
	_, err := db.conn.Exec(
		`UPDATE playlists SET name = ?, description = ?, public = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, description, boolToInt(public), id,
	)
	return err
}

func (db *DB) DeletePlaylist(id int64) error {
	_, err := db.conn.Exec(`DELETE FROM playlists WHERE id = ?`, id)
	return err
}

func (db *DB) GetUserPlaylists(userID int64) ([]model.Playlist, error) {
	rows, err := db.conn.Query(`
		SELECT p.id, p.user_id, p.name, p.description, p.is_smart, p.rules, p.public,
			COALESCE(pt.cnt, 0), p.created_at, p.updated_at
		FROM playlists p
		LEFT JOIN (SELECT playlist_id, COUNT(*) as cnt FROM playlist_tracks GROUP BY playlist_id) pt
			ON p.id = pt.playlist_id
		WHERE p.user_id = ?
		ORDER BY p.name
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPlaylists(rows)
}

func (db *DB) GetPlaylistByID(id int64) (*model.Playlist, error) {
	p := &model.Playlist{}
	err := db.conn.QueryRow(`
		SELECT p.id, p.user_id, p.name, p.description, p.is_smart, p.rules, p.public,
			COALESCE(pt.cnt, 0), p.created_at, p.updated_at
		FROM playlists p
		LEFT JOIN (SELECT playlist_id, COUNT(*) as cnt FROM playlist_tracks GROUP BY playlist_id) pt
			ON p.id = pt.playlist_id
		WHERE p.id = ?
	`, id).Scan(
		&p.ID, &p.UserID, &p.Name, &p.Description, &p.IsSmart, &p.Rules,
		&p.Public, &p.TrackCount, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (db *DB) AddTrackToPlaylist(playlistID, trackID int64, position int) error {
	_, err := db.conn.Exec(
		`INSERT OR REPLACE INTO playlist_tracks (playlist_id, track_id, position) VALUES (?, ?, ?)`,
		playlistID, trackID, position,
	)
	if err != nil {
		return err
	}
	_, err = db.conn.Exec(
		`UPDATE playlists SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, playlistID,
	)
	return err
}

func (db *DB) RemoveTrackFromPlaylist(playlistID, trackID int64) error {
	_, err := db.conn.Exec(
		`DELETE FROM playlist_tracks WHERE playlist_id = ? AND track_id = ?`,
		playlistID, trackID,
	)
	if err != nil {
		return err
	}
	_, err = db.conn.Exec(
		`UPDATE playlists SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, playlistID,
	)
	return err
}

func (db *DB) ReorderPlaylistTracks(playlistID int64, trackIDs []int64) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i, trackID := range trackIDs {
		_, err := tx.Exec(
			`INSERT OR REPLACE INTO playlist_tracks (playlist_id, track_id, position) VALUES (?, ?, ?)`,
			playlistID, trackID, i,
		)
		if err != nil {
			return err
		}
	}

	_, err = tx.Exec(
		`UPDATE playlists SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, playlistID,
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) GetPlaylistTracks(playlistID int64) ([]model.Track, error) {
	rows, err := db.conn.Query(`
		SELECT t.id, t.album_id, t.artist_id, COALESCE(a.name, 'Unknown Artist'), t.title, t.track_number, t.disc_number, t.duration,
			t.year, t.genre, t.format, t.bit_rate, t.file_path, t.file_size, t.mod_time, t.mbid, t.replay_gain
		FROM tracks t
		JOIN playlist_tracks pt ON t.id = pt.track_id
		LEFT JOIN artists a ON t.artist_id = a.id
		WHERE pt.playlist_id = ?
		ORDER BY pt.position
	`, playlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTracks(rows)
}

func (db *DB) SetSmartPlaylistRules(playlistID int64, rules []model.SmartPlaylistRule) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM smart_playlist_rules WHERE playlist_id = ?`, playlistID)
	if err != nil {
		return err
	}

	for _, r := range rules {
		_, err = tx.Exec(
			`INSERT INTO smart_playlist_rules (playlist_id, field, operator, value, modifier, group_id, group_logic)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			playlistID, r.Field, r.Operator, r.Value, r.Modifier, r.GroupID, r.GroupLogic,
		)
		if err != nil {
			return err
		}
	}

	_, err = tx.Exec(
		`UPDATE playlists SET is_smart = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, playlistID,
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) GetSmartPlaylistRules(playlistID int64) ([]model.SmartPlaylistRule, error) {
	rows, err := db.conn.Query(
		`SELECT id, playlist_id, field, operator, value, modifier, group_id, group_logic
		FROM smart_playlist_rules WHERE playlist_id = ? ORDER BY group_id, id`,
		playlistID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []model.SmartPlaylistRule
	for rows.Next() {
		var r model.SmartPlaylistRule
		if err := rows.Scan(&r.ID, &r.PlaylistID, &r.Field, &r.Operator, &r.Value,
			&r.Modifier, &r.GroupID, &r.GroupLogic); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, nil
}

var allowedSmartFields = map[string]string{
	"title":      "t.title",
	"genre":      "t.genre",
	"artist":     "ar.name",
	"album":      "al.title",
	"year":       "t.year",
	"duration":   "t.duration",
	"format":     "t.format",
	"bit_rate":   "t.bit_rate",
	"track_num":  "t.track_number",
	"disc_num":   "t.disc_number",
	"file_path":  "t.file_path",
	"play_count": "ph.cnt",
}

func (db *DB) EvaluateSmartPlaylist(userID int64, rules []model.SmartPlaylistRule, limit int) ([]model.Track, error) {
	if len(rules) == 0 {
		return nil, nil
	}

	needsAlbumJoin := false
	needsPlayHistory := false

	for _, r := range rules {
		col, ok := allowedSmartFields[r.Field]
		if !ok {
			return nil, fmt.Errorf("unknown smart playlist field: %s", r.Field)
		}
		if strings.HasPrefix(col, "al.") {
			needsAlbumJoin = true
		}
		if strings.HasPrefix(col, "ph.") {
			needsPlayHistory = true
		}
	}

	trackCols := `t.id, t.album_id, t.artist_id, COALESCE(ar.name, ''), t.title, t.track_number, t.disc_number, t.duration,
		t.year, t.genre, t.format, t.bit_rate, t.file_path, t.file_size, t.mod_time, t.mbid, t.replay_gain`

	query := fmt.Sprintf(`SELECT DISTINCT %s FROM tracks t
		LEFT JOIN artists ar ON t.artist_id = ar.id`, trackCols)

	if needsAlbumJoin {
		query += ` JOIN albums al ON t.album_id = al.id`
	}
	if needsPlayHistory {
		query += ` LEFT JOIN (SELECT track_id, COUNT(*) as cnt FROM play_history WHERE user_id = ? GROUP BY track_id) ph ON t.id = ph.track_id`
	}

	grouped := groupRulesByGroupID(rules)
	groupIDs := make([]int, 0, len(grouped))
	for gid := range grouped {
		groupIDs = append(groupIDs, gid)
	}
	sort.Ints(groupIDs)

	whereClauses := make([]string, 0, len(groupIDs))
	args := make([]interface{}, 0)
	if needsPlayHistory {
		args = append(args, userID)
	}

	for i, gid := range groupIDs {
		groupRules := grouped[gid]
		innerClauses := make([]string, 0, len(groupRules))

		for _, r := range groupRules {
			col, _ := allowedSmartFields[r.Field]
			clause, clauseArgs := buildRuleClause(col, r)
			innerClauses = append(innerClauses, clause)
			args = append(args, clauseArgs...)
		}

		innerSQL := strings.Join(innerClauses, " AND ")
		if len(innerClauses) > 1 {
			innerSQL = "(" + innerSQL + ")"
		}

		if i > 0 && len(groupRules) > 0 {
			logic := "AND"
			if groupRules[0].GroupLogic == "or" {
				logic = "OR"
			}
			whereClauses = append(whereClauses, fmt.Sprintf("%s %s", logic, innerSQL))
		} else {
			whereClauses = append(whereClauses, innerSQL)
		}
	}

	if len(whereClauses) > 0 {
		query += " WHERE " + strings.Join(whereClauses, " ")
	}

	query += " ORDER BY t.title"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("smart playlist query: %w", err)
	}
	defer rows.Close()
	return scanTracks(rows)
}

func buildRuleClause(col string, r model.SmartPlaylistRule) (string, []interface{}) {
	op := strings.ToLower(r.Operator)
	val := r.Value

	switch op {
	case "equals":
		return fmt.Sprintf("%s = ?", col), []interface{}{val}
	case "not_equals":
		return fmt.Sprintf("%s != ?", col), []interface{}{val}
	case "contains":
		return fmt.Sprintf("%s LIKE ?", col), []interface{}{"%" + val + "%"}
	case "not_contains":
		return fmt.Sprintf("%s NOT LIKE ?", col), []interface{}{"%" + val + "%"}
	case "starts_with":
		return fmt.Sprintf("%s LIKE ?", col), []interface{}{val + "%"}
	case "ends_with":
		return fmt.Sprintf("%s LIKE ?", col), []interface{}{"%" + val}
	case "greater_than":
		return fmt.Sprintf("%s > ?", col), []interface{}{val}
	case "less_than":
		return fmt.Sprintf("%s < ?", col), []interface{}{val}
	case "greater_equal":
		return fmt.Sprintf("%s >= ?", col), []interface{}{val}
	case "less_equal":
		return fmt.Sprintf("%s <= ?", col), []interface{}{val}
	case "between":
		parts := strings.SplitN(val, ",", 2)
		if len(parts) == 2 {
			return fmt.Sprintf("%s BETWEEN ? AND ?", col), []interface{}{parts[0], parts[1]}
		}
		return fmt.Sprintf("%s >= ?", col), []interface{}{val}
	case "in":
		vals := strings.Split(val, ",")
		placeholders := make([]string, len(vals))
		args := make([]interface{}, len(vals))
		for i, v := range vals {
			placeholders[i] = "?"
			args[i] = strings.TrimSpace(v)
		}
		return fmt.Sprintf("%s IN (%s)", col, strings.Join(placeholders, ",")), args
	case "not_in":
		vals := strings.Split(val, ",")
		placeholders := make([]string, len(vals))
		args := make([]interface{}, len(vals))
		for i, v := range vals {
			placeholders[i] = "?"
			args[i] = strings.TrimSpace(v)
		}
		return fmt.Sprintf("%s NOT IN (%s)", col, strings.Join(placeholders, ",")), args
	case "is_empty":
		return fmt.Sprintf("(%s IS NULL OR %s = '')", col, col), nil
	case "is_not_empty":
		return fmt.Sprintf("%s IS NOT NULL AND %s != ''", col, col), nil
	default:
		return fmt.Sprintf("%s = ?", col), []interface{}{val}
	}
}

func groupRulesByGroupID(rules []model.SmartPlaylistRule) map[int][]model.SmartPlaylistRule {
	grouped := make(map[int][]model.SmartPlaylistRule)
	for _, r := range rules {
		grouped[r.GroupID] = append(grouped[r.GroupID], r)
	}
	return grouped
}

func (db *DB) SharePlaylist(playlistID, targetUserID int64) error {
	_, err := db.conn.Exec(
		`INSERT OR IGNORE INTO playlist_shares (playlist_id, user_id) VALUES (?, ?)`,
		playlistID, targetUserID,
	)
	return err
}

func (db *DB) GetSharedPlaylists(userID int64) ([]model.Playlist, error) {
	rows, err := db.conn.Query(`
		SELECT p.id, p.user_id, p.name, p.description, p.is_smart, p.rules, p.public,
			COALESCE(pt.cnt, 0), p.created_at, p.updated_at
		FROM playlists p
		JOIN playlist_shares ps ON p.id = ps.playlist_id
		LEFT JOIN (SELECT playlist_id, COUNT(*) as cnt FROM playlist_tracks GROUP BY playlist_id) pt
			ON p.id = pt.playlist_id
		WHERE ps.user_id = ?
		ORDER BY p.name
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPlaylists(rows)
}

func scanPlaylists(rows *sql.Rows) ([]model.Playlist, error) {
	var playlists []model.Playlist
	for rows.Next() {
		var p model.Playlist
		if err := rows.Scan(
			&p.ID, &p.UserID, &p.Name, &p.Description, &p.IsSmart, &p.Rules,
			&p.Public, &p.TrackCount, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		playlists = append(playlists, p)
	}
	return playlists, nil
}

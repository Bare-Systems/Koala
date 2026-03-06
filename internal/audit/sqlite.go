package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if path == "" {
		return nil, fmt.Errorf("sqlite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	store := &SQLiteStore{db: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) migrate() error {
	stmt := `
CREATE TABLE IF NOT EXISTS audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    category TEXT NOT NULL,
    event_type TEXT NOT NULL,
    severity TEXT NOT NULL,
    message TEXT NOT NULL,
    rollout_id TEXT,
    device_id TEXT,
    key_id TEXT,
    created_at TEXT NOT NULL,
    payload_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_audit_events_category_created ON audit_events(category, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_events_event_type_created ON audit_events(event_type, created_at);
`
	_, err := s.db.Exec(stmt)
	if err != nil {
		return fmt.Errorf("migrate sqlite: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Record(ctx context.Context, e Event) error {
	if e.CreatedAt == "" {
		e.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	payloadJSON := ""
	if e.Payload != nil {
		raw, err := json.Marshal(e.Payload)
		if err != nil {
			return fmt.Errorf("encode payload: %w", err)
		}
		payloadJSON = string(raw)
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO audit_events (category, event_type, severity, message, rollout_id, device_id, key_id, created_at, payload_json)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Category,
		e.EventType,
		e.Severity,
		e.Message,
		e.RolloutID,
		e.DeviceID,
		e.KeyID,
		e.CreatedAt,
		payloadJSON,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

func (s *SQLiteStore) List(ctx context.Context, options ListOptions) ([]Event, error) {
	limit := options.Limit
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT id, category, event_type, severity, message, rollout_id, device_id, key_id, created_at, payload_json
              FROM audit_events`
	args := []any{}
	if options.Category != "" {
		query += ` WHERE category = ?`
		args = append(args, options.Category)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	out := make([]Event, 0)
	for rows.Next() {
		var e Event
		var payloadJSON sql.NullString
		if err := rows.Scan(&e.ID, &e.Category, &e.EventType, &e.Severity, &e.Message, &e.RolloutID, &e.DeviceID, &e.KeyID, &e.CreatedAt, &payloadJSON); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		if payloadJSON.Valid && payloadJSON.String != "" {
			var payload any
			if err := json.Unmarshal([]byte(payloadJSON.String), &payload); err == nil {
				e.Payload = payload
			}
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	// Return chronological order for easier UI consumption.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

package audit

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStorePersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "events.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	event := Event{
		Category:  "security",
		EventType: "unknown_key_id",
		Severity:  "high",
		Message:   "unknown key",
		KeyID:     "key-bad",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Payload:   map[string]any{"kind": "manifest"},
	}
	if err := store.Record(context.Background(), event); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	events, err := reopened.List(context.Background(), ListOptions{Category: "security", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event after reopen, got %d", len(events))
	}
	if events[0].EventType != "unknown_key_id" {
		t.Fatalf("unexpected event type: %s", events[0].EventType)
	}
}

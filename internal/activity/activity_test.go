package activity

import (
	"context"
	"path/filepath"
	"testing"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/config"
	"github.com/nickheyer/discopanel/pkg/logger"
)

func newTestRecorder(t *testing.T) (*Recorder, *storage.Store) {
	t.Helper()
	tmp := t.TempDir()
	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	cfg.Database.Path = filepath.Join(tmp, "test.db")
	cfg.Storage.DataDir = tmp
	cfg.Storage.BackupDir = filepath.Join(tmp, "backups")
	store, err := storage.NewSQLiteStore(cfg)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return NewRecorder(store, logger.New()), store
}

func TestRecordCarriesContext(t *testing.T) {
	rec, store := newTestRecorder(t)

	ctx := WithTraceID(WithSource(context.Background(), "nick"), "trace-1")
	rec.Record(ctx, "srv1", "server.start", Attrs{"key": "value"}, "started the server")

	rows, err := store.GetServerActions(context.Background(), "srv1", 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Source != "nick" || row.Name != "server.start" || row.TraceID != "trace-1" {
		t.Fatalf("row = %+v", row)
	}
	if row.Attrs["key"] != "value" {
		t.Fatalf("attrs = %v", row.Attrs)
	}
	if row.Message != "started the server" {
		t.Fatalf("message = %q", row.Message)
	}
}

func TestRecordSurvivesCancelledContext(t *testing.T) {
	rec, store := newTestRecorder(t)

	ctx, cancel := context.WithCancel(WithSource(context.Background(), "scheduler"))
	cancel()
	rec.Record(ctx, "srv1", "task.command", nil, "ran command")

	rows, err := store.GetServerActions(context.Background(), "srv1", 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %d err = %v, want 1 row", len(rows), err)
	}
}

func TestAnnounceEchoesWithSource(t *testing.T) {
	rec, _ := newTestRecorder(t)

	var gotID, gotLine string
	rec.SetConsoleSink(func(serverID, line string) {
		gotID, gotLine = serverID, line
	})
	ctx := WithSource(context.Background(), "crash doctor")
	rec.Announce(ctx, "srv1", "doctor.disable", nil, "disabled %s", "bad.jar")

	if gotID != "srv1" || gotLine != "crash doctor: disabled bad.jar" {
		t.Fatalf("echo = %q %q", gotID, gotLine)
	}
}

func TestDefaultsAndTrace(t *testing.T) {
	if SourceFrom(context.Background()) != "panel" {
		t.Fatal("untagged context must read as panel")
	}
	ctx := WithTrace(context.Background())
	id := TraceFrom(ctx)
	if id == "" {
		t.Fatal("WithTrace must stamp an id")
	}
	if TraceFrom(WithTrace(ctx)) != id {
		t.Fatal("WithTrace must keep an existing id")
	}
}

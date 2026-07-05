package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nickheyer/discopanel/internal/config"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	tmp := t.TempDir()
	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	cfg.Database.Path = filepath.Join(tmp, "test.db")
	cfg.Storage.DataDir = tmp
	cfg.Storage.BackupDir = filepath.Join(tmp, "backups")
	store, err := NewSQLiteStore(cfg)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestMetricsHistoryRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Aligned to the bucket size so the first bucket holds a full window
	base := time.Now().UTC().Truncate(5 * time.Minute).Add(-30 * time.Minute)
	var batch []*MetricsSample
	for i := range 60 {
		batch = append(batch, &MetricsSample{
			ServerID:   "srv1",
			Timestamp:  base.Add(time.Duration(i) * 30 * time.Second),
			TPS:        20,
			MSPT:       12.5,
			Players:    i % 5,
			CPUPercent: 50,
			MemoryMB:   2048,
			DiskBytes:  1 << 30,
		})
	}
	if err := store.AddMetricsSamples(ctx, batch); err != nil {
		t.Fatalf("insert: %v", err)
	}

	raw, err := store.GetMetricsHistory(ctx, "srv1", base.Add(-time.Minute), time.Now(), 0)
	if err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if len(raw) != 60 {
		t.Fatalf("raw count = %d, want 60", len(raw))
	}
	if raw[0].TPS != 20 || raw[0].MemoryMB != 2048 {
		t.Fatalf("raw values wrong: %+v", raw[0])
	}

	bucketed, err := store.GetMetricsHistory(ctx, "srv1", base.Add(-time.Minute), time.Now(), 300)
	if err != nil {
		t.Fatalf("bucketed query: %v", err)
	}
	// Sixty 30s points cover 30 minutes, six or seven 5min buckets
	if len(bucketed) < 6 || len(bucketed) > 7 {
		t.Fatalf("bucket count = %d, want 6-7", len(bucketed))
	}
	if bucketed[0].TPS != 20 {
		t.Fatalf("bucket tps = %v, want 20", bucketed[0].TPS)
	}
	if bucketed[0].Players != 4 {
		t.Fatalf("bucket players = %d, want max 4", bucketed[0].Players)
	}
	if !bucketed[0].Timestamp.Before(bucketed[1].Timestamp) {
		t.Fatalf("buckets not ordered: %v then %v", bucketed[0].Timestamp, bucketed[1].Timestamp)
	}
}

func TestMetricsRollupAndPrune(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	old := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Hour)
	recent := time.Now().UTC().Add(-time.Minute)
	var batch []*MetricsSample
	for i := range 20 {
		batch = append(batch, &MetricsSample{
			ServerID:  "srv1",
			Timestamp: old.Add(time.Duration(i) * 30 * time.Second),
			TPS:       float64(10 + i%2*10), // alternates 10 and 20
			Players:   i,
		})
	}
	batch = append(batch, &MetricsSample{ServerID: "srv1", Timestamp: recent, TPS: 20})
	if err := store.AddMetricsSamples(ctx, batch); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := store.RollupMetricsSamples(ctx, time.Now().Add(-24*time.Hour), 300); err != nil {
		t.Fatalf("rollup: %v", err)
	}

	// Twenty 30s points cover 10 minutes, two 5min buckets remain
	var rolled []*MetricsSample
	if err := store.DB().Where("resolution = ?", 300).Find(&rolled).Error; err != nil {
		t.Fatalf("rolled query: %v", err)
	}
	if len(rolled) != 2 {
		t.Fatalf("rollup count = %d, want 2", len(rolled))
	}
	if rolled[0].TPS != 15 {
		t.Fatalf("rollup avg tps = %v, want 15", rolled[0].TPS)
	}
	if rolled[1].Players != 19 {
		t.Fatalf("rollup max players = %d, want 19", rolled[1].Players)
	}

	// Raw older than the cutoff is gone, the recent point survives
	var rawLeft []*MetricsSample
	if err := store.DB().Where("resolution = 0").Find(&rawLeft).Error; err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if len(rawLeft) != 1 {
		t.Fatalf("raw left = %d, want 1", len(rawLeft))
	}

	// A bucketed read spans rollups and raw without double counting
	all, err := store.GetMetricsHistory(ctx, "srv1", old.Add(-time.Hour), time.Now(), 300)
	if err != nil {
		t.Fatalf("bucketed query: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("bucketed span = %d, want 3", len(all))
	}

	if err := store.PruneMetricsSamples(ctx, 300, time.Now()); err != nil {
		t.Fatalf("prune: %v", err)
	}
	var count int64
	store.DB().Model(&MetricsSample{}).Where("resolution = ?", 300).Count(&count)
	if count != 0 {
		t.Fatalf("prune left %d rollups, want 0", count)
	}
}

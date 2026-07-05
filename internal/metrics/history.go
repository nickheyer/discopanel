package metrics

import (
	"context"
	"time"

	storage "github.com/nickheyer/discopanel/internal/db"
)

const (
	// Raw samples kept one day before rollup
	historyRawRetention = 24 * time.Hour
	// Rollup bucket width in seconds
	historyRollupSeconds = 300
	// Rollup samples kept thirty days
	historyRollupRetention = 30 * 24 * time.Hour
)

// Persists periodic snapshots and maintains the samples table
func (c *Collector) collectHistoryLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.collectorConfig.HistoryInterval)
	defer ticker.Stop()
	maintenance := time.NewTicker(time.Hour)
	defer maintenance.Stop()

	c.maintainHistory()

	for {
		select {
		case <-ticker.C:
			c.sampleHistory()
		case <-maintenance.C:
			c.maintainHistory()
		case <-c.stopChan:
			return
		}
	}
}

// Snapshots current metrics of alive servers into the samples table
func (c *Collector) sampleHistory() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	now := time.Now().UTC()
	var batch []*storage.MetricsSample
	for id, m := range c.GetAllMetrics() {
		// Lifecycle baseline only exists while the container is alive
		if !c.ServerAlive(id) {
			continue
		}
		batch = append(batch, &storage.MetricsSample{
			ServerID:   id,
			Timestamp:  now,
			TPS:        m.TPS,
			MSPT:       m.MSPT,
			Players:    m.PlayersOnline,
			CPUPercent: m.CPUPercent,
			MemoryMB:   m.MemoryUsage,
			DiskBytes:  m.DiskUsage,
		})
	}
	if err := c.store.AddMetricsSamples(ctx, batch); err != nil {
		c.log.Debug("Metrics history: failed to insert samples: %v", err)
	}
}

// Rolls up old raw samples and prunes expired rollups
func (c *Collector) maintainHistory() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := c.store.RollupMetricsSamples(ctx, time.Now().Add(-historyRawRetention), historyRollupSeconds); err != nil {
		c.log.Warn("Metrics history: rollup failed: %v", err)
	}
	if err := c.store.PruneMetricsSamples(ctx, historyRollupSeconds, time.Now().Add(-historyRollupRetention)); err != nil {
		c.log.Warn("Metrics history: prune failed: %v", err)
	}
}

// ServerAlive reports whether the container was alive at last check
func (c *Collector) ServerAlive(serverID string) bool {
	_, seen := c.getLifecycle(serverID)
	return seen
}

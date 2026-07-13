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

	traffic := c.takeProxyDeltas()

	now := time.Now().UTC()
	var batch []*storage.MetricsSample
	for id, m := range c.GetAllMetrics() {
		// Lifecycle baseline only exists while the container is alive
		if !c.ServerAlive(id) {
			continue
		}
		// Stale heap without a live agent must not record
		var heapUsed float64
		if m.AgentConnected {
			heapUsed = m.HeapUsedMB
		}
		t := traffic[id]
		batch = append(batch, &storage.MetricsSample{
			ServerID:         id,
			Timestamp:        now,
			TPS:              m.TPS,
			MSPT:             m.MSPT,
			Players:          m.PlayersOnline,
			CPUPercent:       m.CPUPercent,
			MemoryMB:         m.MemoryUsage,
			HeapUsedMB:       heapUsed,
			DiskBytes:        m.DiskUsage,
			ProxyActiveConns: t.ActiveConns,
			ProxyBytesIn:     t.BytesToBackend,
			ProxyBytesOut:    t.BytesToClient,
			ProxyLogins:      t.Logins,
		})
	}
	if err := c.store.AddMetricsSamples(ctx, batch); err != nil {
		c.log.Debug("Metrics history: failed to insert samples: %v", err)
	}
}

// Window deltas from monotonic proxy totals, resets clamp to zero
func (c *Collector) takeProxyDeltas() map[string]ProxyTraffic {
	if c.proxyTraffic == nil {
		return nil
	}
	totals := c.proxyTraffic()
	deltas := make(map[string]ProxyTraffic, len(totals))
	if c.lastProxyTotals == nil {
		c.lastProxyTotals = make(map[string]ProxyTraffic, len(totals))
	}
	clamp := func(cur, last int64) int64 {
		if d := cur - last; d > 0 {
			return d
		}
		return 0
	}
	for id, cur := range totals {
		last := c.lastProxyTotals[id]
		deltas[id] = ProxyTraffic{
			ActiveConns:    cur.ActiveConns,
			TotalConns:     clamp(cur.TotalConns, last.TotalConns),
			Logins:         clamp(cur.Logins, last.Logins),
			BytesToBackend: clamp(cur.BytesToBackend, last.BytesToBackend),
			BytesToClient:  clamp(cur.BytesToClient, last.BytesToClient),
		}
		c.lastProxyTotals[id] = cur
	}
	return deltas
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

// Reports whether the container was alive at last check
func (c *Collector) ServerAlive(serverID string) bool {
	_, seen := c.getLifecycle(serverID)
	return seen
}

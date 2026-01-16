package metrics

import "time"

// Collected metrics for a single server
type ServerMetrics struct {
	ServerID      string
	CPUPercent    float64
	MemoryUsage   float64 // MB
	DiskUsage     int64   // bytes
	DiskTotal     int64   // bytes
	PlayersOnline int
	TPS           float64
	LastUpdated   time.Time
}

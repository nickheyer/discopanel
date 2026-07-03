package lifecycle

import (
	"fmt"

	"github.com/nickheyer/discopanel/internal/autopilot"
	storage "github.com/nickheyer/discopanel/internal/db"
)

// applyMemoryGuardrail clamps the JVM heap when it would not fit inside the
// container memory limit (a heap at or above the limit gets the container
// OOM-killed mid-game). Only the in-memory config used for this start is
// touched; the stored configuration stays as the user set it, and the report
// card points at the real fix.
func (m *Manager) applyMemoryGuardrail(server *storage.Server, cfg *storage.ServerConfig) {
	if cfg == nil || server.Memory <= 0 {
		return
	}
	if cfg.AutoMemory != nil && *cfg.AutoMemory {
		return
	}

	heapMB := autopilot.ParseMemoryMB(strPtrVal(cfg.MaxMemory))
	if heapMB == 0 {
		heapMB = autopilot.ParseMemoryMB(strPtrVal(cfg.Memory))
	}
	clamped, wasClamped := autopilot.ClampHeapForLimit(server.Memory, heapMB)
	if !wasClamped {
		return
	}

	value := fmt.Sprintf("%dM", clamped)
	cfg.MaxMemory = &value
	cfg.Memory = nil
	if autopilot.ParseMemoryMB(strPtrVal(cfg.InitMemory)) > clamped {
		cfg.InitMemory = &value
	}
	m.log.Warn("lifecycle: %s heap %dMB exceeds container limit %dMB, clamping to %dMB for this start",
		server.Name, heapMB, server.Memory, clamped)
	m.console(server.ID,
		"heap size %dMB does not fit the %dMB container limit, clamped to %dMB for this start (enable Automatic Memory or raise the server memory)",
		heapMB, server.Memory, clamped)
}

func strPtrVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

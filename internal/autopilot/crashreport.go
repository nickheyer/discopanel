package autopilot

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/metrics"
	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
)

// Reports are small, the cap only guards against garbage files
const maxCrashReportBytes = 2 << 20

// Typed capture wins, the crash report file is the universal floor
func effectiveFatal(server *storage.Server, m *metrics.ServerMetrics) *agentv1.FatalError {
	fatal := m.LastFatalError
	if len(fatal.GetFailedMods()) > 0 {
		return fatal
	}
	failed := reportVerdicts(server.DataPath, m.LastCrashReportPath)
	if len(failed) == 0 {
		return fatal
	}
	merged := &agentv1.FatalError{FailedMods: failed}
	if fatal != nil {
		merged.Thread = fatal.GetThread()
		merged.Uncaught = fatal.GetUncaught()
		merged.Causes = fatal.GetCauses()
	}
	return merged
}

// Reads the crash report file text from disk
func readCrashReport(dataPath, reportPath string) string {
	if dataPath == "" || reportPath == "" {
		return ""
	}
	f, err := os.Open(filepath.Join(dataPath, reportPath))
	if err != nil {
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCrashReportBytes))
	if err != nil {
		return ""
	}
	return string(data)
}

// Extracts loader-blamed mods from the crash report on disk
func reportVerdicts(dataPath, reportPath string) []*agentv1.FailedMod {
	text := readCrashReport(dataPath, reportPath)
	if text == "" {
		return nil
	}
	return parseReportMods(text)
}

// Reads the loader's own issue blocks, no loader named anywhere
func parseReportMods(text string) []*agentv1.FailedMod {
	var mods []*agentv1.FailedMod
	var cur *agentv1.FailedMod
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(trimmed, "-- Mod loading issue for: "); ok {
			id := strings.TrimSpace(strings.TrimSuffix(rest, "--"))
			if id == "" {
				cur = nil
				continue
			}
			cur = &agentv1.FailedMod{ModId: id}
			mods = append(mods, cur)
			continue
		}
		// Any other section header ends the current block
		if strings.HasPrefix(trimmed, "-- ") {
			cur = nil
			continue
		}
		if cur == nil {
			continue
		}
		if v, ok := strings.CutPrefix(trimmed, "Mod file:"); ok && cur.FileName == "" {
			cur.FileName = strings.TrimSpace(v)
		}
		if v, ok := strings.CutPrefix(trimmed, "Exception message:"); ok && cur.ErrorMessage == "" {
			cur.ErrorMessage = strings.TrimSpace(v)
		}
	}
	return mods
}

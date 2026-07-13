package autopilot

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/metrics"
	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
)

// Reports are small, the cap only guards against garbage files
const maxCrashReportBytes = 2 << 20

// Typed capture wins, report file is the floor
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

// One cached crash report keyed by mtime and size
type crashReportEntry struct {
	mtime time.Time
	size  int64
	text  string
}

const maxCrashReportEntries = 32

var (
	crashReportMu    sync.Mutex
	crashReportCache = map[string]crashReportEntry{}
)

// Reads the crash report text, memoized on path and mtime
func readCrashReport(dataPath, reportPath string) string {
	if dataPath == "" || reportPath == "" {
		return ""
	}
	full := filepath.Join(dataPath, reportPath)
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	crashReportMu.Lock()
	e, ok := crashReportCache[full]
	crashReportMu.Unlock()
	if ok && e.mtime.Equal(info.ModTime()) && e.size == info.Size() {
		return e.text
	}
	f, err := os.Open(full)
	if err != nil {
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCrashReportBytes))
	if err != nil {
		return ""
	}
	text := string(data)
	crashReportMu.Lock()
	if len(crashReportCache) >= maxCrashReportEntries {
		for k := range crashReportCache {
			delete(crashReportCache, k)
			break
		}
	}
	crashReportCache[full] = crashReportEntry{mtime: info.ModTime(), size: info.Size(), text: text}
	crashReportMu.Unlock()
	return text
}

// Extracts loader-blamed mods from the crash report on disk
func reportVerdicts(dataPath, reportPath string) []*agentv1.FailedMod {
	text := readCrashReport(dataPath, reportPath)
	if text == "" {
		return nil
	}
	return parseReportMods(text)
}

// Reads loader issue idioms from forge and fabric reports
func parseReportMods(text string) []*agentv1.FailedMod {
	if mods := parseForgeIssueMods(text); len(mods) > 0 {
		return mods
	}
	return parseFabricResolutionMods(text)
}

// Reads forge family issue blocks by section header
func parseForgeIssueMods(text string) []*agentv1.FailedMod {
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

// Roster lines pairing mod id with display name
var fabricRosterEntry = regexp.MustCompile(`^([a-z][a-z0-9_-]*): .+`)

// Mod references shaped like "Mod 'Sodium' (sodium)"
var fabricModRef = regexp.MustCompile(`[Mm]od '[^']*' \(([a-z][a-z0-9_-]*)\)`)

// Reads fabric family resolution failures from the report text
func parseFabricResolutionMods(text string) []*agentv1.FailedMod {
	roster := fabricModRoster(text)
	if len(roster) == 0 {
		return nil
	}
	var mods []*agentv1.FailedMod
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		if !strings.Contains(trimmed, "requires") && !strings.Contains(trimmed, "is incompatible with") {
			continue
		}
		match := fabricModRef.FindStringSubmatch(trimmed)
		if match == nil {
			continue
		}
		id := match[1]
		if !roster[id] || seen[id] {
			continue
		}
		seen[id] = true
		fm := &agentv1.FailedMod{ModId: id, ErrorMessage: trimmed, Reason: "mod_error"}
		if strings.Contains(trimmed, "requires") {
			fm.Reason = "missing_dependency"
		}
		mods = append(mods, fm)
	}
	return mods
}

// Collects ids from the report's fabric or quilt mod listing
func fabricModRoster(text string) map[string]bool {
	roster := map[string]bool{}
	in := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Fabric Mods:") || strings.HasPrefix(trimmed, "Quilt Mods:") {
			in = true
			continue
		}
		if !in {
			continue
		}
		match := fabricRosterEntry.FindStringSubmatch(trimmed)
		if match == nil {
			in = false
			continue
		}
		roster[match[1]] = true
	}
	return roster
}

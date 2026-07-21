package main

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nickheyer/discopanel/pkg/minecraft"
	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	crashLoopWindow    = 10 * time.Minute
	crashLoopThreshold = 3
	maxDoctorPasses    = 8
	minDisableBudget   = 8
	incidentStaleAfter = 6 * time.Hour
	verifyQuietFor     = 90 * time.Second
)

// Caps how much of a pack one incident may disable
func disableBudget(installed int) int {
	if b := installed / 10; b > minDisableBudget {
		return b
	}
	return minDisableBudget
}

// One server the doctor watches, facts from the panel API
type serverInfo struct {
	ID        string
	Name      string
	DataPath  string // Local path inside this container
	ModLoader v1.ModLoader
	McVersion string
	Running   bool
	Stopped   bool // Panel intent, wanted stops stay stopped
}

// Acts on servers through the panel API
type panelActor interface {
	Restart(ctx context.Context, serverID string) error
	Stop(ctx context.Context, serverID string) error
	ForcePatterns(ctx context.Context, serverID string) []string
}

// Repairs one server per pass, journal on the shared volume
type engine struct {
	panel     panelActor
	installer *depInstaller // Nil when dep installs are off
	logf      func(format string, args ...any)
}

// Handles every unprocessed exit, reports whether work happened
func (e *engine) checkServer(ctx context.Context, srv *serverInfo) {
	history := runtimespec.ReadExitHistory(srv.DataPath)
	j := runtimespec.LoadDoctor(srv.DataPath)

	newest := int64(0)
	for i := range history {
		if history[i].ExitedAtUnixMs > newest {
			newest = history[i].ExitedAtUnixMs
		}
	}

	if newest > j.LastHandledMs {
		exit := latestExit(history)
		j.LastHandledMs = newest
		e.respond(ctx, srv, j, exit, history)
		return
	}

	// Quiet running server verifies any open repair
	if j.Incident != nil && srv.Running && time.Since(lastTouch(j.Incident)) > verifyQuietFor {
		e.resolve(srv, j)
	}
}

func latestExit(history []*agentv1.Exited) *agentv1.Exited {
	var latest *agentv1.Exited
	for i := range history {
		if latest == nil || history[i].ExitedAtUnixMs > latest.ExitedAtUnixMs {
			latest = history[i]
		}
	}
	return latest
}

func lastTouch(inc *v1.DoctorIncident) time.Time {
	return runtimespec.LastActivity(inc).AsTime()
}

// Counts loop-evidence exits inside the window
func exitsWithin(history []*agentv1.Exited, window time.Duration) int {
	cutoff := time.Now().Add(-window).UnixMilli()
	n := 0
	for i := range history {
		if history[i].ExitedAtUnixMs > cutoff && loopEvidence(history[i]) {
			n++
		}
	}
	return n
}

// Requested stops that did not crash are supervisor artifacts
func loopEvidence(e *agentv1.Exited) bool {
	return e.Crashed || !e.StopRequested
}

// Runs one repair pass for a fresh exit
func (e *engine) respond(ctx context.Context, srv *serverInfo, j *v1.DoctorState, exit *agentv1.Exited, history []*agentv1.Exited) {
	exitedAt := time.UnixMilli(exit.ExitedAtUnixMs)
	if time.Since(exitedAt) > crashLoopWindow {
		e.saveJournal(srv, j)
		return
	}

	// Requested stops are supervisor artifacts, never loop evidence
	if !loopEvidence(exit) {
		e.saveJournal(srv, j)
		return
	}

	// A wanted stop stays stopped, repairs resume on next start
	if srv.Stopped {
		e.logf("%s: crash while panel wants it stopped, standing down", srv.Name)
		e.saveJournal(srv, j)
		return
	}

	// Only crashes are repairable, clean loops just get broken
	if !exit.Crashed || exit.OomKilled {
		e.saveJournal(srv, j)
		e.breakCrashLoop(ctx, srv, history)
		return
	}

	modsDir := minecraft.GetModsPath(srv.DataPath, srv.ModLoader)

	// A long quiet gap means a new episode, pass count resets
	if j.Incident != nil && time.Since(runtimespec.LastActivity(j.Incident).AsTime()) > incidentStaleAfter {
		e.logf("%s: incident went stale, resetting pass count", srv.Name)
		j.Incident.Passes = 0
	}
	opened := false
	if j.Incident == nil {
		j.Incident = &v1.DoctorIncident{
			OpenedAt: timestamppb.Now(),
			Budget:   int32(disableBudget(len(minecraft.ScanModsDir(modsDir)))),
		}
		opened = true
	}
	inc := j.Incident
	inc.Passes++
	if cause := classifyFatal(effectiveFatal(srv, exit)); cause != "" {
		inc.Cause = cause
	}

	if inc.Passes > maxDoctorPasses {
		e.exhaust(ctx, srv, j, "too many repair attempts")
		return
	}

	force := e.panel.ForcePatterns(ctx, srv.ID)
	actions := e.plan(srv, exit, modsDir, force, inc)
	if len(actions) == 0 {
		if opened || len(inc.Actions) == 0 {
			// Nothing to try, this crash is not repairable here
			j.Incident = nil
			e.saveJournal(srv, j)
			e.breakCrashLoop(ctx, srv, history)
			return
		}
		e.exhaust(ctx, srv, j, "no repair helped")
		return
	}
	if runtimespec.DisabledCount(inc)+plannedDisables(actions) > int(inc.Budget) {
		e.exhaust(ctx, srv, j, "too many mods would be disabled")
		return
	}

	for _, a := range actions {
		e.apply(ctx, srv, modsDir, a, inc)
	}
	// Live summary lets the panel narrate open incidents
	inc.Summary = summarizeIncident(inc)
	e.saveJournal(srv, j)

	e.logf("%s: restarting to verify the repair (attempt %d of %d)", srv.Name, inc.Passes, maxDoctorPasses)
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
	defer cancel()
	if err := e.panel.Restart(rctx, srv.ID); err != nil {
		e.logf("%s: restart failed: %v", srv.Name, err)
	}
}

// Closes an open incident as repaired after a verified boot
func (e *engine) resolve(srv *serverInfo, j *v1.DoctorState) {
	inc := j.Incident

	// Boot verified, live disables become durable excludes
	for _, a := range inc.Actions {
		if a.Reverted {
			continue
		}
		switch a.Kind {
		case v1.DoctorActionKind_DOCTOR_ACTION_KIND_DISABLE:
			appendExclude(j, a.File)
		case v1.DoctorActionKind_DOCTOR_ACTION_KIND_ENABLE:
			removeExclude(j, a.File)
		case v1.DoctorActionKind_DOCTOR_ACTION_KIND_DISABLE_PACK:
			appendExclude(j, filepath.Base(a.File))
		}
	}

	inc.Outcome = v1.DoctorOutcome_DOCTOR_OUTCOME_REPAIRED
	inc.ClosedAt = timestamppb.Now()
	inc.Summary = summarizeIncident(inc)
	j.Resolved, j.Incident = inc, nil
	e.saveJournal(srv, j)
	e.logf("%s: server is up, incident resolved (%s)", srv.Name, inc.Summary)
}

func appendExclude(j *v1.DoctorState, file string) {
	if slices.Contains(j.Excludes, file) {
		return
	}
	j.Excludes = append(j.Excludes, file)
}

func removeExclude(j *v1.DoctorState, file string) {
	kept := j.Excludes[:0]
	for _, f := range j.Excludes {
		if f != file {
			kept = append(kept, f)
		}
	}
	j.Excludes = kept
}

func summarizeIncident(inc *v1.DoctorIncident) string {
	var disabled, enabled, installed, packs []string
	for _, a := range inc.Actions {
		if a.Reverted {
			continue
		}
		switch a.Kind {
		case v1.DoctorActionKind_DOCTOR_ACTION_KIND_DISABLE:
			disabled = append(disabled, a.File)
		case v1.DoctorActionKind_DOCTOR_ACTION_KIND_ENABLE:
			enabled = append(enabled, a.File)
		case v1.DoctorActionKind_DOCTOR_ACTION_KIND_INSTALL:
			installed = append(installed, a.File)
		case v1.DoctorActionKind_DOCTOR_ACTION_KIND_DISABLE_PACK:
			packs = append(packs, filepath.Base(a.File))
		}
	}
	var parts []string
	if len(disabled) > 0 {
		parts = append(parts, "disabled "+strings.Join(disabled, ", "))
	}
	if len(packs) > 0 {
		parts = append(parts, "disabled data pack "+strings.Join(packs, ", "))
	}
	if len(enabled) > 0 {
		parts = append(parts, "re-enabled "+strings.Join(enabled, ", "))
	}
	if len(installed) > 0 {
		parts = append(parts, "installed "+strings.Join(installed, ", "))
	}
	if len(parts) == 0 {
		return "no changes were needed"
	}
	return strings.Join(parts, ", ")
}

func plannedDisables(actions []*v1.DoctorAction) int {
	n := 0
	for i := range actions {
		if actions[i].Kind == v1.DoctorActionKind_DOCTOR_ACTION_KIND_DISABLE {
			n++
		}
	}
	return n
}

// Executes one action on disk and journals it
func (e *engine) apply(ctx context.Context, srv *serverInfo, modsDir string, a *v1.DoctorAction, inc *v1.DoctorIncident) bool {
	a.AppliedAt = timestamppb.Now()
	runtimespec.MarkTried(inc, runtimespec.ActionKey(a))

	switch a.Kind {
	case v1.DoctorActionKind_DOCTOR_ACTION_KIND_DISABLE:
		if err := minecraft.DisableModJar(modsDir, a.File); err != nil {
			e.logf("%s: could not disable %s: %v", srv.Name, a.File, err)
			return false
		}
		e.logf("%s: disabled %s (%s)", srv.Name, a.File, a.Reason)
	case v1.DoctorActionKind_DOCTOR_ACTION_KIND_ENABLE:
		if err := minecraft.EnableModJar(modsDir, a.File); err != nil {
			e.logf("%s: could not re-enable %s: %v", srv.Name, a.File, err)
			return false
		}
		e.logf("%s: re-enabled %s (%s)", srv.Name, a.File, a.Reason)
	case v1.DoctorActionKind_DOCTOR_ACTION_KIND_DISABLE_PACK:
		if err := minecraft.DisableDatapack(srv.DataPath, a.File); err != nil {
			e.logf("%s: could not disable data pack %s: %v", srv.Name, a.File, err)
			return false
		}
		e.logf("%s: disabled data pack %s (%s)", srv.Name, a.File, a.Reason)
	case v1.DoctorActionKind_DOCTOR_ACTION_KIND_INSTALL:
		if e.installer == nil {
			return false
		}
		file, err := e.installer.Install(ctx, srv, modsDir, a.ModId, a.Range, a.Dialect)
		if err != nil {
			e.logf("%s: could not source %s: %v", srv.Name, a.ModId, err)
			return false
		}
		a.File = file
		e.logf("%s: installed missing dependency %s (%s)", srv.Name, a.ModId, file)
	}
	inc.Actions = append(inc.Actions, a)
	return true
}

// Undoes one live action on disk, true when undone
func (e *engine) undoAction(srv *serverInfo, modsDir string, a *v1.DoctorAction) bool {
	switch a.Kind {
	case v1.DoctorActionKind_DOCTOR_ACTION_KIND_DISABLE:
		if err := minecraft.EnableModJar(modsDir, a.File); err != nil {
			if fileExists(filepath.Join(modsDir, a.File)) {
				return true
			}
			e.logf("%s: could not restore %s: %v", srv.Name, a.File, err)
			return false
		}
	case v1.DoctorActionKind_DOCTOR_ACTION_KIND_ENABLE:
		if err := minecraft.DisableModJar(modsDir, a.File); err != nil {
			e.logf("%s: could not restore %s: %v", srv.Name, a.File, err)
			return false
		}
	case v1.DoctorActionKind_DOCTOR_ACTION_KIND_DISABLE_PACK:
		if err := minecraft.EnableDatapack(srv.DataPath, a.File); err != nil {
			e.logf("%s: could not restore %s: %v", srv.Name, a.File, err)
			return false
		}
	case v1.DoctorActionKind_DOCTOR_ACTION_KIND_INSTALL:
		if a.File != "" {
			if err := removeFile(filepath.Join(modsDir, a.File)); err != nil {
				e.logf("%s: could not remove %s: %v", srv.Name, a.File, err)
				return false
			}
		}
	}
	return true
}

// Rolls back every live action, newest first
func (e *engine) revertAll(srv *serverInfo, modsDir string, inc *v1.DoctorIncident) {
	for i := len(inc.Actions) - 1; i >= 0; i-- {
		a := inc.Actions[i]
		if a.Reverted {
			continue
		}
		if e.undoAction(srv, modsDir, a) {
			a.Reverted = true
		}
	}
}

// Undoes guesses the recurring crash proves wrong
// A guess for another crash signature stays in effect
func (e *engine) revertGuesses(srv *serverInfo, modsDir string, inc *v1.DoctorIncident, signature string) {
	for i := len(inc.Actions) - 1; i >= 0; i-- {
		a := inc.Actions[i]
		if a.Reverted || a.Kind != v1.DoctorActionKind_DOCTOR_ACTION_KIND_DISABLE || a.Evidence != v1.DoctorEvidence_DOCTOR_EVIDENCE_FRAME {
			continue
		}
		if a.Cause != signature {
			continue
		}
		if e.undoAction(srv, modsDir, a) {
			a.Reverted = true
		}
	}
}

// Gives up honestly, restores the pack, and stops the server
func (e *engine) exhaust(ctx context.Context, srv *serverInfo, j *v1.DoctorState, why string) {
	inc := j.Incident
	modsDir := minecraft.GetModsPath(srv.DataPath, srv.ModLoader)
	e.revertAll(srv, modsDir, inc)

	inc.Outcome = v1.DoctorOutcome_DOCTOR_OUTCOME_GAVE_UP
	inc.ClosedAt = timestamppb.Now()
	inc.Summary = why + ", all changes were undone"
	j.Resolved, j.Incident = inc, nil
	e.saveJournal(srv, j)
	e.logf("%s: gave up (%s), stopping the server", srv.Name, why)

	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
	defer cancel()
	if err := e.panel.Stop(sctx, srv.ID); err != nil {
		e.logf("%s: stop failed: %v", srv.Name, err)
	}
}

// Stops a server exiting over and over to break the loop
func (e *engine) breakCrashLoop(ctx context.Context, srv *serverInfo, history []*agentv1.Exited) {
	n := exitsWithin(history, crashLoopWindow)
	if n < crashLoopThreshold {
		return
	}
	e.logf("%s: %d exits in %d minutes, stopping to break the loop",
		srv.Name, n, int(crashLoopWindow.Minutes()))
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
	defer cancel()
	if err := e.panel.Stop(sctx, srv.ID); err != nil {
		e.logf("%s: loop break stop failed: %v", srv.Name, err)
	}
}

func (e *engine) saveJournal(srv *serverInfo, j *v1.DoctorState) {
	if err := runtimespec.SaveDoctor(srv.DataPath, j); err != nil {
		e.logf("%s: journal save failed: %v", srv.Name, err)
	}
}

// Excludes from the journal, files the doctor keeps out
func journalExcludes(srv *serverInfo) []string {
	return runtimespec.DoctorExcludes(srv.DataPath)
}

// Maps the loader's failure key onto a remedy class
func classifyFailedMod(fm *agentv1.FailedMod) v1.FailedModClass {
	key := strings.ToLower(fm.GetReason())
	switch {
	case strings.Contains(key, "missingdependency"),
		strings.Contains(key, "missing_dependency"):
		return v1.FailedModClass_FAILED_MOD_CLASS_MISSING_DEPENDENCY
	case strings.Contains(key, "dupedmod"), strings.Contains(key, "duplicate"):
		return v1.FailedModClass_FAILED_MOD_CLASS_DUPLICATE
	}
	if simpleTypeName(fm.GetErrorType()) == "UnsupportedClassVersionError" {
		return v1.FailedModClass_FAILED_MOD_CLASS_JAVA_VERSION
	}
	return v1.FailedModClass_FAILED_MOD_CLASS_MOD_ERROR
}

// Human summary of the incident for the status page
func incidentLine(inc *v1.DoctorIncident) string {
	if inc == nil {
		return ""
	}
	return fmt.Sprintf("pass %s of %s, %s", strconv.Itoa(int(inc.Passes)), strconv.Itoa(maxDoctorPasses), summarizeIncident(inc))
}

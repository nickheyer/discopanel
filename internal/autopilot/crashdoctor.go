package autopilot

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nickheyer/discopanel/internal/activity"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/metrics"
	"github.com/nickheyer/discopanel/internal/minecraft"
	"github.com/nickheyer/discopanel/pkg/logger"
	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
)

type crashModRef struct {
	ModID   string
	ModFile string
}

type crashDiagnosis struct {
	Cause string
	Mods  []crashModRef
}

var mixinFramePattern = regexp.MustCompile(`(?:handler|localvar|redirect|modify|constant|args|wrapoperation|wrapwithcondition|bridge)\$[A-Za-z0-9]+\$([A-Za-z0-9_]+)\$`)

func diagnoseFatal(fatal *agentv1.FatalError, modsDir string) *crashDiagnosis {
	if fatal == nil || (len(fatal.GetCauses()) == 0 && len(fatal.GetFailedMods()) == 0) {
		return nil
	}

	var metas []minecraft.ModJarMeta
	if modsDir != "" {
		metas = minecraft.ScanModsDir(modsDir)
	}

	d := &crashDiagnosis{Cause: classifyFatal(fatal)}

	if mods := resolveFailedMods(fatal.GetFailedMods(), metas); len(mods) > 0 {
		d.Mods = mods
		return d
	}

	if len(metas) > 0 {
		if id, file := attributeFatal(fatal, metas); id != "" || file != "" {
			d.Mods = []crashModRef{{ModID: id, ModFile: file}}
		}
	}
	if d.Cause == "" && len(d.Mods) == 0 {
		return nil
	}
	return d
}

func resolveFailedMods(failed []*agentv1.FailedMod, metas []minecraft.ModJarMeta) []crashModRef {
	var mods []crashModRef
	seen := make(map[string]bool)
	for _, fm := range failed {
		ref := resolveFailedMod(fm, metas)
		if ref.ModID == "" && ref.ModFile == "" {
			continue
		}
		key := ref.ModID + "|" + ref.ModFile
		if seen[key] {
			continue
		}
		seen[key] = true
		mods = append(mods, ref)
	}
	return mods
}

// Maps one loader-blamed mod onto an installed jar
func resolveFailedMod(fm *agentv1.FailedMod, metas []minecraft.ModJarMeta) crashModRef {
	ref := crashModRef{ModID: fm.GetModId()}
	if name := path.Base(strings.ReplaceAll(fm.GetFileName(), "\\", "/")); name != "" && name != "." {
		for i := range metas {
			if metas[i].FileName == name {
				ref.ModFile = name
				break
			}
		}
	}
	if ref.ModFile == "" && ref.ModID != "" {
		for i := range metas {
			if metas[i].HasModID(ref.ModID) {
				ref.ModFile = metas[i].FileName
				break
			}
		}
	}
	return ref
}

func classifyFatal(fatal *agentv1.FatalError) string {
	for _, c := range fatal.GetCauses() {
		switch simpleTypeName(c.GetType()) {
		case "OutOfMemoryError":
			return "The JVM ran out of memory. Raise the server memory or lower the heap."
		case "UnsupportedClassVersionError":
			return "A mod was built for a newer Java version than this server runs."
		case "DuplicateModsFoundException":
			return "Two mod files provide the same mod. Remove the older duplicate from the mods folder."
		case "LoadingFailedException", "ModLoadingException":
			return "The mod loader reported mods that cannot load on a dedicated server."
		}
	}
	for _, c := range fatal.GetCauses() {
		for _, f := range c.GetFrames() {
			if simpleTypeName(f.GetClassName()) == "RuntimeDistCleaner" {
				return "A client-only mod tried to load client code that does not exist on a dedicated server."
			}
		}
	}
	for _, fm := range fatal.GetFailedMods() {
		if strings.Contains(fm.GetErrorMessage(), "for invalid dist DEDICATED_SERVER") {
			return "A client-only mod tried to load client code that does not exist on a dedicated server."
		}
	}
	return ""
}

func simpleTypeName(t string) string {
	if idx := strings.LastIndexByte(t, '.'); idx >= 0 {
		return t[idx+1:]
	}
	return t
}

func attributeFatal(fatal *agentv1.FatalError, metas []minecraft.ModJarMeta) (modID, fileName string) {
	causes := fatal.GetCauses()
	for i := len(causes) - 1; i >= 0; i-- {
		for _, frame := range causes[i].GetFrames() {
			if m := mixinFramePattern.FindStringSubmatch(frame.GetMethodName() + "$"); m != nil {
				for j := range metas {
					if metas[j].HasModID(m[1]) {
						return m[1], metas[j].FileName
					}
				}
			}
			if jar := jarFromLocation(frame.GetSourceLocation()); jar != "" {
				for j := range metas {
					if metas[j].FileName == jar {
						id := ""
						if len(metas[j].Mods) > 0 {
							id = metas[j].Mods[0].ID
						}
						return id, jar
					}
				}
			}
		}
	}
	return "", ""
}

func jarFromLocation(loc string) string {
	if loc == "" {
		return ""
	}
	if idx := strings.IndexByte(loc, '!'); idx >= 0 {
		loc = loc[:idx]
	}
	if idx := strings.Index(loc, "%23"); idx >= 0 {
		loc = loc[:idx]
	}
	base := path.Base(strings.TrimSuffix(loc, "/"))
	if !strings.HasSuffix(strings.ToLower(base), ".jar") {
		return ""
	}
	return base
}

// Typed classifications of one loader-blamed mod failure
type failReason string

const (
	failMissingDep failReason = "missing_dependency"
	failDuplicate  failReason = "duplicate"
	failJava       failReason = "java_version"
	failModError   failReason = "mod_error"
)

// Maps the loader's failure key onto a remedy class
func classifyFailedMod(fm *agentv1.FailedMod) failReason {
	key := strings.ToLower(fm.GetReason())
	switch {
	case strings.Contains(key, "missingdependency"),
		strings.Contains(key, "missing_dependency"):
		return failMissingDep
	case strings.Contains(key, "dupedmod"), strings.Contains(key, "duplicate"):
		return failDuplicate
	}
	if simpleTypeName(fm.GetErrorType()) == "UnsupportedClassVersionError" {
		return failJava
	}
	return failModError
}

const (
	crashLoopThreshold = 3
	maxDoctorPasses    = 8
	minDisableBudget   = 8
	repairTimeout      = 15 * time.Minute
)

// Caps how much of a pack one incident may disable
func disableBudget(installed int) int {
	if b := installed / 10; b > minDisableBudget {
		return b
	}
	return minDisableBudget
}

// Ledger source tag for everything the doctor does
const doctorSource = "crash doctor"

type CrashLifecycle interface {
	Stop(ctx context.Context, serverID string) error
	Restart(ctx context.Context, serverID string) error
	StopRequestedBy(serverID string) string
}

type CrashStore interface {
	GetServer(ctx context.Context, id string) (*storage.Server, error)
	GetServerProperties(ctx context.Context, id string) (*storage.ServerProperties, error)
	SaveServerProperties(ctx context.Context, cfg *storage.ServerProperties) error
}

// Sources a missing dependency into the mods dir by mod id
type DepInstaller interface {
	InstallModByID(ctx context.Context, server *storage.Server, modID, versionRange string, dialects []string) (string, error)
}

type CrashResponder struct {
	Store     CrashStore
	Collector *metrics.Collector
	Lifecycle CrashLifecycle
	Installer DepInstaller
	Rec       *activity.Recorder
	Log       *logger.Logger

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// One repair or adopt runs per server at a time
func (r *CrashResponder) serverLock(serverID string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.locks == nil {
		r.locks = make(map[string]*sync.Mutex)
	}
	if r.locks[serverID] == nil {
		r.locks[serverID] = &sync.Mutex{}
	}
	return r.locks[serverID]
}

// Repairs run off the stream handler, installs may take a while
func (r *CrashResponder) OnCrashExit(ctx context.Context, serverID string) {
	_ = ctx
	go r.respond(serverID)
}

// Trace id shared by every ledger event of one incident
func incidentTrace(inc *doctorIncident) string {
	return "incident-" + strconv.FormatInt(inc.OpenedAt.UnixMilli(), 10)
}

// Closes an open incident as repaired once the server boots
func (r *CrashResponder) OnServerReady(ctx context.Context, serverID string) {
	lock := r.serverLock(serverID)
	lock.Lock()
	defer lock.Unlock()

	server, err := r.Store.GetServer(ctx, serverID)
	if err != nil {
		return
	}
	j := loadDoctor(server.DataPath)
	inc := j.Incident
	if inc == nil {
		return
	}
	ctx = activity.WithTraceID(activity.WithSource(ctx, "crash doctor"), incidentTrace(inc))
	cfg, err := r.Store.GetServerProperties(ctx, serverID)
	if err != nil {
		return
	}

	// Boot verified, adopted disables become durable excludes
	for i := range inc.Actions {
		a := &inc.Actions[i]
		if a.Reverted {
			continue
		}
		switch a.Kind {
		case actionDisable:
			minecraft.AppendPackExclude(server.ModLoader, cfg, a.File)
		case actionEnable:
			minecraft.RemovePackExclude(server.ModLoader, cfg, a.File)
		case actionDisablePack:
			minecraft.AppendPackExclude(server.ModLoader, cfg, filepath.Base(a.File))
		}
	}
	if err := r.Store.SaveServerProperties(ctx, cfg); err != nil {
		r.Log.Error("autopilot: crash doctor could not save pack excludes for server %s: %v", serverID, err)
	}

	inc.Outcome = "repaired"
	inc.Summary = summarizeIncident(inc)
	j.Resolved, j.Incident = inc, nil
	if err := saveDoctor(server.DataPath, j); err != nil {
		r.Log.Error("autopilot: crash doctor journal save failed for server %s: %v", serverID, err)
	}

	r.Collector.RecordAutoRepair(serverID, inc.Summary)
	r.Rec.Announce(ctx, serverID, "doctor.resolve", activity.Attrs{"summary": inc.Summary, "passes": strconv.Itoa(inc.Passes)}, "server is up (%s)", inc.Summary)
	r.Log.Info("autopilot: crash doctor resolved incident for server %s (%s)", serverID, inc.Summary)
}

func summarizeIncident(inc *doctorIncident) string {
	var disabled, enabled, installed, packs []string
	for i := range inc.Actions {
		a := &inc.Actions[i]
		if a.Reverted {
			continue
		}
		switch a.Kind {
		case actionDisable:
			disabled = append(disabled, a.File)
		case actionEnable:
			enabled = append(enabled, a.File)
		case actionInstall:
			installed = append(installed, a.File)
		case actionDisablePack:
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

func (r *CrashResponder) respond(serverID string) {
	lock := r.serverLock(serverID)
	lock.Lock()
	defer lock.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), repairTimeout)
	defer cancel()
	ctx = activity.WithSource(ctx, doctorSource)

	m := r.Collector.GetMetrics(serverID)
	if m == nil || !m.LastExitCrashed {
		return
	}
	// Memory exits are a sizing problem, not a mod problem
	if m.LastExitOomKilled {
		r.breakCrashLoop(serverID)
		return
	}
	server, err := r.Store.GetServer(ctx, serverID)
	if err != nil {
		return
	}
	// A wanted stop stays stopped, the user overrides the doctor
	if src := r.userStop(serverID); src != "" {
		r.standDown(ctx, serverID, server, src)
		return
	}
	cfg, err := r.Store.GetServerProperties(ctx, serverID)
	if err != nil {
		return
	}
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if modsDir == "" {
		r.breakCrashLoop(serverID)
		return
	}

	j := loadDoctor(server.DataPath)
	opened := false
	if j.Incident == nil {
		j.Incident = &doctorIncident{
			OpenedAt: time.Now(),
			Budget:   disableBudget(len(minecraft.ScanModsDir(modsDir))),
		}
		opened = true
	}
	inc := j.Incident
	inc.Passes++
	ctx = activity.WithTraceID(ctx, incidentTrace(inc))

	if inc.Passes > maxDoctorPasses {
		r.exhaust(ctx, serverID, server, j, "too many repair attempts")
		return
	}

	actions := r.plan(server, cfg, m, modsDir, inc)
	if len(actions) == 0 {
		if opened || len(inc.Actions) == 0 {
			// Nothing to try, this crash is not repairable here
			j.Incident = nil
			_ = saveDoctor(server.DataPath, j)
			r.breakCrashLoop(serverID)
			return
		}
		r.exhaust(ctx, serverID, server, j, "no repair helped")
		return
	}
	if inc.disabledCount()+plannedDisables(actions) > inc.Budget {
		r.exhaust(ctx, serverID, server, j, "too many mods would be disabled")
		return
	}

	for _, a := range actions {
		if !r.apply(ctx, serverID, server, modsDir, a, inc) {
			continue
		}
	}
	if err := saveDoctor(server.DataPath, j); err != nil {
		r.Log.Error("autopilot: crash doctor journal save failed for server %s: %v", serverID, err)
	}

	// The user may have stopped it while this pass repaired
	if src := r.userStop(serverID); src != "" {
		r.standDown(ctx, serverID, server, src)
		return
	}
	r.Rec.Announce(ctx, serverID, "doctor.restart", activity.Attrs{"attempt": strconv.Itoa(inc.Passes), "max_attempts": strconv.Itoa(maxDoctorPasses)}, "restarting to verify the repair (attempt %d of %d)", inc.Passes, maxDoctorPasses)
	if err := r.Lifecycle.Restart(ctx, serverID); err != nil {
		r.Log.Error("autopilot: crash doctor restart failed for server %s: %v", serverID, err)
	}
}

// Reports a pending stop request that was not the doctor's own
func (r *CrashResponder) userStop(serverID string) string {
	if src := r.Lifecycle.StopRequestedBy(serverID); src != "" && src != doctorSource {
		return src
	}
	return ""
}

// Leaves a stopped server stopped, repairs wait for the user
func (r *CrashResponder) standDown(ctx context.Context, serverID string, server *storage.Server, src string) {
	r.Log.Info("autopilot: crash doctor standing down for server %s, stopped by %s", serverID, src)
	j := loadDoctor(server.DataPath)
	if j.Incident == nil {
		return
	}
	ctx = activity.WithTraceID(ctx, incidentTrace(j.Incident))
	r.Rec.Record(ctx, serverID, "doctor.stand_down", activity.Attrs{"stopped_by": src}, "leaving the server stopped, repairs resume on the next start")
}

func plannedDisables(actions []doctorAction) int {
	n := 0
	for i := range actions {
		if actions[i].Kind == actionDisable {
			n++
		}
	}
	return n
}

// Executes one action on disk and journals it
func (r *CrashResponder) apply(ctx context.Context, serverID string, server *storage.Server, modsDir string, a doctorAction, inc *doctorIncident) bool {
	a.AppliedAt = time.Now()
	inc.markTried(a.key())

	switch a.Kind {
	case actionDisable:
		if err := minecraft.DisableModJar(modsDir, a.File); err != nil {
			r.Log.Error("autopilot: crash doctor could not disable %s: %v", a.File, err)
			return false
		}
		r.Rec.Announce(ctx, serverID, "doctor.disable", activity.Attrs{"file": a.File, "reason": a.Reason, "evidence": a.Evidence}, "disabled %s (%s)", a.File, a.Reason)
	case actionEnable:
		if err := minecraft.EnableModJar(modsDir, a.File); err != nil {
			r.Log.Error("autopilot: crash doctor could not re-enable %s: %v", a.File, err)
			return false
		}
		r.Rec.Announce(ctx, serverID, "doctor.enable", activity.Attrs{"file": a.File, "reason": a.Reason}, "re-enabled %s (%s)", a.File, a.Reason)
	case actionDisablePack:
		if err := minecraft.DisableDatapack(server.DataPath, a.File); err != nil {
			r.Log.Error("autopilot: crash doctor could not disable data pack %s: %v", a.File, err)
			return false
		}
		r.Rec.Announce(ctx, serverID, "doctor.disable_pack", activity.Attrs{"file": a.File, "reason": a.Reason, "evidence": a.Evidence}, "disabled data pack %s (%s)", a.File, a.Reason)
	case actionInstall:
		file, err := r.Installer.InstallModByID(ctx, server, a.ModID, a.Range, dialectsFor(a.Dialect))
		if err != nil {
			r.Log.Info("autopilot: crash doctor could not source %s: %v", a.ModID, err)
			return false
		}
		a.File = file
		r.Rec.Announce(ctx, serverID, "doctor.install", activity.Attrs{"mod": a.ModID, "file": file, "range": a.Range}, "installed missing dependency %s (%s)", a.ModID, file)
	}
	inc.Actions = append(inc.Actions, a)
	return true
}

func dialectsFor(dialect string) []string {
	if dialect == "" {
		return nil
	}
	return []string{dialect}
}

// Rolls back every live action, newest first
func (r *CrashResponder) revertAll(dataPath, modsDir string, inc *doctorIncident) {
	for i := len(inc.Actions) - 1; i >= 0; i-- {
		a := &inc.Actions[i]
		if a.Reverted {
			continue
		}
		switch a.Kind {
		case actionDisable:
			if err := minecraft.EnableModJar(modsDir, a.File); err != nil {
				r.Log.Error("autopilot: crash doctor could not restore %s: %v", a.File, err)
				continue
			}
		case actionEnable:
			if err := minecraft.DisableModJar(modsDir, a.File); err != nil {
				r.Log.Error("autopilot: crash doctor could not restore %s: %v", a.File, err)
				continue
			}
		case actionDisablePack:
			if err := minecraft.EnableDatapack(dataPath, a.File); err != nil {
				r.Log.Error("autopilot: crash doctor could not restore %s: %v", a.File, err)
				continue
			}
		case actionInstall:
			if a.File != "" {
				if err := os.Remove(filepath.Join(modsDir, a.File)); err != nil {
					r.Log.Error("autopilot: crash doctor could not remove %s: %v", a.File, err)
					continue
				}
			}
		}
		a.Reverted = true
	}
}

// Gives up honestly, restores the pack, and stops the server
func (r *CrashResponder) exhaust(ctx context.Context, serverID string, server *storage.Server, j *doctorState, why string) {
	inc := j.Incident
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	r.revertAll(server.DataPath, modsDir, inc)

	inc.Outcome = "gave_up"
	inc.Summary = why + ", all changes were undone"
	j.Resolved, j.Incident = inc, nil
	if err := saveDoctor(server.DataPath, j); err != nil {
		r.Log.Error("autopilot: crash doctor journal save failed for server %s: %v", serverID, err)
	}

	r.Collector.RecordAutoRepair(serverID, inc.Summary)
	r.Collector.MarkCrashLoopStopped(serverID)
	r.Rec.Announce(ctx, serverID, "doctor.give_up", activity.Attrs{"reason": why}, "%s, stopping the server", inc.Summary)
	r.Log.Warn("autopilot: crash doctor gave up on server %s (%s)", serverID, why)

	if err := r.Lifecycle.Stop(ctx, serverID); err != nil {
		r.Log.Error("autopilot: crash doctor stop failed for server %s: %v", serverID, err)
	}
}

// Chooses this pass's actions from the strongest available evidence
func (r *CrashResponder) plan(server *storage.Server, cfg *storage.ServerProperties, m *metrics.ServerMetrics, modsDir string, inc *doctorIncident) []doctorAction {
	metas := minecraft.ScanModsDir(modsDir)
	force := minecraft.ForceIncludePatterns(server.ModLoader, cfg)
	excludes := minecraft.PackExcludePatterns(server.ModLoader, cfg)

	fatal := effectiveFatal(server, m)
	if failed := fatal.GetFailedMods(); len(failed) > 0 {
		return r.planVerdicts(server, failed, metas, modsDir, force, excludes, inc)
	}
	// Runtime crashes stay hands-off, world data is at stake
	if m.LastExitWasReady {
		return nil
	}

	r.revertGuesses(modsDir, inc)

	if actions := r.planRegistry(server, m, fatal, metas, modsDir, force, excludes, inc); len(actions) > 0 {
		return actions
	}

	return planFrameGuess(fatal, metas, force, inc)
}

// Remedies loader verdicts by failure reason, not by reflex
func (r *CrashResponder) planVerdicts(server *storage.Server, failed []*agentv1.FailedMod, metas []minecraft.ModJarMeta, modsDir string, force, excludes []string, inc *doctorIncident) []doctorAction {
	var actions []doctorAction
	solved := minecraft.SolveDeps(metas, serverDialects(server))
	disabledMetas := minecraft.ScanModsDir(modsDir + "_disabled")

	add := func(a doctorAction) {
		if inc.tried(a.key()) {
			return
		}
		for i := range actions {
			if actions[i].key() == a.key() {
				return
			}
		}
		actions = append(actions, a)
	}

	for _, fm := range failed {
		file := resolveFailedMod(fm, metas).ModFile
		switch classifyFailedMod(fm) {
		case failMissingDep:
			missing := missingDepsOf(fm.GetModId(), solved)
			for _, dep := range missing {
				resolved := false
				// A dep we disabled earlier comes back before anything else
				for j := range disabledMetas {
					if !disabledMetas[j].HasModID(dep.DepID) ||
						minecraft.MatchesPatterns(disabledMetas[j].FileName, excludes) {
						continue
					}
					a := doctorAction{Kind: actionEnable, File: disabledMetas[j].FileName, ModID: dep.DepID, Reason: "needed by " + dep.ModID, Evidence: evidenceSolver}
					if !inc.tried(a.key()) {
						add(a)
						resolved = true
					}
					break
				}
				if !resolved && r.Installer != nil {
					a := doctorAction{Kind: actionInstall, ModID: dep.DepID, Range: dep.Range, Dialect: depDialect(metas, dep), Evidence: evidenceSolver}
					if !inc.tried(a.key()) {
						add(a)
						resolved = true
					}
				}
				if !resolved && file != "" && !minecraft.MatchesPatterns(file, force) {
					add(doctorAction{Kind: actionDisable, File: file, ModID: fm.GetModId(), Reason: "requires missing " + dep.DepID, Evidence: evidenceVerdict})
				}
			}
			// The loader saw a dep problem our solver cannot map
			if len(missing) == 0 && file != "" && !minecraft.MatchesPatterns(file, force) {
				add(doctorAction{Kind: actionDisable, File: file, ModID: fm.GetModId(), Reason: "unresolvable dependency", Evidence: evidenceVerdict})
			}
		case failDuplicate:
			for _, issue := range solved {
				if issue.Kind == minecraft.DepDuplicate && issue.ModID == fm.GetModId() && issue.OtherFile != "" && !minecraft.MatchesPatterns(issue.OtherFile, force) {
					add(doctorAction{Kind: actionDisable, File: issue.OtherFile, ModID: fm.GetModId(), Reason: "older duplicate of " + issue.File, Evidence: evidenceVerdict})
				}
			}
		case failJava:
			// A jar cannot fix the JVM, the finding says so instead
		case failModError:
			if file != "" && !minecraft.MatchesPatterns(file, force) {
				add(doctorAction{Kind: actionDisable, File: file, ModID: fm.GetModId(), Reason: "the loader reported it cannot load", Evidence: evidenceVerdict})
			}
		}
	}
	return actions
}

func missingDepsOf(modID string, issues []minecraft.DepIssue) []minecraft.DepIssue {
	var out []minecraft.DepIssue
	for _, issue := range issues {
		if issue.Kind == minecraft.DepMissing && (modID == "" || issue.ModID == modID) {
			out = append(out, issue)
		}
	}
	return out
}

// Finds the metadata dialect that declared this dep
func depDialect(metas []minecraft.ModJarMeta, issue minecraft.DepIssue) string {
	for i := range metas {
		for _, dep := range metas[i].Deps {
			if dep.Owner == issue.ModID && dep.ID == issue.DepID && dep.Dialect != "" {
				return dep.Dialect
			}
		}
	}
	return ""
}

// Undoes unverified guesses before planning new ones
func (r *CrashResponder) revertGuesses(modsDir string, inc *doctorIncident) {
	for i := len(inc.Actions) - 1; i >= 0; i-- {
		a := &inc.Actions[i]
		if a.Reverted || a.Kind != actionDisable || a.Evidence != evidenceFrame {
			continue
		}
		if err := minecraft.EnableModJar(modsDir, a.File); err != nil {
			r.Log.Error("autopilot: crash doctor could not restore %s: %v", a.File, err)
			continue
		}
		a.Reverted = true
	}
}

// A mod named by the crash frames is a guess worth one try
func planFrameGuess(fatal *agentv1.FatalError, metas []minecraft.ModJarMeta, force []string, inc *doctorIncident) []doctorAction {
	_, file := attributeFatal(fatal, metas)
	if file == "" || minecraft.MatchesPatterns(file, force) {
		return nil
	}
	a := doctorAction{Kind: actionDisable, File: file, Reason: "the crash happened inside this mod's code", Evidence: evidenceFrame}
	if inc.tried(a.key()) {
		return nil
	}
	return []doctorAction{a}
}

// Dialects the server's platform reads, declared else observed
func serverDialects(server *storage.Server) []string {
	return minecraft.ResolveDialects(server.ModLoader, server.DataPath, minecraft.GetModsPath(server.DataPath, server.ModLoader))
}

func (r *CrashResponder) breakCrashLoop(serverID string) {
	m := r.Collector.GetMetrics(serverID)
	if m == nil || m.CrashesWithin(crashLoopWindow) < crashLoopThreshold {
		return
	}
	if !m.CrashLoopStoppedAt.IsZero() && time.Since(m.CrashLoopStoppedAt) < crashLoopWindow {
		return
	}
	r.Collector.MarkCrashLoopStopped(serverID)

	ctx := activity.WithTrace(activity.WithSource(context.Background(), "crash doctor"))
	r.Rec.Announce(ctx, serverID, "doctor.loop_break",
		activity.Attrs{"crashes": strconv.Itoa(crashLoopThreshold), "window_minutes": strconv.Itoa(int(crashLoopWindow.Minutes()))},
		"server crashed %d times in %d minutes, stopping it to break the loop",
		crashLoopThreshold, int(crashLoopWindow.Minutes()))
	r.Log.Warn("autopilot: crash loop detected for server %s, stopping it", serverID)

	go func() {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		if err := r.Lifecycle.Stop(ctx, serverID); err != nil {
			r.Log.Error("autopilot: crash loop stop failed for server %s: %v", serverID, err)
		}
	}()
}

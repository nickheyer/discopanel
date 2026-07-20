package main

import (
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/nickheyer/discopanel/pkg/minecraft"
	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
)

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func removeFile(p string) error {
	return os.Remove(p)
}

// Dedups planned actions against each other and tried keys
type actionPlan struct {
	inc     *runtimespec.DoctorIncident
	actions []runtimespec.DoctorAction
}

func (p *actionPlan) add(a runtimespec.DoctorAction) {
	if p.inc.HasTried(a.Key()) {
		return
	}
	for i := range p.actions {
		if p.actions[i].Key() == a.Key() {
			return
		}
	}
	p.actions = append(p.actions, a)
}

// Typed capture wins, report file is the floor
func effectiveFatal(srv *serverInfo, exit *agentv1.Exited) *agentv1.FatalError {
	fatal := exit.GetFatalError()
	if len(fatal.GetFailedMods()) > 0 {
		return fatal
	}
	failed := parseReportMods(readCrashReport(srv.DataPath, exit.GetCrashReportPath()))
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

// Chooses this pass's actions from the strongest available evidence
func (e *engine) plan(srv *serverInfo, exit *agentv1.Exited, modsDir string, force []string, inc *runtimespec.DoctorIncident) []runtimespec.DoctorAction {
	metas := minecraft.ScanModsDir(modsDir)
	excludes := journalExcludes(srv)

	fatal := effectiveFatal(srv, exit)
	if failed := fatal.GetFailedMods(); modsDir != "" && len(failed) > 0 {
		return e.planVerdicts(srv, failed, metas, modsDir, force, excludes, inc)
	}
	// Runtime crashes stay hands-off, world data is at stake
	if exit.GetWasReady() {
		return nil
	}

	e.revertGuesses(srv, modsDir, inc, fatalSignature(fatal))

	if actions := e.planRegistry(srv, exit, fatal, metas, modsDir, force, excludes, inc); len(actions) > 0 {
		return actions
	}

	return planFrameGuess(fatal, metas, force, inc)
}

// Remedies loader verdicts by failure reason, not by reflex
func (e *engine) planVerdicts(srv *serverInfo, failed []*agentv1.FailedMod, metas []minecraft.ModJarMeta, modsDir string, force, excludes []string, inc *runtimespec.DoctorIncident) []runtimespec.DoctorAction {
	plan := &actionPlan{inc: inc}
	add := plan.add
	solved := minecraft.SolveDeps(metas, serverDialects(srv))
	disabledMetas := minecraft.ScanModsDir(modsDir + "_disabled")

	for _, fm := range failed {
		file := resolveFailedMod(fm, metas).ModFile
		switch classifyFailedMod(fm) {
		case failMissingDep:
			missing := missingDepsOf(fm.GetModId(), solved)
			// Loader text names deps nested jar metadata hides
			if len(missing) == 0 {
				if dep := missingDepFromMessage(fm.GetErrorMessage()); dep != "" {
					missing = append(missing, minecraft.DepIssue{Kind: minecraft.DepMissing, ModID: fm.GetModId(), DepID: dep})
				}
			}
			for _, dep := range missing {
				resolved := false
				// A dep we disabled earlier comes back before anything else
				for j := range disabledMetas {
					if !disabledMetas[j].HasModID(dep.DepID) ||
						minecraft.MatchesPatterns(disabledMetas[j].FileName, excludes) {
						continue
					}
					a := runtimespec.DoctorAction{Kind: runtimespec.ActionEnable, File: disabledMetas[j].FileName, ModID: dep.DepID, Reason: "needed by " + dep.ModID, Evidence: runtimespec.EvidenceSolver}
					if !inc.HasTried(a.Key()) {
						add(a)
						resolved = true
					}
					break
				}
				if !resolved && e.installer != nil {
					a := runtimespec.DoctorAction{Kind: runtimespec.ActionInstall, ModID: dep.DepID, Range: dep.Range, Dialect: depDialect(metas, dep), Evidence: runtimespec.EvidenceSolver}
					if !inc.HasTried(a.Key()) {
						add(a)
						resolved = true
					}
				}
				if !resolved && file != "" && !minecraft.MatchesPatterns(file, force) {
					add(runtimespec.DoctorAction{Kind: runtimespec.ActionDisable, File: file, ModID: fm.GetModId(), Reason: "requires missing " + dep.DepID, Evidence: runtimespec.EvidenceVerdict})
				}
			}
			// The loader saw a dep problem our solver cannot map
			if len(missing) == 0 && file != "" && !minecraft.MatchesPatterns(file, force) {
				add(runtimespec.DoctorAction{Kind: runtimespec.ActionDisable, File: file, ModID: fm.GetModId(), Reason: "unresolvable dependency", Evidence: runtimespec.EvidenceVerdict})
			}
		case failDuplicate:
			for _, issue := range solved {
				if issue.Kind == minecraft.DepDuplicate && issue.ModID == fm.GetModId() && issue.OtherFile != "" && !minecraft.MatchesPatterns(issue.OtherFile, force) {
					add(runtimespec.DoctorAction{Kind: runtimespec.ActionDisable, File: issue.OtherFile, ModID: fm.GetModId(), Reason: "older duplicate of " + issue.File, Evidence: runtimespec.EvidenceVerdict})
				}
			}
		case failJava:
			// Jars cannot fix the JVM
		case failModError:
			// A linkage failure convicts the crashing frame, not the reporter
			if a, ok := accompliceAction(fm, file, metas, force); ok && !inc.HasTried(a.Key()) {
				add(a)
				continue
			}
			if file != "" && !minecraft.MatchesPatterns(file, force) {
				add(runtimespec.DoctorAction{Kind: runtimespec.ActionDisable, File: file, ModID: fm.GetModId(), Reason: "the loader reported it cannot load", Evidence: runtimespec.EvidenceVerdict})
			}
		}
	}
	return plan.actions
}

// Root failure types meaning code or classes failed to link
var linkageErrorTypes = map[string]bool{
	"AbstractMethodError":          true,
	"NoClassDefFoundError":         true,
	"NoSuchFieldError":             true,
	"NoSuchMethodError":            true,
	"IncompatibleClassChangeError": true,
	"UnsatisfiedLinkError":         true,
	"BootstrapMethodError":         true,
	"ClassNotFoundException":       true,
}

// First installed jar in the failure frames names the culprit
func accompliceAction(fm *agentv1.FailedMod, blamedFile string, metas []minecraft.ModJarMeta, force []string) (runtimespec.DoctorAction, bool) {
	if !linkageErrorTypes[simpleTypeName(fm.GetErrorType())] {
		return runtimespec.DoctorAction{}, false
	}
	for _, frame := range fm.GetFrames() {
		jar := jarFromLocation(frame.GetSourceLocation())
		meta := metaByFile(metas, jar)
		if jar == "" || meta == nil {
			continue
		}
		// The reporter crashing in its own code convicts itself
		if jar == blamedFile || minecraft.MatchesPatterns(jar, force) {
			return runtimespec.DoctorAction{}, false
		}
		id := ""
		if len(meta.Mods) > 0 {
			id = meta.Mods[0].ID
		}
		return runtimespec.DoctorAction{Kind: runtimespec.ActionDisable, File: jar, ModID: id, Reason: "its code crashed " + fm.GetModId() + " during load", Evidence: runtimespec.EvidenceVerdict}, true
	}
	return runtimespec.DoctorAction{}, false
}

// Forge phrases missing deps as requires id version or above
var requiresPattern = regexp.MustCompile(`requires ([a-z][a-z0-9_.-]*)`)

// Pulls the missing dep id out of loader text
func missingDepFromMessage(msg string) string {
	m := requiresPattern.FindStringSubmatch(msg)
	if m == nil {
		return ""
	}
	return m[1]
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

// Crash frame mods are guesses worth one try
func planFrameGuess(fatal *agentv1.FatalError, metas []minecraft.ModJarMeta, force []string, inc *runtimespec.DoctorIncident) []runtimespec.DoctorAction {
	_, file := attributeFatal(fatal, metas)
	if file == "" || minecraft.MatchesPatterns(file, force) {
		return nil
	}
	reason := "the crash happened inside this mod's code"
	if isStallFatal(fatal) {
		reason = "the boot stalled inside this mod's code"
	}
	a := runtimespec.DoctorAction{Kind: runtimespec.ActionDisable, File: file, Reason: reason, Evidence: runtimespec.EvidenceFrame, Cause: fatalSignature(fatal)}
	if inc.HasTried(a.Key()) {
		return nil
	}
	return []runtimespec.DoctorAction{a}
}

// Deepest cause type names one crash's identity
func fatalSignature(fatal *agentv1.FatalError) string {
	causes := fatal.GetCauses()
	if len(causes) == 0 {
		return ""
	}
	return causes[len(causes)-1].GetType()
}

// Stall dump fatals carry BootStall thread causes
func isStallFatal(fatal *agentv1.FatalError) bool {
	for _, c := range fatal.GetCauses() {
		if c.GetType() == "BootStall" {
			return true
		}
	}
	return false
}

// Dialects the server's platform reads, declared else observed
func serverDialects(srv *serverInfo) []string {
	return minecraft.ResolveDialects(srv.ModLoader, srv.DataPath, minecraft.GetModsPath(srv.DataPath, srv.ModLoader))
}

type crashModRef struct {
	ModID   string
	ModFile string
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
			if metas[i].HasReportedModID(ref.ModID) {
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
		case "BootStall":
			return "The server stopped making progress while starting and sat idle."
		}
	}
	for _, c := range fatal.GetCauses() {
		if strings.Contains(c.GetMessage(), "in environment type SERVER") {
			return "A client-only mod tried to load client code that does not exist on a dedicated server."
		}
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

// Maps structured crash frames onto installed jars
// Frames come typed from the JVM agent, never from log text

var mixinFramePattern = regexp.MustCompile(`(?:handler|localvar|redirect|modify|constant|args|wrapoperation|wrapwithcondition|bridge)\$[A-Za-z0-9]+\$([A-Za-z0-9_]+)\$`)

// Walks cause frames for an installed jar, deepest cause first
func attributeFatal(fatal *agentv1.FatalError, metas []minecraft.ModJarMeta) (modID, fileName string) {
	causes := fatal.GetCauses()
	for i := len(causes) - 1; i >= 0; i-- {
		for _, frame := range causes[i].GetFrames() {
			if m := mixinFramePattern.FindStringSubmatch(frame.GetMethodName() + "$"); m != nil {
				for j := range metas {
					if metas[j].HasReportedModID(m[1]) {
						return m[1], metas[j].FileName
					}
				}
			}
			if jar := jarFromLocation(frame.GetSourceLocation()); jar != "" {
				if meta := metaByFile(metas, jar); meta != nil {
					id := ""
					if len(meta.Mods) > 0 {
						id = meta.Mods[0].ID
					}
					return id, jar
				}
			}
		}
	}
	return "", ""
}

// Extracts a jar file name from a code source location
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

func metaByFile(metas []minecraft.ModJarMeta, file string) *minecraft.ModJarMeta {
	for i := range metas {
		if metas[i].FileName == file {
			return &metas[i]
		}
	}
	return nil
}

// Strips the package from a fully qualified type name
func simpleTypeName(t string) string {
	if idx := strings.LastIndexByte(t, '.'); idx >= 0 {
		return t[idx+1:]
	}
	return t
}

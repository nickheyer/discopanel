// Data level diagnosis for unbound registry reference crashes
package autopilot

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/metrics"
	"github.com/nickheyer/discopanel/internal/minecraft"
	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
)

// One registry entry the crash names as unbound or missing
type registryRef struct {
	Registry  string
	Namespace string
	Path      string
}

func (r registryRef) id() string {
	return r.Namespace + ":" + r.Path
}

// Vanilla prints registry keys in ResourceKey bracket form
var resourceKeyPattern = regexp.MustCompile(`ResourceKey\[([a-z0-9_.:/-]+) / ([a-z0-9_.-]+):([a-z0-9_./-]+)\]`)

// Bare ids listed after an unbound values registry dump
var registryIDPattern = regexp.MustCompile(`([a-z0-9_.-]+):([a-z0-9_./-]+)`)

// Caps refs per incident so content scans stay bounded
const maxRegistryRefs = 64

// Pulls unbound registry references out of crash text
func parseUnboundRefs(texts ...string) []registryRef {
	var refs []registryRef
	seen := map[string]bool{}
	add := func(ref registryRef) {
		key := ref.Registry + "|" + ref.id()
		if len(refs) >= maxRegistryRefs || seen[key] {
			return
		}
		seen[key] = true
		refs = append(refs, ref)
	}
	for _, text := range texts {
		for _, line := range strings.Split(text, "\n") {
			lower := strings.ToLower(line)
			if !strings.Contains(lower, "unbound value") && !strings.Contains(lower, "missing element") {
				continue
			}
			for _, m := range resourceKeyPattern.FindAllStringSubmatch(line, -1) {
				// A root key names a registry, its ids follow
				if m[1] == "minecraft:root" {
					registry := m[2] + ":" + m[3]
					if _, after, ok := strings.Cut(line, "]: ["); ok {
						for _, im := range registryIDPattern.FindAllStringSubmatch(after, -1) {
							add(registryRef{Registry: registry, Namespace: im[1], Path: im[2]})
						}
					}
					continue
				}
				add(registryRef{Registry: m[1], Namespace: m[2], Path: m[3]})
			}
		}
	}
	return refs
}

// Plans repairs for content referencing an absent namespace
func (r *CrashResponder) planRegistry(server *storage.Server, m *metrics.ServerMetrics, fatal *agentv1.FatalError, metas []minecraft.ModJarMeta, modsDir string, force, excludes []string, inc *doctorIncident) []doctorAction {
	texts := make([]string, 0, len(fatal.GetCauses())+2)
	for _, c := range fatal.GetCauses() {
		texts = append(texts, c.GetMessage())
	}
	texts = append(texts, m.LastCrashExcerpt, readCrashReport(server.DataPath, m.LastCrashReportPath))
	refs := parseUnboundRefs(texts...)
	if len(refs) == 0 {
		return nil
	}

	byNS := map[string][]registryRef{}
	for _, ref := range refs {
		byNS[ref.Namespace] = append(byNS[ref.Namespace], ref)
	}
	namespaces := make([]string, 0, len(byNS))
	for ns := range byNS {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)

	var actions []doctorAction
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

	var disabledMetas []minecraft.ModJarMeta
	if modsDir != "" {
		disabledMetas = minecraft.ScanModsDir(modsDir + "_disabled")
	}
	for _, ns := range namespaces {
		if nsProvider(metas, ns) != "" {
			continue
		}
		// A provider we disabled earlier comes back first
		if file := nsProvider(disabledMetas, ns); file != "" && !minecraft.MatchesPatterns(file, excludes) {
			add(doctorAction{Kind: actionEnable, File: file, ModID: ns, Reason: "provides the missing " + ns + " content", Evidence: evidenceRegistry})
			continue
		}
		// Sourcing the provider beats disabling its dependents
		if modsDir != "" {
			add(doctorAction{Kind: actionInstall, ModID: ns, Reason: "provides the missing " + ns + " content", Evidence: evidenceRegistry})
		}
		ids := make([]string, 0, len(byNS[ns]))
		for _, ref := range byNS[ns] {
			ids = append(ids, ref.id())
		}
		found := false
		for _, rel := range minecraft.FindDatapackRefs(server.DataPath, ids) {
			if minecraft.MatchesPatterns(filepath.Base(rel), force) {
				continue
			}
			add(doctorAction{Kind: actionDisablePack, File: rel, Reason: "references " + ns + " content that is not installed", Evidence: evidenceRegistry})
			found = true
		}
		if found {
			continue
		}
		// No datapack culprit, a mod jar may carry the refs
		for i := range metas {
			if minecraft.MatchesPatterns(metas[i].FileName, force) {
				continue
			}
			if minecraft.ZipRefsAny(filepath.Join(modsDir, metas[i].FileName), ids) {
				add(doctorAction{Kind: actionDisable, File: metas[i].FileName, Reason: "references " + ns + " content that is not installed", Evidence: evidenceRegistry})
			}
		}
	}
	return actions
}

// Finds the jar whose mod ids claim the namespace
func nsProvider(metas []minecraft.ModJarMeta, ns string) string {
	for i := range metas {
		if metas[i].HasReportedModID(ns) {
			return metas[i].FileName
		}
	}
	return ""
}

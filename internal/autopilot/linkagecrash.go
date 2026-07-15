// Content level diagnosis for mod API linkage crashes
package autopilot

import (
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/metrics"
	"github.com/nickheyer/discopanel/internal/minecraft"
	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
)

// Error names that mean bytecode failed to link
var linkageErrorNames = []string{
	"AbstractMethodError", "NoSuchMethodError", "NoSuchFieldError",
	"NoClassDefFoundError", "IncompatibleClassChangeError",
}

// Class descriptors like Ldev/kosmx/playerAnim/api/Foo;
var descriptorClassPattern = regexp.MustCompile(`L([A-Za-z_][\w$]*(?:/[\w$]+){2,});`)

// Bare dotted or slashed class paths in error messages
var bareClassPattern = regexp.MustCompile(`\b[a-z_][\w$]*(?:[./][A-Za-z_$][\w$]*){2,}`)

// Mixin config names decorating a crash report frame
var frameConfigPattern = regexp.MustCompile(`pl:mixin:APP:([^:,{}]+):`)

// Platform packages that never convict a mod
var platformClassPrefixes = []string{
	"java/", "javax/", "jdk/", "sun/", "com/sun/",
	"net/minecraft/", "com/mojang/",
	"net/minecraftforge/", "net/neoforged/", "net/fabricmc/", "org/quiltmc/",
	"cpw/mods/", "org/spongepowered/", "org/sinytra/",
	"kotlin/", "scala/", "org/apache/", "org/slf4j/", "com/google/",
	"io/netty/", "it/unimi/", "org/joml/", "org/lwjgl/", "org/objectweb/",
}

const (
	maxLinkageRefs   = 8
	maxLinkageFrames = 4
)

func isPlatformClass(path string) bool {
	for _, prefix := range platformClassPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// Pulls foreign class paths out of linkage error lines
func parseLinkageRefs(text string) []string {
	var refs []string
	seen := map[string]bool{}
	add := func(path string) {
		path = strings.ReplaceAll(path, ".", "/")
		if len(refs) >= maxLinkageRefs || seen[path] || isPlatformClass(path) {
			return
		}
		if strings.Count(path, "/") < 2 {
			return
		}
		seen[path] = true
		refs = append(refs, path)
	}
	for _, line := range strings.Split(text, "\n") {
		if !linkageErrorLine(line) {
			continue
		}
		for _, m := range descriptorClassPattern.FindAllStringSubmatch(line, -1) {
			add(m[1])
		}
		for _, token := range bareClassPattern.FindAllString(line, -1) {
			add(token)
			// Method references also name their owning class
			if idx := strings.LastIndexAny(token, "./"); idx > 0 {
				add(token[:idx])
			}
		}
	}
	return refs
}

func linkageErrorLine(line string) bool {
	if strings.HasPrefix(strings.TrimSpace(line), "at ") {
		return false
	}
	for _, name := range linkageErrorNames {
		if strings.Contains(line, name) {
			return true
		}
	}
	return false
}

// Mixin configs decorating the first crashing frames
func parseCrashFrameConfigs(text string) []string {
	var configs []string
	seen := map[string]bool{}
	frames := 0
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "at ") {
			continue
		}
		matches := frameConfigPattern.FindAllStringSubmatch(line, -1)
		if len(matches) == 0 {
			continue
		}
		for _, m := range matches {
			if !seen[m[1]] {
				seen[m[1]] = true
				configs = append(configs, m[1])
			}
		}
		frames++
		if frames >= maxLinkageFrames {
			break
		}
	}
	return configs
}

// Class owner lookups memoized, mods rarely change
var classOwnerCache sync.Map

// Finds the jar shipping a class, cached per mods dir
func classOwnerJar(modsDir string, metas []minecraft.ModJarMeta, classPath string) string {
	key := modsDir + "|" + classPath
	if v, ok := classOwnerCache.Load(key); ok {
		return v.(string)
	}
	owner := ""
	for i := range metas {
		if minecraft.JarHasEntry(filepath.Join(modsDir, metas[i].FileName), classPath+".class") {
			owner = metas[i].FileName
			break
		}
	}
	classOwnerCache.Store(key, owner)
	return owner
}

// Stall dump frames carry classes only, owners convict
func stallFrameOwner(fatal *agentv1.FatalError, metas []minecraft.ModJarMeta, modsDir string) (string, string) {
	if modsDir == "" {
		return "", ""
	}
	for _, c := range fatal.GetCauses() {
		if c.GetType() != "BootStall" {
			continue
		}
		for _, f := range c.GetFrames() {
			path := strings.ReplaceAll(f.GetClassName(), ".", "/")
			if path == "" || isPlatformClass(path) {
				continue
			}
			file := classOwnerJar(modsDir, metas, path)
			if file == "" {
				continue
			}
			id := ""
			if meta := metaByFile(metas, file); meta != nil && len(meta.Mods) > 0 {
				id = meta.Mods[0].ID
			}
			return id, file
		}
	}
	return "", ""
}

// Convicts the crash frame mixin owner whose code failed to link
func (r *CrashResponder) planLinkage(server *storage.Server, m *metrics.ServerMetrics, metas []minecraft.ModJarMeta, modsDir string, force []string, inc *doctorIncident) []doctorAction {
	text := readCrashReport(server.DataPath, m.LastCrashReportPath)
	if text == "" {
		text = m.LastCrashExcerpt
	}
	if text == "" {
		return nil
	}
	refs := parseLinkageRefs(text)
	if len(refs) == 0 {
		return nil
	}
	configs := parseCrashFrameConfigs(text)
	if len(configs) == 0 {
		return nil
	}

	// Jars applying mixins to the crashing frames
	var suspects []*minecraft.ModJarMeta
	for i := range metas {
		path := filepath.Join(modsDir, metas[i].FileName)
		for _, config := range configs {
			if minecraft.JarHasEntry(path, config) {
				suspects = append(suspects, &metas[i])
				break
			}
		}
	}
	if len(suspects) == 0 {
		return nil
	}

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

	for _, ref := range refs {
		owner := ""
		ownerID := ""
		for i := range metas {
			if minecraft.JarHasEntry(filepath.Join(modsDir, metas[i].FileName), ref+".class") {
				owner = metas[i].FileName
				if len(metas[i].Mods) > 0 {
					ownerID = metas[i].Mods[0].ID
				}
				break
			}
		}
		// Classes no installed jar ships belong to other planners
		if owner == "" {
			continue
		}
		label := ownerID
		if label == "" {
			label = owner
		}
		for _, s := range suspects {
			if s.FileName == owner || minecraft.MatchesPatterns(s.FileName, force) {
				continue
			}
			if !minecraft.JarRefsClass(filepath.Join(modsDir, s.FileName), ref) {
				continue
			}
			id := ""
			if len(s.Mods) > 0 {
				id = s.Mods[0].ID
			}
			add(doctorAction{Kind: actionDisable, File: s.FileName, ModID: id, Reason: "its code failed to link with " + label, Evidence: evidenceRegistry})
		}
	}
	return actions
}

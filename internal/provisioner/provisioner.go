// Prepares server data directories, containers never install
package provisioner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nickheyer/discopanel/internal/activity"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/minecraft"
	"github.com/nickheyer/discopanel/pkg/files"
	"github.com/nickheyer/discopanel/pkg/logger"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
)

// Receives human-readable provisioning progress lines for a server
type ProgressSink func(serverID string, message string)

type Provisioner struct {
	store  *storage.Store
	docker *docker.Client
	cfg    *config.Config
	log    *logger.Logger
	rec    *activity.Recorder
	sink   ProgressSink

	mu     sync.Mutex
	active map[string]*sync.Mutex // Per-server provisioning locks
}

// Reports what a successful provision resolved
type Result struct {
	Loader        storage.ModLoader
	LoaderVersion string
	MCVersion     string
	JavaMajor     int
}

func New(store *storage.Store, dockerClient *docker.Client, cfg *config.Config, rec *activity.Recorder, log *logger.Logger) *Provisioner {
	return &Provisioner{
		store:  store,
		docker: dockerClient,
		cfg:    cfg,
		log:    log,
		rec:    rec,
		active: make(map[string]*sync.Mutex),
	}
}

// Registers a sink for progress lines, e.g. log streamer
func (p *Provisioner) SetProgressSink(sink ProgressSink) {
	p.sink = sink
}

func (p *Provisioner) progress(server *storage.Server, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	p.log.Info("provisioner [%s]: %s", server.Name, msg)
	if p.sink != nil {
		p.sink(server.ID, msg)
	}
}

// Progress line that also lands in the activity ledger
func (p *Provisioner) action(ctx context.Context, server *storage.Server, source, name string, attrs activity.Attrs, format string, args ...any) {
	p.progress(server, format, args...)
	if p.rec != nil {
		p.rec.Record(activity.WithSource(ctx, source), server.ID, name, attrs, format, args...)
	}
}

func (p *Provisioner) serverLock(serverID string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()
	if l, ok := p.active[serverID]; ok {
		return l
	}
	l := &sync.Mutex{}
	p.active[serverID] = l
	return l
}

// Identifies the modpack a server should be provisioned from
type desiredModpack struct {
	source    string // "curseforge" | "modrinth" | "zip"
	id        string
	versionID string // Empty means whatever is installed or latest
}

// Idempotently provisions server files and applies config
func (p *Provisioner) Ensure(ctx context.Context, server *storage.Server, cfg *storage.ServerProperties) (*Result, error) {
	lock := p.serverLock(server.ID)
	lock.Lock()
	defer lock.Unlock()

	if err := os.MkdirAll(server.DataPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create server data directory: %w", err)
	}

	// Free gate belongs before any multi gigabyte install
	if !strings.EqualFold(strVal(cfg.EULA), "true") {
		return nil, fmt.Errorf("the Minecraft EULA must be accepted before the server can start")
	}

	force := cfg.ForceProvision != nil && *cfg.ForceProvision
	manifest, err := runtimespec.ReadManifest(server.DataPath)
	if err != nil {
		p.progress(server, "provision manifest unreadable (%v), re-provisioning", err)
		manifest = nil
	}

	desired := p.desiredModpackFor(server, cfg)

	var result *Result
	if p.needsInstall(server, manifest, desired, force) {
		p.snapshotWorld(server)
		result, err = p.install(ctx, server, cfg, desired, force)
		if err != nil {
			return nil, err
		}

		mref := (*runtimespec.ModpackRef)(nil)
		if desired != nil {
			mref = &runtimespec.ModpackRef{
				Source:    desired.source,
				ID:        desired.id,
				VersionID: desired.versionID,
			}
		}
		if err := runtimespec.WriteManifest(server.DataPath, &runtimespec.Manifest{
			Version:       1,
			Loader:        string(server.ModLoader),
			LoaderVersion: result.LoaderVersion,
			MCVersion:     result.MCVersion,
			JavaMajor:     result.JavaMajor,
			Modpack:       mref,
			ProvisionedAt: time.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			return nil, fmt.Errorf("failed to write provision manifest: %w", err)
		}
		p.action(ctx, server, "provisioner", "provision.install",
			activity.Attrs{"loader": string(result.Loader), "loader_version": result.LoaderVersion, "mc_version": result.MCVersion},
			"installed server files (%s %s, MC %s, Java %d)",
			result.Loader, result.LoaderVersion, result.MCVersion, result.JavaMajor)
	} else {
		result = &Result{
			Loader:        storage.ModLoader(manifest.Loader),
			LoaderVersion: manifest.LoaderVersion,
			MCVersion:     manifest.MCVersion,
			JavaMajor:     manifest.JavaMajor,
		}
		p.progress(server, "server files verified (%s %s, MC %s, Java %d)",
			result.Loader, result.LoaderVersion, result.MCVersion, result.JavaMajor)
	}

	// Pack-managed mods get the client-only sweep every pass
	if desired != nil {
		p.disableClientOnlyMods(ctx, server, minecraft.ForceIncludePatterns(server.ModLoader, cfg))
	}

	// Dependency pre-flight fixes what it can prove, reports the rest
	p.preflightMods(ctx, server, cfg)

	// Config files are cheap and authoritative, always applied
	if err := p.applyConfigFiles(ctx, server, cfg, result.MCVersion, force); err != nil {
		return nil, fmt.Errorf("failed to apply configuration files: %w", err)
	}

	return result, nil
}

// Validates the installed mod graph before every boot
func (p *Provisioner) preflightMods(ctx context.Context, server *storage.Server, cfg *storage.ServerProperties) {
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if modsDir == "" {
		return
	}
	metas := minecraft.ScanModsDir(modsDir)
	if len(metas) == 0 {
		return
	}
	dialects := minecraft.ResolveDialects(server.ModLoader, server.DataPath, modsDir)

	issues := minecraft.SolveDeps(metas, dialects)
	if len(issues) == 0 {
		return
	}
	if p.preflightFix(ctx, server, cfg, modsDir, issues) {
		issues = minecraft.SolveDeps(minecraft.ScanModsDir(modsDir), dialects)
	}
	for _, issue := range issues {
		p.progress(server, "mod check: %s", issue.Describe())
	}
}

// Applies the provable local fixes, reports whether anything changed
func (p *Provisioner) preflightFix(ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, modsDir string, issues []minecraft.DepIssue) bool {
	force := minecraft.ForceIncludePatterns(server.ModLoader, cfg)
	excludes := minecraft.PackExcludePatterns(server.ModLoader, cfg)
	fixed := false

	for _, issue := range issues {
		switch issue.Kind {
		case minecraft.DepDuplicate:
			if issue.OtherFile == "" || minecraft.MatchesPatterns(issue.OtherFile, force) {
				continue
			}
			if err := minecraft.DisableModJar(modsDir, issue.OtherFile); err != nil {
				continue
			}
			p.action(ctx, server, "mod check", "mod.disable", activity.Attrs{"file": issue.OtherFile, "duplicate_of": issue.File}, "disabled %s, an older duplicate of %s", issue.OtherFile, issue.File)
			fixed = true
		case minecraft.DepMissing:
			// A disabled jar that provides the dep comes back
			for _, dm := range minecraft.ScanModsDir(modsDir + "_disabled") {
				if !dm.HasModID(issue.DepID) || minecraft.MatchesPatterns(dm.FileName, excludes) {
					continue
				}
				if err := minecraft.EnableModJar(modsDir, dm.FileName); err == nil {
					p.action(ctx, server, "mod check", "mod.enable", activity.Attrs{"file": dm.FileName, "needed_by": issue.ModID}, "re-enabled %s, %s needs it", dm.FileName, issue.ModID)
					fixed = true
				}
				break
			}
		}
	}
	return fixed
}

// Decides whether server files must be (re)installed
func (p *Provisioner) needsInstall(server *storage.Server, manifest *runtimespec.Manifest, desired *desiredModpack, force bool) bool {
	if force || manifest == nil {
		return true
	}
	if manifest.Loader != string(server.ModLoader) {
		return true
	}
	// Modpack servers derive MC version from the pack
	if desired == nil && manifest.MCVersion != server.MCVersion {
		return true
	}

	var installed *runtimespec.ModpackRef = manifest.Modpack
	switch {
	case desired == nil && installed != nil,
		desired != nil && installed == nil:
		return true
	case desired != nil && installed != nil:
		if desired.source != installed.Source || desired.id != installed.ID {
			return true
		}
		if desired.versionID != "" && desired.versionID != installed.VersionID {
			return true
		}
	}

	// The launch target must still exist on disk
	spec, err := runtimespec.ReadLaunchSpec(server.DataPath)
	if err != nil {
		return true
	}
	return !launchTargetExists(server.DataPath, spec)
}

// Archives world dirs before a reinstall rewrites the tree
func (p *Provisioner) snapshotWorld(server *storage.Server) {
	if p.cfg == nil || p.cfg.Storage.BackupDir == "" {
		return
	}
	worldDirs, err := files.FindWorldDirs(server.DataPath)
	if err != nil || len(worldDirs) == 0 {
		return
	}
	rels := make([]string, 0, len(worldDirs))
	for _, w := range worldDirs {
		if rel, err := filepath.Rel(server.DataPath, w); err == nil {
			rels = append(rels, rel)
		}
	}
	destDir := filepath.Join(p.cfg.Storage.BackupDir, filepath.Base(server.DataPath))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		p.log.Warn("Pre-provision snapshot skipped: %v", err)
		return
	}
	dest := filepath.Join(destDir, fmt.Sprintf("pre-provision_%s.zip", time.Now().UTC().Format("20060102-150405")))
	if _, err := files.CreateZipArchive(rels, server.DataPath, dest, true); err != nil {
		p.log.Warn("Pre-provision snapshot failed: %v", err)
		return
	}
	p.progress(server, "world snapshot saved (%s)", filepath.Base(dest))
}

// Derives the modpack identity from server and config
func (p *Provisioner) desiredModpackFor(server *storage.Server, cfg *storage.ServerProperties) *desiredModpack {
	pack := minecraft.PackPlatformFor(server.ModLoader)
	if pack == nil {
		return nil
	}
	switch pack.Source {
	case "curseforge":
		if v := strVal(cfg.CFModpackZip); v != "" {
			return &desiredModpack{source: "zip", id: v}
		}
		slug, fileID := parseCurseForgeRef(strVal(cfg.CFPageURL), strVal(cfg.CFSlug), strVal(cfg.CFFileID))
		return &desiredModpack{source: "curseforge", id: slug, versionID: fileID}
	case "modrinth":
		project, version := parseModrinthRef(strVal(cfg.ModrinthModpack), strVal(cfg.ModrinthVersion))
		return &desiredModpack{source: "modrinth", id: project, versionID: version}
	default:
		return nil
	}
}

type installFunc func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, desired *desiredModpack, force bool) (*Result, error)

// One native install path per loader, absence means user files
var loaderInstallers = map[storage.ModLoader]installFunc{
	storage.ModLoaderVanilla: func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, desired *desiredModpack, force bool) (*Result, error) {
		return p.installVanilla(ctx, server)
	},
	storage.ModLoaderFabric: func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, desired *desiredModpack, force bool) (*Result, error) {
		return p.installFabric(ctx, server, "")
	},
	storage.ModLoaderQuilt: func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, desired *desiredModpack, force bool) (*Result, error) {
		return p.installQuilt(ctx, server, cfg, "")
	},
	storage.ModLoaderForge: func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, desired *desiredModpack, force bool) (*Result, error) {
		return p.installForge(ctx, server, cfg, strVal(cfg.ForgeVersion))
	},
	storage.ModLoaderNeoForge: func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, desired *desiredModpack, force bool) (*Result, error) {
		return p.installNeoForge(ctx, server, cfg, strVal(cfg.ForgeVersion))
	},
	storage.ModLoaderPaper: func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, desired *desiredModpack, force bool) (*Result, error) {
		return p.installPaperMC(ctx, server, "paper")
	},
	storage.ModLoaderFolia: func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, desired *desiredModpack, force bool) (*Result, error) {
		return p.installPaperMC(ctx, server, "folia")
	},
	storage.ModLoaderPurpur: func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, desired *desiredModpack, force bool) (*Result, error) {
		return p.installPurpur(ctx, server)
	},
	storage.ModLoaderAutoCurseForge: func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, desired *desiredModpack, force bool) (*Result, error) {
		return p.installCurseForgePack(ctx, server, cfg, desired, force)
	},
	storage.ModLoaderCurseForge: func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, desired *desiredModpack, force bool) (*Result, error) {
		return p.installCurseForgePack(ctx, server, cfg, desired, force)
	},
	storage.ModLoaderModrinth: func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, desired *desiredModpack, force bool) (*Result, error) {
		return p.installModrinthPack(ctx, server, cfg, desired, force)
	},
	storage.ModLoaderCustom: func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, desired *desiredModpack, force bool) (*Result, error) {
		return p.installCustom(ctx, server, cfg)
	},
}

// Reports whether the loader installs without user files
func HasNativeInstaller(loader storage.ModLoader) bool {
	_, ok := loaderInstallers[loader]
	return ok
}

// Loader installs that take a pinned version, packs use these
var packLoaderInstallers = map[storage.ModLoader]func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, version string) (*Result, error){
	storage.ModLoaderFabric: func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, version string) (*Result, error) {
		return p.installFabric(ctx, server, version)
	},
	storage.ModLoaderQuilt: func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, version string) (*Result, error) {
		return p.installQuilt(ctx, server, cfg, version)
	},
	storage.ModLoaderForge: func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, version string) (*Result, error) {
		return p.installForge(ctx, server, cfg, version)
	},
	storage.ModLoaderNeoForge: func(p *Provisioner, ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, version string) (*Result, error) {
		return p.installNeoForge(ctx, server, cfg, version)
	},
}

// Installs a pack's pinned loader, the pack MC version wins
func (p *Provisioner) installLoaderForPack(ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, loader storage.ModLoader, version, mcVersion string) (*Result, error) {
	fn, ok := packLoaderInstallers[loader]
	if !ok {
		return nil, fmt.Errorf("packs cannot install loader %q", loader)
	}
	packServer := *server
	if mcVersion != "" {
		packServer.MCVersion = mcVersion
	}
	result, err := fn(p, ctx, &packServer, cfg, version)
	if err != nil {
		return nil, err
	}
	// Reports pack platform as loader, keeps resolved version
	result.Loader = server.ModLoader
	return result, nil
}

// Performs the actual installation for the server's loader
func (p *Provisioner) install(ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, desired *desiredModpack, force bool) (*Result, error) {
	p.progress(server, "provisioning %s server (MC %s)...", server.ModLoader, server.MCVersion)
	p.pruneCaches()

	if fn, ok := loaderInstallers[server.ModLoader]; ok {
		return fn(p, ctx, server, cfg, desired, force)
	}
	// Loaders without a native installer still use custom-jar
	if strVal(cfg.CustomServer) != "" || strVal(cfg.CustomJarExec) != "" {
		return p.installCustom(ctx, server, cfg)
	}
	return nil, fmt.Errorf(
		"mod loader %q has no native DiscoPanel installer: upload your server files to the data directory and set the Custom Server JAR or Custom JAR Execution fields",
		server.ModLoader)
}

// Computes java requirement, writes launch spec, builds Result
func (p *Provisioner) finishLaunch(server *storage.Server, spec *runtimespec.LaunchSpec, loader storage.ModLoader, loaderVersion, mcVersion string) (*Result, error) {
	javaMajor := docker.RequiredJavaMajor(mcVersion)
	spec.Version = 1
	spec.Loader = string(loader)
	spec.MCVersion = mcVersion
	spec.JavaMajor = javaMajor

	if !launchTargetExists(server.DataPath, spec) {
		return nil, fmt.Errorf("launch target %q missing after installation", launchTarget(spec))
	}
	if err := runtimespec.WriteLaunchSpec(server.DataPath, spec); err != nil {
		return nil, fmt.Errorf("failed to write launch spec: %w", err)
	}

	return &Result{
		Loader:        loader,
		LoaderVersion: loaderVersion,
		MCVersion:     mcVersion,
		JavaMajor:     javaMajor,
	}, nil
}

func launchTarget(spec *runtimespec.LaunchSpec) string {
	switch spec.Kind {
	case runtimespec.LaunchKindJar:
		return spec.Jar
	case runtimespec.LaunchKindArgsFile:
		return spec.ArgsFile
	default:
		return spec.Exec
	}
}

func launchTargetExists(dataPath string, spec *runtimespec.LaunchSpec) bool {
	switch spec.Kind {
	case runtimespec.LaunchKindJar:
		return fileExists(joinData(dataPath, spec.Jar))
	case runtimespec.LaunchKindArgsFile:
		return fileExists(joinData(dataPath, spec.ArgsFile))
	case runtimespec.LaunchKindCustom:
		return spec.Exec != ""
	default:
		return false
	}
}

func (p *Provisioner) disableClientOnlyMods(ctx context.Context, server *storage.Server, forceIncludes []string) {
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if modsDir == "" {
		return
	}
	for _, meta := range minecraft.ScanModsDir(modsDir) {
		if !meta.ClientOnly || minecraft.MatchesPatterns(meta.FileName, forceIncludes) {
			continue
		}
		if err := minecraft.DisableModJar(modsDir, meta.FileName); err != nil {
			p.progress(server, "could not disable client-only mod %s (%v)", meta.FileName, err)
			continue
		}
		p.action(ctx, server, "mod check", "mod.disable", activity.Attrs{"file": meta.FileName, "reason": "client-only"}, "disabled client-only mod %s", meta.FileName)
	}
}

func (p *Provisioner) runInstallerContainer(ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, cmd []string) error {
	javaMajor := docker.RequiredJavaMajor(server.MCVersion)
	image := p.docker.RuntimeImage(javaMajor)

	uid := 1000
	gid := 1000
	if cfg.UID != nil {
		uid = *cfg.UID
	}
	if cfg.GID != nil {
		gid = *cfg.GID
	}

	opts := docker.OneShotOptions{
		Image:      image,
		Cmd:        cmd,
		DataPath:   server.DataPath,
		WorkingDir: "/data",
		User:       fmt.Sprintf("%d:%d", uid, gid),
		Name:       fmt.Sprintf("discopanel-install-%s", server.ID),
		Labels: map[string]string{
			"discopanel.server.id": server.ID,
			"discopanel.managed":   "true",
			"discopanel.oneshot":   "true",
		},
	}

	return p.docker.RunOneShot(ctx, opts, func(line string) {
		p.progress(server, "[installer] %s", line)
	})
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func boolVal(b *bool) bool {
	return b != nil && *b
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

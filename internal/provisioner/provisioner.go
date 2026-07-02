// Package provisioner prepares Minecraft server data directories panel-side:
// it downloads server distributions, installs modpacks, writes configuration
// files (server.properties, eula.txt, ops/whitelist), and records a launch
// spec that the discopanel-runtime container entrypoint executes. Containers
// never install anything themselves.
package provisioner

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/pkg/logger"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
)

// ProgressSink receives human-readable provisioning progress lines for a server.
type ProgressSink func(serverID string, message string)

type Provisioner struct {
	store  *storage.Store
	docker *docker.Client
	cfg    *config.Config
	log    *logger.Logger
	sink   ProgressSink

	mu     sync.Mutex
	active map[string]*sync.Mutex // per-server provisioning locks
}

// Result reports what a successful provision resolved.
type Result struct {
	Loader        storage.ModLoader
	LoaderVersion string
	MCVersion     string
	JavaMajor     int
}

func New(store *storage.Store, dockerClient *docker.Client, cfg *config.Config, log *logger.Logger) *Provisioner {
	return &Provisioner{
		store:  store,
		docker: dockerClient,
		cfg:    cfg,
		log:    log,
		active: make(map[string]*sync.Mutex),
	}
}

// SetProgressSink registers a sink for progress lines (e.g. the log streamer).
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

// desiredModpack identifies the modpack a server should be provisioned from.
type desiredModpack struct {
	source    string // "curseforge" | "modrinth" | "zip"
	id        string
	versionID string // empty means "whatever is installed / latest on install"
}

func (d *desiredModpack) key() string {
	if d == nil {
		return ""
	}
	return d.source + ":" + d.id
}

// Ensure brings the server data directory to the desired provisioned state and
// guarantees a valid launch spec and configuration files exist. It is
// idempotent: matching manifests skip installation, and configuration files
// are (re)applied on every call.
func (p *Provisioner) Ensure(ctx context.Context, server *storage.Server, cfg *storage.ServerConfig) (*Result, error) {
	lock := p.serverLock(server.ID)
	lock.Lock()
	defer lock.Unlock()

	if err := os.MkdirAll(server.DataPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create server data directory: %w", err)
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
		p.progress(server, "provisioning complete (%s %s, MC %s, Java %d)",
			result.Loader, result.LoaderVersion, result.MCVersion, result.JavaMajor)
	} else {
		result = &Result{
			Loader:        storage.ModLoader(manifest.Loader),
			LoaderVersion: manifest.LoaderVersion,
			MCVersion:     manifest.MCVersion,
			JavaMajor:     manifest.JavaMajor,
		}
	}

	// Configuration files are cheap and authoritative from the DB - always applied.
	if err := p.applyConfigFiles(ctx, server, cfg, result.MCVersion); err != nil {
		return nil, fmt.Errorf("failed to apply configuration files: %w", err)
	}

	return result, nil
}

// needsInstall decides whether server files must be (re)installed.
func (p *Provisioner) needsInstall(server *storage.Server, manifest *runtimespec.Manifest, desired *desiredModpack, force bool) bool {
	if force || manifest == nil {
		return true
	}
	if manifest.Loader != string(server.ModLoader) {
		return true
	}
	// Modpack servers derive their MC version from the pack; plain servers from the row.
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

	// The launch target must still exist on disk.
	spec, err := runtimespec.ReadLaunchSpec(server.DataPath)
	if err != nil {
		return true
	}
	return !launchTargetExists(server.DataPath, spec)
}

// desiredModpackFor derives the modpack identity from server + config.
func (p *Provisioner) desiredModpackFor(server *storage.Server, cfg *storage.ServerConfig) *desiredModpack {
	switch server.ModLoader {
	case storage.ModLoaderAutoCurseForge, storage.ModLoaderCurseForge:
		if v := strVal(cfg.CFModpackZip); v != "" {
			return &desiredModpack{source: "zip", id: v}
		}
		slug, fileID := parseCurseForgeRef(strVal(cfg.CFPageURL), strVal(cfg.CFSlug), strVal(cfg.CFFileID))
		return &desiredModpack{source: "curseforge", id: slug, versionID: fileID}
	case storage.ModLoaderModrinth:
		project, version := parseModrinthRef(strVal(cfg.ModrinthModpack), strVal(cfg.ModrinthVersion))
		return &desiredModpack{source: "modrinth", id: project, versionID: version}
	default:
		return nil
	}
}

// install performs the actual installation for the server's loader.
func (p *Provisioner) install(ctx context.Context, server *storage.Server, cfg *storage.ServerConfig, desired *desiredModpack, force bool) (*Result, error) {
	p.progress(server, "provisioning %s server (MC %s)...", server.ModLoader, server.MCVersion)

	switch server.ModLoader {
	case storage.ModLoaderVanilla:
		return p.installVanilla(ctx, server)
	case storage.ModLoaderFabric:
		return p.installFabric(ctx, server, "")
	case storage.ModLoaderQuilt:
		return p.installQuilt(ctx, server, cfg, "")
	case storage.ModLoaderForge:
		return p.installForge(ctx, server, cfg, strVal(cfg.ForgeVersion))
	case storage.ModLoaderNeoForge:
		return p.installNeoForge(ctx, server, cfg, strVal(cfg.ForgeVersion))
	case storage.ModLoaderPaper:
		return p.installPaperMC(ctx, server, "paper")
	case storage.ModLoaderFolia:
		return p.installPaperMC(ctx, server, "folia")
	case storage.ModLoaderPurpur:
		return p.installPurpur(ctx, server)
	case storage.ModLoaderAutoCurseForge, storage.ModLoaderCurseForge:
		return p.installCurseForgePack(ctx, server, cfg, desired, force)
	case storage.ModLoaderModrinth:
		return p.installModrinthPack(ctx, server, cfg, desired, force)
	case storage.ModLoaderCustom:
		return p.installCustom(ctx, server, cfg)
	default:
		// Loaders without a native installer still work via the custom-jar path.
		if strVal(cfg.CustomServer) != "" || strVal(cfg.CustomJarExec) != "" {
			return p.installCustom(ctx, server, cfg)
		}
		return nil, fmt.Errorf(
			"mod loader %q has no native DiscoPanel installer: upload your server files to the data directory and set the Custom Server JAR or Custom JAR Execution fields",
			server.ModLoader)
	}
}

// finishLaunch computes the java requirement, writes the launch spec, and
// builds the Result. loaderVersion may be empty for vanilla-like loaders.
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

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func boolVal(b *bool) bool {
	return b != nil && *b
}

func intVal(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

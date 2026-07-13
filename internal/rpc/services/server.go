package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/nickheyer/discopanel/internal/activity"
	"github.com/nickheyer/discopanel/internal/command"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/events"
	"github.com/nickheyer/discopanel/internal/lifecycle"
	"github.com/nickheyer/discopanel/internal/metrics"
	"github.com/nickheyer/discopanel/internal/minecraft"
	"github.com/nickheyer/discopanel/internal/module"
	"github.com/nickheyer/discopanel/internal/provisioner"
	"github.com/nickheyer/discopanel/internal/proxy"
	"github.com/nickheyer/discopanel/pkg/files"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Compile-time check that ServerService implements the interface
var _ discopanelv1connect.ServerServiceHandler = (*ServerService)(nil)

// ServerService implements the Server service
type ServerService struct {
	store            *storage.Store
	docker           *docker.Client
	sender           *command.Sender
	config           *config.Config
	proxy            *proxy.Manager
	lifecycle        *lifecycle.Manager
	rec              *activity.Recorder
	log              *logger.Logger
	logStreamer      *logger.LogStreamer
	metricsCollector *metrics.Collector
	moduleManager    *module.Manager
	bus              *events.Bus

	// Encoded server-icon.png keyed by server, checked by mtime
	faviconMu sync.Mutex
	favicons  map[string]faviconEntry
}

type faviconEntry struct {
	modTime time.Time
	size    int64
	dataURI string
}

// NewServerService creates a new server service
func NewServerService(store *storage.Store, docker *docker.Client, sender *command.Sender, config *config.Config, proxy *proxy.Manager, lifecycleManager *lifecycle.Manager, logStreamer *logger.LogStreamer, metricsCollector *metrics.Collector, moduleManager *module.Manager, bus *events.Bus, rec *activity.Recorder, log *logger.Logger) *ServerService {
	return &ServerService{
		store:            store,
		docker:           docker,
		sender:           sender,
		config:           config,
		proxy:            proxy,
		lifecycle:        lifecycleManager,
		rec:              rec,
		log:              log,
		logStreamer:      logStreamer,
		metricsCollector: metricsCollector,
		moduleManager:    moduleManager,
		bus:              bus,
		favicons:         make(map[string]faviconEntry),
	}
}

// Detaches request work from cancellation, values ride along
func detach(ctx context.Context) context.Context {
	return context.WithoutCancel(ctx)
}

// Serves server-icon.png from disk, cached by file identity
func (s *ServerService) serverFavicon(server *storage.Server) string {
	if server.DataPath == "" {
		return ""
	}
	iconPath := filepath.Join(server.DataPath, "server-icon.png")
	info, err := os.Stat(iconPath)
	if err != nil {
		return ""
	}
	s.faviconMu.Lock()
	defer s.faviconMu.Unlock()
	if e, ok := s.favicons[server.ID]; ok && e.modTime.Equal(info.ModTime()) && e.size == info.Size() {
		return e.dataURI
	}
	data, err := os.ReadFile(iconPath)
	if err != nil {
		return ""
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	s.favicons[server.ID] = faviconEntry{modTime: info.ModTime(), size: info.Size(), dataURI: uri}
	return uri
}

// applyMetrics copies the collector's cached runtime stats onto the server
// row's transient fields (shared by ListServers and GetServer).
func (s *ServerService) applyMetrics(server *storage.Server) {
	if s.metricsCollector == nil {
		return
	}
	m := s.metricsCollector.GetMetrics(server.ID)
	if m == nil {
		return
	}
	server.MemoryUsage = m.MemoryUsage
	server.CPUPercent = m.CPUPercent
	server.CPUCores = m.CPUCount
	server.DiskUsage = m.DiskUsage
	server.DiskTotal = m.DiskTotal
	server.DiskUsed = m.DiskUsed
	server.WorldSize = m.WorldSize
	server.PlayersOnline = m.PlayersOnline
	server.TPS = m.TPS

	// SLP fields
	server.SLPAvailable = m.SLPAvailable
	server.SLPLatencyMs = m.SLPLatencyMs
	server.MOTD = m.MOTD
	server.ServerVersion = m.ServerVersion
	server.ProtocolVersion = m.ProtocolVersion
	server.PlayerSample = m.PlayerSample
	server.MaxPlayersSLP = m.MaxPlayers

	// Agent-sourced fields
	server.AgentConnected = m.AgentConnected
	server.MSPT = m.MSPT
	server.HeapUsedMB = m.HeapUsedMB
	server.HeapMaxMB = m.HeapMaxMB
	server.CPUThrottlePercent = m.CPUThrottlePercent
}

func dbServerToProto(server *storage.Server) *v1.Server {
	if server == nil {
		return nil
	}

	// Convert JavaVersion string to int32
	javaVersion, _ := strconv.ParseInt(server.JavaVersion, 10, 32)

	protoServer := &v1.Server{
		Id:              server.ID,
		Name:            server.Name,
		Description:     server.Description,
		McVersion:       server.MCVersion,
		Port:            int32(server.Port),
		ProxyHostname:   server.ProxyHostname,
		ProxyListenerId: server.ProxyListenerID,
		ProxyPort:       int32(server.ProxyPort),
		MaxPlayers:      int32(server.MaxPlayers),
		Memory:          int32(server.Memory),
		MemoryMin:       int32(server.MemoryMin),
		MemoryMax:       int32(server.MemoryMax),
		DataPath:        server.DataPath,
		ContainerId:     server.ContainerID,
		JavaVersion:     int32(javaVersion),
		DockerImage:     server.DockerImage,
		RuntimeDigest:   server.RuntimeDigest,
		AutoStart:       server.AutoStart,
		Detached:        server.Detached,
		MemoryUsage:     int64(server.MemoryUsage),
		CpuPercent:      server.CPUPercent,
		CpuCores:        int32(server.CPUCores),
		DiskUsage:       server.DiskUsage,
		DiskTotal:       server.DiskTotal,
		DiskUsed:        server.DiskUsed,
		WorldSize:       server.WorldSize,
		PlayersOnline:   int32(server.PlayersOnline),
		Tps:             server.TPS,
		AdditionalPorts: server.AdditionalPorts,
		CreatedAt:       timestamppb.New(server.CreatedAt),
		UpdatedAt:       timestamppb.New(server.UpdatedAt),

		// SLP fields
		SlpAvailable:    server.SLPAvailable,
		SlpLatencyMs:    server.SLPLatencyMs,
		Motd:            server.MOTD,
		ServerVersion:   server.ServerVersion,
		ProtocolVersion: int32(server.ProtocolVersion),
		PlayerSample:    server.PlayerSample,
		MaxPlayersSlp:   int32(server.MaxPlayersSLP),
		Favicon:         server.Favicon,

		// Agent fields
		AgentConnected:     server.AgentConnected,
		Mspt:               server.MSPT,
		HeapUsedMb:         server.HeapUsedMB,
		HeapMaxMb:          server.HeapMaxMB,
		CpuThrottlePercent: server.CPUThrottlePercent,
	}

	// Apply overrides
	protoServer.DockerOverrides = server.DockerOverrides

	// Map mod loader
	protoServer.ModLoader = dbModLoaderToProto(server.ModLoader)

	// Map status
	protoServer.Status = dbStatusToProto(server.Status)

	// Map optional last started
	if server.LastStarted != nil {
		protoServer.LastStarted = timestamppb.New(*server.LastStarted)
	}

	return protoServer
}

// Converts database mod loader to proto, unknown reads unspecified
func dbModLoaderToProto(loader storage.ModLoader) v1.ModLoader {
	return minecraft.ProtoFor(loader)
}

// Converts proto mod loader to database, unspecified defaults vanilla
func protoModLoaderToDB(loader v1.ModLoader) storage.ModLoader {
	if l, ok := minecraft.LoaderFromProto(loader); ok {
		return l
	}
	return storage.ModLoaderVanilla
}

// dbStatusToProto converts database status to proto
func dbStatusToProto(status storage.ServerStatus) v1.ServerStatus {
	switch status {
	case storage.StatusCreating:
		return v1.ServerStatus_SERVER_STATUS_CREATING
	case storage.StatusStarting:
		return v1.ServerStatus_SERVER_STATUS_STARTING
	case storage.StatusRunning:
		return v1.ServerStatus_SERVER_STATUS_RUNNING
	case storage.StatusStopping:
		return v1.ServerStatus_SERVER_STATUS_STOPPING
	case storage.StatusStopped:
		return v1.ServerStatus_SERVER_STATUS_STOPPED
	case storage.StatusError:
		return v1.ServerStatus_SERVER_STATUS_ERROR
	case storage.StatusUnhealthy:
		return v1.ServerStatus_SERVER_STATUS_UNHEALTHY
	case storage.StatusProvisioning:
		return v1.ServerStatus_SERVER_STATUS_PROVISIONING
	case storage.StatusPaused:
		return v1.ServerStatus_SERVER_STATUS_PAUSED
	default:
		return v1.ServerStatus_SERVER_STATUS_UNSPECIFIED
	}
}

// ListServers lists all servers
func (s *ServerService) ListServers(ctx context.Context, req *connect.Request[v1.ListServersRequest]) (*connect.Response[v1.ListServersResponse], error) {
	servers, err := s.store.ListServers(ctx)
	if err != nil {
		s.log.Error("Failed to list servers: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list servers"))
	}

	// Get all proxy listeners once for efficiency
	var listeners map[string]*storage.ProxyListener
	if s.config.Proxy.Enabled {
		allListeners, err := s.store.GetProxyListeners(ctx)
		if err == nil {
			listeners = make(map[string]*storage.ProxyListener)
			for _, l := range allListeners {
				listeners[l.ID] = l
			}
		}
	}

	// Update status from Docker and apply cached metrics
	for _, server := range servers {
		// If server uses proxy, ensure ProxyPort is populated from the listener
		if server.ProxyHostname != "" && server.ProxyListenerID != "" && listeners != nil {
			if listener, ok := listeners[server.ProxyListenerID]; ok {
				server.ProxyPort = listener.Port
			}
		}

		// Icon comes from disk, cheap enough for light polls
		server.Favicon = s.serverFavicon(server)

		// Stored status only unless the caller wants live stats
		if server.ContainerID != "" && req.Msg.FullStats {
			status, err := s.docker.GetContainerStatus(ctx, server.ContainerID)
			if err == nil {
				server.Status = status
			}

			// Apply cached metrics from the background collector
			s.applyMetrics(server)
		}
	}

	// Convert to proto
	protoServers := make([]*v1.Server, len(servers))
	for i, server := range servers {
		protoServers[i] = dbServerToProto(server)
	}

	return connect.NewResponse(&v1.ListServersResponse{
		Servers: protoServers,
	}), nil
}

// GetServer gets a specific server
func (s *ServerService) GetServer(ctx context.Context, req *connect.Request[v1.GetServerRequest]) (*connect.Response[v1.GetServerResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// If server uses proxy, ensure ProxyPort is populated from the listener
	if server.ProxyHostname != "" && server.ProxyListenerID != "" {
		listener, err := s.store.GetProxyListener(ctx, server.ProxyListenerID)
		if err == nil && listener != nil {
			server.ProxyPort = listener.Port
		}
	}

	// Update status from Docker
	if server.ContainerID != "" {
		status, err := s.docker.GetContainerStatus(ctx, server.ContainerID)
		if err == nil {
			server.Status = status
		}
	}

	// Apply cached metrics from the background collector
	s.applyMetrics(server)
	server.Favicon = s.serverFavicon(server)

	return connect.NewResponse(&v1.GetServerResponse{
		Server: dbServerToProto(server),
	}), nil
}

// CreateServer creates a new server
// Fills heap defaults then validates the memory trio
func normalizeServerMemory(server *storage.Server) error {
	if server.Memory < 1024 {
		return fmt.Errorf("server memory must be at least 1024 MB")
	}
	defInit, defMax := storage.DefaultHeapForMemory(server.Memory)
	if server.MemoryMax <= 0 {
		server.MemoryMax = defMax
	}
	if server.MemoryMin <= 0 {
		server.MemoryMin = min(defInit, server.MemoryMax)
	}
	if server.MemoryMin > server.MemoryMax {
		return fmt.Errorf("initial heap %d MB exceeds max heap %d MB", server.MemoryMin, server.MemoryMax)
	}
	if server.MemoryMax > server.Memory {
		return fmt.Errorf("max heap %d MB exceeds server memory %d MB", server.MemoryMax, server.Memory)
	}
	return nil
}

func (s *ServerService) CreateServer(ctx context.Context, req *connect.Request[v1.CreateServerRequest]) (*connect.Response[v1.CreateServerResponse], error) {
	msg := req.Msg

	// Convert mod loader from proto
	modLoader := protoModLoaderToDB(msg.ModLoader)

	// If modpack is selected, load it and derive settings
	var modpackURL string
	if msg.ModpackId != "" {
		modpack, err := s.store.GetIndexedModpack(ctx, msg.ModpackId)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid modpack"))
		}

		// Set the modpack URL
		modpackURL = modpack.WebsiteURL

		// Override mod loader based on indexer
		switch modpack.Indexer {
		case "fuego", "manual":
			modLoader = storage.ModLoaderAutoCurseForge
		case "modrinth":
			modLoader = storage.ModLoaderModrinth
		}

		// Get MC version from modpack if not explicitly set
		if msg.McVersion == "" {
			var gameVersions []string
			if err := json.Unmarshal([]byte(modpack.GameVersions), &gameVersions); err == nil && len(gameVersions) > 0 {
				msg.McVersion = minecraft.FindMostRecentMinecraftVersion(gameVersions)
			}
		}

		// Set minimum memory for modpacks
		if msg.Memory < 4096 {
			msg.Memory = 4096
		}
	}

	// Validate request
	if msg.Name == "" || msg.McVersion == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name and MC version are required"))
	}

	// Handle proxy configuration
	proxyHostname := msg.ProxyHostname
	proxyListenerID := msg.ProxyListenerId
	port := int(msg.Port)

	if proxyHostname != "" {
		// If using base URL, append it to the hostname
		if msg.UseBaseUrl {
			proxyConfig, _, err := s.store.GetProxyConfig(ctx)
			if err == nil && proxyConfig.BaseURL != "" {
				// Only append base URL if hostname doesn't already contain a domain
				if !strings.Contains(proxyHostname, ".") {
					proxyHostname = proxyHostname + "." + proxyConfig.BaseURL
				}
			}
		}

		// Validate listener selection
		if proxyListenerID != "" {
			listener, err := s.store.GetProxyListener(ctx, proxyListenerID)
			if err != nil || !listener.Enabled {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid or disabled proxy listener"))
			}
			port = listener.Port
		} else {
			// No listener specified, get the default one
			listeners, err := s.store.GetProxyListeners(ctx)
			if err != nil || len(listeners) == 0 {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("no proxy listeners configured"))
			}

			// Find default or first enabled listener
			var defaultListener *storage.ProxyListener
			for _, l := range listeners {
				if l.IsDefault && l.Enabled {
					defaultListener = l
					break
				}
			}
			if defaultListener == nil {
				for _, l := range listeners {
					if l.Enabled {
						defaultListener = l
						break
					}
				}
			}
			if defaultListener == nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("no enabled proxy listeners available"))
			}
			proxyListenerID = defaultListener.ID
			port = defaultListener.Port
		}
	} else {
		// For non-proxy servers, must have a unique port
		if port == 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port is required for non-proxy servers"))
		}

		// Check if port is already in use
		existing, err := s.store.GetServerByPort(ctx, port)
		if err != nil {
			s.log.Error("Failed to check port: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check port availability"))
		}
		if existing != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port already in use"))
		}

		// Also check if this port is used by the proxy
		if s.config.Proxy.Enabled {
			if slices.Contains(s.config.Proxy.ListenPorts, port) {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port is already in use by the proxy server"))
			}
		}
	}

	// Determine runtime image tag if not specified or stale (pre-runtime tags)
	dockerImage := msg.DockerImage
	if !docker.IsValidRuntimeTag(dockerImage) {
		dockerImage = docker.OptimalRuntimeTag(msg.McVersion)
	}

	// Validate additional ports
	var additionalPorts []*v1.AdditionalPort
	usedPorts := make(map[string]bool)

	for _, protoPort := range msg.AdditionalPorts {
		// Validate port range
		if protoPort.ContainerPort < 1 || protoPort.ContainerPort > 65535 {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid container port %d", protoPort.ContainerPort))
		}
		if protoPort.HostPort < 1 || protoPort.HostPort > 65535 {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid host port %d", protoPort.HostPort))
		}

		// Default protocol to TCP
		protocol := protoPort.Protocol
		if protocol == "" {
			protocol = "tcp"
		} else if protocol != "tcp" && protocol != "udp" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid protocol %s (must be tcp or udp)", protocol))
		}

		// Check for duplicate ports
		portKey := fmt.Sprintf("%d/%s", protoPort.HostPort, protocol)
		if usedPorts[portKey] {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("duplicate host port %d/%s", protoPort.HostPort, protocol))
		}
		usedPorts[portKey] = true

		// Check if port conflicts
		if int(protoPort.HostPort) == port {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("additional port %d conflicts with main server port", protoPort.HostPort))
		}
		if s.config.Proxy.Enabled && slices.Contains(s.config.Proxy.ListenPorts, int(protoPort.HostPort)) {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port %d is already in use by the proxy server", protoPort.HostPort))
		}

		additionalPorts = append(additionalPorts, &v1.AdditionalPort{
			ContainerPort: protoPort.ContainerPort,
			HostPort:      protoPort.HostPort,
			Protocol:      protocol,
			Name:          protoPort.Name,
		})
	}

	// Create server object
	serverUUID := uuid.New().String()
	serverDataDir := fmt.Sprintf("%s_%s", files.SanitizePathName(msg.Name), serverUUID)
	serverDataPath := filepath.Join(s.config.Storage.DataDir, "servers", serverDataDir)

	server := &storage.Server{
		ID:              serverUUID,
		Name:            msg.Name,
		Description:     msg.Description,
		ModLoader:       modLoader,
		MCVersion:       msg.McVersion,
		Status:          storage.StatusCreating,
		Port:            port,
		ProxyHostname:   proxyHostname,
		ProxyListenerID: proxyListenerID,
		MaxPlayers:      int(msg.MaxPlayers),
		Memory:          int(msg.Memory),
		MemoryMin:       int(msg.MemoryMin),
		MemoryMax:       int(msg.MemoryMax),
		DataPath:        serverDataPath,
		JavaVersion:     docker.GetRequiredJavaVersion(msg.McVersion, modLoader),
		DockerImage:     dockerImage,
		AutoStart:       msg.AutoStart,
		Detached:        msg.Detached,
		AdditionalPorts: additionalPorts,
		DockerOverrides: msg.DockerOverrides,
	}

	// Set defaults
	if server.MaxPlayers == 0 {
		server.MaxPlayers = 20
	}
	if server.Memory == 0 {
		server.Memory = 4096
	}
	if server.ModLoader == "" {
		server.ModLoader = storage.ModLoaderVanilla
	}

	if err := normalizeServerMemory(server); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// When using proxy, set the ports correctly
	if server.ProxyHostname != "" && proxyListenerID != "" {
		listener, err := s.store.GetProxyListener(ctx, proxyListenerID)
		if err == nil && listener != nil {
			server.ProxyPort = listener.Port
			server.Port = 25565 // Internal container port for proxied servers
		}
	}

	// Create data directory
	if err := os.MkdirAll(server.DataPath, 0755); err != nil {
		s.log.Error("Failed to create data directory: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create server directory"))
	}

	// Save to database
	if err := s.store.CreateServer(ctx, server); err != nil {
		s.log.Error("Failed to create server: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create server"))
	}
	s.rec.Record(ctx, server.ID, "server.create", nil, "created the server")

	// Get the server config
	serverConfig, err := s.store.GetServerProperties(ctx, server.ID)
	if err != nil {
		s.log.Error("Failed to get server config: %v", err)
		serverConfig = s.store.CreateDefaultServerProperties(server.ID)
	}

	// Reflects heap sizing into read-only properties
	serverConfig.SyncMemoryFromServer(server)

	if err := s.store.UpdateServerProperties(ctx, serverConfig); err != nil {
		s.log.Error("Failed to update server config with memory settings: %v", err)
	}

	// Configure modpack if selected
	if msg.ModpackId != "" {
		modpack, _ := s.store.GetIndexedModpack(ctx, msg.ModpackId)
		if modpack != nil && modpack.Indexer == "manual" {
			// For manual modpacks, copy the zip file
			modpackFile, err := s.store.GetIndexedModpackFiles(ctx, msg.ModpackId)
			if err == nil && len(modpackFile) > 0 {
				sourcePath := modpackFile[0].DownloadURL
				destPath := filepath.Join(server.DataPath, "modpack.zip")

				// Copy the modpack file
				if sourceFile, err := os.Open(sourcePath); err == nil {
					defer sourceFile.Close()
					if destFile, err := os.Create(destPath); err == nil {
						defer destFile.Close()
						io.Copy(destFile, sourceFile)

						// Set CF_MODPACK_ZIP for manual modpack
						cfModpackZip := "/data/modpack.zip"
						serverConfig.CFModpackZip = &cfModpackZip

						// Set a dummy slug
						cfSlug := "manual-" + modpack.ID
						serverConfig.CFSlug = &cfSlug
					}
				}
			}
		} else if modpackURL != "" && server.ModLoader == storage.ModLoaderAutoCurseForge {
			// If version is pinned, append /files/<id>
			if msg.ModpackVersionId != "" {
				versionedURL := fmt.Sprintf("%s/files/%s", modpackURL, msg.ModpackVersionId)
				serverConfig.CFPageURL = &versionedURL
			} else {
				serverConfig.CFPageURL = &modpackURL
			}
		} else if modpack != nil && modpack.Indexer == "modrinth" {
			var projectSpec string
			if msg.ModpackVersionId != "" && msg.ModpackVersionId != "latest" {
				projectSpec = fmt.Sprintf("%s:%s", modpack.IndexerID, msg.ModpackVersionId)
				s.log.Info("Using specific Modrinth version: %s", projectSpec)
			} else {
				projectSpec = modpack.IndexerID
				s.log.Info("Using latest Modrinth version for project: %s", projectSpec)
			}
			serverConfig.ModrinthModpack = &projectSpec
			downloadDeps := "required"
			serverConfig.ModrinthDownloadDependencies = &downloadDeps

			// Only set version type when using latest
			if msg.ModpackVersionId == "" || msg.ModpackVersionId == "latest" {
				versionType := "release"
				serverConfig.ModrinthModpackVersionType = &versionType
			}
		}

		// Update config with modpack settings
		if err := s.store.UpdateServerProperties(ctx, serverConfig); err != nil {
			s.log.Error("Failed to update server config with modpack settings: %v", err)
		}

		// Pack art becomes the server icon like an upload would
		if modpack != nil {
			s.adoptModpackIcon(ctx, server, modpack)
			s.rec.Record(ctx, server.ID, "modpack.select", activity.Attrs{"modpack": modpack.Name}, "selected modpack %s", modpack.Name)
		}
	}

	// Provisioning and container creation happen on first start.
	if msg.StartImmediately {
		server.Status = storage.StatusProvisioning
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server status: %v", err)
		}
		go func() {
			bgCtx, cancel := context.WithTimeout(detach(ctx), 2*time.Hour)
			defer cancel()
			if err := s.lifecycle.Start(bgCtx, server.ID); err != nil {
				s.log.Error("Failed to start newly created server %s: %v", server.Name, err)
			}
		}()
	} else {
		server.Status = storage.StatusStopped
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server status: %v", err)
		}
		s.log.Info("Server %s created but not started immediately", server.ID)
	}

	return connect.NewResponse(&v1.CreateServerResponse{
		Server: dbServerToProto(server),
	}), nil
}

// UpdateServer updates a server
func (s *ServerService) UpdateServer(ctx context.Context, req *connect.Request[v1.UpdateServerRequest]) (*connect.Response[v1.UpdateServerResponse], error) {
	msg := req.Msg

	server, err := s.store.GetServer(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Check if container recreation is needed
	needsRecreation := false
	originalMemory := server.Memory
	originalModLoader := server.ModLoader
	originalMCVersion := server.MCVersion
	originalDockerImage := server.DockerImage

	// Update fields
	if msg.Name != "" {
		server.Name = msg.Name
	}
	if msg.Description != "" {
		server.Description = msg.Description
	}
	if msg.Port != nil && int(*msg.Port) != server.Port {
		newPort := int(*msg.Port)

		if server.ProxyHostname != "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("cannot change port for proxy-enabled servers"))
		}

		if newPort < 1 || newPort > 65535 {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid port %d", newPort))
		}

		existing, err := s.store.GetServerByPort(ctx, newPort)
		if err != nil {
			s.log.Error("Failed to check port: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check port availability"))
		}
		if existing != nil && existing.ID != server.ID {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port already in use"))
		}

		if s.config.Proxy.Enabled && slices.Contains(s.config.Proxy.ListenPorts, newPort) {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port is already in use by the proxy server"))
		}

		server.Port = newPort
		needsRecreation = true
	}
	if msg.MaxPlayers > 0 {
		server.MaxPlayers = int(msg.MaxPlayers)
		needsRecreation = true
	}
	if msg.Memory > 0 || msg.MemoryMin > 0 || msg.MemoryMax > 0 {
		originalMemoryMin := server.MemoryMin
		originalMemoryMax := server.MemoryMax
		if msg.Memory > 0 {
			server.Memory = int(msg.Memory)
		}

		// Zero heap values rescale to defaults in normalize
		server.MemoryMin = int(msg.MemoryMin)
		server.MemoryMax = int(msg.MemoryMax)
		if err := normalizeServerMemory(server); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}

		if server.Memory != originalMemory || server.MemoryMin != originalMemoryMin || server.MemoryMax != originalMemoryMax {
			needsRecreation = true
			if err := s.store.SyncServerPropertiesWithServer(ctx, server); err != nil {
				s.log.Error("Failed to sync server config memory: %v", err)
			}
		}
	}
	if msg.ModLoader != v1.ModLoader_MOD_LOADER_UNSPECIFIED && protoModLoaderToDB(msg.ModLoader) != originalModLoader {
		server.ModLoader = protoModLoaderToDB(msg.ModLoader)
		needsRecreation = true
	}
	if msg.McVersion != "" && msg.McVersion != originalMCVersion {
		server.MCVersion = msg.McVersion
		needsRecreation = true
	}
	if msg.DockerImage != "" && msg.DockerImage != originalDockerImage {
		if !docker.IsValidRuntimeTag(msg.DockerImage) {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unknown runtime image tag %q", msg.DockerImage))
		}
		server.DockerImage = msg.DockerImage
		needsRecreation = true
	}
	if msg.AutoStart != nil {
		server.AutoStart = *msg.AutoStart
	}
	if msg.Detached != nil {
		server.Detached = *msg.Detached
	}

	// Handle additional ports update
	if len(msg.AdditionalPorts) > 0 {
		// Validate additional ports
		var additionalPorts []*v1.AdditionalPort
		usedPorts := make(map[string]bool)

		for _, protoPort := range msg.AdditionalPorts {
			// Validate port range
			if protoPort.ContainerPort < 1 || protoPort.ContainerPort > 65535 {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid container port %d", protoPort.ContainerPort))
			}
			if protoPort.HostPort < 1 || protoPort.HostPort > 65535 {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid host port %d", protoPort.HostPort))
			}

			// Default protocol to TCP
			protocol := protoPort.Protocol
			if protocol == "" {
				protocol = "tcp"
			} else if protocol != "tcp" && protocol != "udp" {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid protocol %s", protocol))
			}

			// Check for duplicate ports
			portKey := fmt.Sprintf("%d/%s", protoPort.HostPort, protocol)
			if usedPorts[portKey] {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("duplicate host port %d/%s", protoPort.HostPort, protocol))
			}
			usedPorts[portKey] = true

			// Check if port conflicts
			if int(protoPort.HostPort) == server.Port {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("additional port %d conflicts with main server port", protoPort.HostPort))
			}
			if s.config.Proxy.Enabled && slices.Contains(s.config.Proxy.ListenPorts, int(protoPort.HostPort)) {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port %d is already in use by the proxy server", protoPort.HostPort))
			}

			additionalPorts = append(additionalPorts, &v1.AdditionalPort{
				ContainerPort: protoPort.ContainerPort,
				HostPort:      protoPort.HostPort,
				Protocol:      protocol,
				Name:          protoPort.Name,
			})
		}

		server.AdditionalPorts = additionalPorts
		needsRecreation = true
	}

	// Handle docker overrides update
	if msg.DockerOverrides != nil {
		// Check that labels do not start with "discopanel."
		for key := range msg.DockerOverrides.Labels {
			if strings.HasPrefix(key, "discopanel.") {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("docker label keys cannot start with 'discopanel.', namespace reserved for internal management"))
			}
		}

		server.DockerOverrides = msg.DockerOverrides
		needsRecreation = true
	}

	// Handle modpack version update
	if msg.ModpackId != "" {
		serverConfig, err := s.store.GetServerProperties(ctx, server.ID)
		if err != nil {
			s.log.Error("Failed to get server config: %v", err)
			serverConfig = s.store.CreateDefaultServerProperties(server.ID)
		}

		modpack, err := s.store.GetIndexedModpack(ctx, msg.ModpackId)
		if err == nil {
			modpackURL := modpack.WebsiteURL

			switch modpack.Indexer {
			case "fuego", "manual":
				server.ModLoader = storage.ModLoaderAutoCurseForge
				needsRecreation = true

				if msg.ModpackVersionId != "" {
					versionedURL := fmt.Sprintf("%s/files/%s", modpackURL, msg.ModpackVersionId)
					serverConfig.CFPageURL = &versionedURL
				} else {
					serverConfig.CFPageURL = &modpackURL
				}
			case "modrinth":
				server.ModLoader = storage.ModLoaderModrinth
				needsRecreation = true

				var projectSpec string
				if msg.ModpackVersionId != "" && msg.ModpackVersionId != "latest" {
					projectSpec = fmt.Sprintf("%s:%s", modpack.IndexerID, msg.ModpackVersionId)
				} else {
					projectSpec = modpack.IndexerID
				}
				serverConfig.ModrinthModpack = &projectSpec

				downloadDeps := "required"
				serverConfig.ModrinthDownloadDependencies = &downloadDeps

				if msg.ModpackVersionId == "" || msg.ModpackVersionId == "latest" {
					versionType := "release"
					serverConfig.ModrinthModpackVersionType = &versionType
				}
			}

			if err := s.store.UpdateServerProperties(ctx, serverConfig); err != nil {
				s.log.Error("Failed to update server config with modpack settings: %v", err)
			}

			// Pack art becomes the server icon like an upload would
			s.adoptModpackIcon(ctx, server, modpack)
			s.rec.Record(ctx, server.ID, "modpack.select", activity.Attrs{"modpack": modpack.Name}, "selected modpack %s", modpack.Name)
		}
	}

	// Save server updates first
	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update server"))
	}

	if needsRecreation {
		s.recreateAfterConfigChange(ctx, server)
	}

	return connect.NewResponse(&v1.UpdateServerResponse{
		Server: dbServerToProto(server),
	}), nil
}

// Rebuilds the container after config changes, restarts if running
func (s *ServerService) recreateAfterConfigChange(ctx context.Context, server *storage.Server) bool {
	if server.ContainerID == "" {
		return false
	}

	wasRunning := false
	if status, err := s.docker.GetContainerStatus(ctx, server.ContainerID); err == nil {
		switch status {
		case storage.StatusRunning, storage.StatusStarting, storage.StatusUnhealthy, storage.StatusPaused:
			wasRunning = true
		}
	}

	// Running servers come back through the full lifecycle
	if wasRunning {
		go func() {
			bgCtx, cancel := context.WithTimeout(detach(ctx), 2*time.Hour)
			defer cancel()
			if err := s.lifecycle.Recreate(bgCtx, server.ID); err != nil {
				s.log.Error("Failed to recreate server %s after update: %v", server.Name, err)
			}
		}()
		return true
	}

	if err := s.docker.RemoveContainer(ctx, server.ContainerID); err != nil {
		s.log.Debug("Failed to remove container after update (may not exist): %v", err)
	}
	server.ContainerID = ""
	server.Status = storage.StatusStopped
	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server after container removal: %v", err)
	}
	s.rec.Record(ctx, server.ID, "container.remove", nil, "removed the container so new settings apply on next start")
	return false
}

// Adopts modpack art as the server icon, uploads win
func (s *ServerService) adoptModpackIcon(ctx context.Context, server *storage.Server, modpack *storage.IndexedModpack) {
	if server.IconSource == storage.IconSourceUpload || modpack.LogoURL == "" {
		return
	}
	iconPNG, err := provisioner.FetchServerIcon(ctx, s.config.Server.UserAgent, modpack.LogoURL)
	if err != nil {
		s.log.Warn("Modpack icon fetch failed for %s: %v", server.Name, err)
		return
	}
	if err := os.MkdirAll(server.DataPath, 0755); err != nil {
		s.log.Error("Failed to create server data dir: %v", err)
		return
	}
	if err := os.WriteFile(filepath.Join(server.DataPath, "server-icon.png"), iconPNG, 0644); err != nil {
		s.log.Error("Failed to write modpack icon: %v", err)
		return
	}
	server.IconSource = storage.IconSourceModpack
}

// UploadServerIcon converts an uploaded image into server-icon.png
func (s *ServerService) UploadServerIcon(ctx context.Context, req *connect.Request[v1.UploadServerIconRequest]) (*connect.Response[v1.UploadServerIconResponse], error) {
	const maxIconBytes = 4 << 20

	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}
	if len(req.Msg.Image) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("image data is required"))
	}
	if len(req.Msg.Image) > maxIconBytes {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("image must be under 4 MB"))
	}

	iconPNG, err := provisioner.ConvertServerIcon(bytes.NewReader(req.Msg.Image))
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unsupported image format"))
	}

	if err := os.MkdirAll(server.DataPath, 0755); err != nil {
		s.log.Error("Failed to create server data dir: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save icon"))
	}
	iconPath := filepath.Join(server.DataPath, "server-icon.png")
	if err := os.WriteFile(iconPath, iconPNG, 0644); err != nil {
		s.log.Error("Failed to write server icon: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save icon"))
	}

	server.IconSource = storage.IconSourceUpload
	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to persist icon source: %v", err)
	}
	s.rec.Record(ctx, server.ID, "icon.upload", nil, "uploaded a server icon")

	favicon := "data:image/png;base64," + base64.StdEncoding.EncodeToString(iconPNG)
	return connect.NewResponse(&v1.UploadServerIconResponse{
		Favicon: favicon,
	}), nil
}

// DeleteServer deletes a server
func (s *ServerService) DeleteServer(ctx context.Context, req *connect.Request[v1.DeleteServerRequest]) (*connect.Response[v1.DeleteServerResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Remove proxy route if configured
	if s.proxy != nil && server.ProxyHostname != "" {
		if err := s.proxy.RemoveServerRoute(server.ID); err != nil {
			s.log.Error("Failed to remove proxy route: %v", err)
		}
	}

	// Stop and remove module containers before deleting the server
	// (database cascade will delete module records, but containers need cleanup)
	if s.moduleManager != nil {
		modules, err := s.store.ListServerModules(ctx, server.ID)
		if err == nil {
			for _, mod := range modules {
				if mod.ContainerID != "" {
					if err := s.moduleManager.StopModule(ctx, mod.ID); err != nil {
						s.log.Error("Failed to stop module %s: %v", mod.ID, err)
					}
					if err := s.moduleManager.DeleteModule(ctx, mod.ID); err != nil {
						s.log.Error("Failed to delete module %s: %v", mod.ID, err)
					}
				}
			}
		}
	}

	// Stop and remove container
	if server.ContainerID != "" {
		if _, err := s.docker.StopContainer(ctx, server.ContainerID, 30); err != nil {
			s.log.Error("Failed to stop container: %v", err)
		}
		if err := s.docker.RemoveContainer(ctx, server.ContainerID); err != nil {
			s.log.Error("Failed to remove container: %v", err)
		}
	}

	// Delete from database
	if err := s.store.DeleteServer(ctx, req.Msg.Id); err != nil {
		s.log.Error("Failed to delete server: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete server"))
	}

	// Delete data directory
	if err := os.RemoveAll(server.DataPath); err != nil {
		s.log.Error("Failed to delete server data: %v", err)
	}

	return connect.NewResponse(&v1.DeleteServerResponse{}), nil
}

// StartServer starts a server (provisioning + container start run async)
func (s *ServerService) StartServer(ctx context.Context, req *connect.Request[v1.StartServerRequest]) (*connect.Response[v1.StartServerResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	if s.lifecycle.IsStarting(server.ID) {
		return connect.NewResponse(&v1.StartServerResponse{
			Status: string(storage.StatusProvisioning),
		}), nil
	}

	server.Status = storage.StatusProvisioning
	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
	}

	go func() {
		bgCtx, cancel := context.WithTimeout(detach(ctx), 2*time.Hour)
		defer cancel()
		if err := s.lifecycle.Start(bgCtx, server.ID); err != nil {
			s.log.Error("Failed to start server %s: %v", server.Name, err)
		}
	}()

	return connect.NewResponse(&v1.StartServerResponse{
		Status: string(storage.StatusProvisioning),
	}), nil
}

// StopServer stops a server (graceful stop runs async)
func (s *ServerService) StopServer(ctx context.Context, req *connect.Request[v1.StopServerRequest]) (*connect.Response[v1.StopServerResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	if server.ContainerID == "" {
		server.Status = storage.StatusStopped
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server status: %v", err)
		}
		return connect.NewResponse(&v1.StopServerResponse{
			Status: "stopped",
		}), nil
	}

	server.Status = storage.StatusStopping
	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
	}

	go func() {
		bgCtx, cancel := context.WithTimeout(detach(ctx), 15*time.Minute)
		defer cancel()
		if err := s.lifecycle.Stop(bgCtx, server.ID); err != nil {
			s.log.Error("Failed to stop server %s: %v", server.Name, err)
		}
	}()

	return connect.NewResponse(&v1.StopServerResponse{
		Status: "stopping",
	}), nil
}

// RestartServer restarts a server (runs async)
func (s *ServerService) RestartServer(ctx context.Context, req *connect.Request[v1.RestartServerRequest]) (*connect.Response[v1.RestartServerResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	go func() {
		bgCtx, cancel := context.WithTimeout(detach(ctx), 2*time.Hour)
		defer cancel()
		if err := s.lifecycle.Restart(bgCtx, server.ID); err != nil {
			s.log.Error("Failed to restart server %s: %v", server.Name, err)
		}
	}()

	return connect.NewResponse(&v1.RestartServerResponse{
		Status: "restarting",
	}), nil
}

// Destroys and recreates a server container from scratch - brute force reset
func (s *ServerService) RecreateServer(ctx context.Context, req *connect.Request[v1.RecreateServerRequest]) (*connect.Response[v1.RecreateServerResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	go func() {
		bgCtx, cancel := context.WithTimeout(detach(ctx), 2*time.Hour)
		defer cancel()
		if err := s.lifecycle.Recreate(bgCtx, server.ID); err != nil {
			s.log.Error("Failed to recreate server %s: %v", server.Name, err)
		}
	}()

	return connect.NewResponse(&v1.RecreateServerResponse{
		Status: "recreated",
	}), nil
}

// SendCommand sends a command to a server
func (s *ServerService) SendCommand(ctx context.Context, req *connect.Request[v1.SendCommandRequest]) (*connect.Response[v1.SendCommandResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)

	silent := false
	if req.Msg.Silent != nil {
		silent = *req.Msg.Silent
	}

	if err != nil {
		s.log.Error("Failed to get server: %v", err)
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Check if server is running
	if server.ContainerID == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("server container not found"))
	}

	status, err := s.docker.GetContainerStatus(ctx, server.ContainerID)
	if err != nil || (status != storage.StatusRunning && status != storage.StatusUnhealthy) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("server is not running"))
	}

	if req.Msg.Command == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("command is required"))
	}

	// Add command to log stream if available
	commandTime := time.Now()
	if !silent && s.logStreamer != nil {
		s.logStreamer.AddCommandEntry(server.ID, req.Msg.Command, commandTime)
	}

	// Send command
	output, err := s.sender.SendCommand(ctx, server.ID, req.Msg.Command)
	success := err == nil
	if success {
		s.rec.Record(ctx, server.ID, "command.run", activity.Attrs{"command": req.Msg.Command}, "ran command %q", req.Msg.Command)
	}

	// Add command output to log stream if available
	if !silent && s.logStreamer != nil && (output != "" || !success) {
		s.logStreamer.AddCommandOutput(server.ID, output, success, commandTime)
	}

	if err != nil {
		s.log.Error("Failed to execute command: %v", err)
		return connect.NewResponse(&v1.SendCommandResponse{
			Success: false,
			Error:   err.Error(),
		}), nil
	}

	return connect.NewResponse(&v1.SendCommandResponse{
		Success: true,
		Output:  output,
	}), nil
}

// Reads the server's latest.log and uploads it to mclo.gs
func (s *ServerService) UploadToMCLogs(ctx context.Context, req *connect.Request[v1.UploadToMCLogsRequest]) (*connect.Response[v1.UploadToMCLogsResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	logPath := filepath.Join(server.DataPath, "logs", "latest.log")
	content, err := os.ReadFile(logPath)
	if err != nil {
		s.log.Error("Failed to read server log file: %v", err)
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("log file not found"))
	}

	if len(content) == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("log file is empty"))
	}

	// Truncate to 25000 lines if needed
	lines := bytes.Split(content, []byte("\n"))
	if len(lines) > 25000 {
		lines = lines[len(lines)-25000:]
		content = bytes.Join(lines, []byte("\n"))
	}

	// Build mclo.gs request
	payload, _ := json.Marshal(map[string]string{
		"content": string(content),
		"source":  fmt.Sprintf("DiscoPanel-%s", server.Name),
	})

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.mclo.gs/1/log", bytes.NewReader(payload))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create request"))
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		s.log.Error("Failed to upload to mclo.gs: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to upload to mclo.gs"))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to read mclo.gs response"))
	}

	var result struct {
		Success bool   `json:"success"`
		URL     string `json:"url"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to parse mclo.gs response"))
	}

	if !result.Success {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("mclo.gs error: %s", result.Error))
	}

	return connect.NewResponse(&v1.UploadToMCLogsResponse{
		Url: result.URL,
	}), nil
}

// GetServerLogs gets server logs
func (s *ServerService) GetServerLogs(ctx context.Context, req *connect.Request[v1.GetServerLogsRequest]) (*connect.Response[v1.GetServerLogsResponse], error) {
	// Parse tail parameter
	tail := int(req.Msg.Tail)
	if tail == 0 {
		tail = 100
	}

	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Get structured log entries from the log streamer if available
	var protoLogs []*v1.LogEntry
	if s.logStreamer != nil {
		// Attach a follow if the container is up but nothing is streaming
		// yet (e.g. panel restarted while the server was running).
		if server.ContainerID != "" {
			if err := s.logStreamer.StartStreaming(server.ID, server.ContainerID); err != nil {
				s.log.Warn("Failed to start log streaming for server %s: %v", server.ID, err)
			}
		}
		protoLogs = s.logStreamer.GetLogs(server.ID, tail)
	}

	return connect.NewResponse(&v1.GetServerLogsResponse{
		Logs:  protoLogs,
		Total: int32(len(protoLogs)),
	}), nil
}

// ClearServerLogs clears server logs
func (s *ServerService) ClearServerLogs(ctx context.Context, req *connect.Request[v1.ClearServerLogsRequest]) (*connect.Response[v1.ClearServerLogsResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Clear structured log entries if log streamer is available
	if s.logStreamer != nil {
		s.logStreamer.ClearLogs(server.ID)
	}

	return connect.NewResponse(&v1.ClearServerLogsResponse{}), nil
}

// GetNextAvailablePort gets the next available port
func (s *ServerService) GetNextAvailablePort(ctx context.Context, req *connect.Request[v1.GetNextAvailablePortRequest]) (*connect.Response[v1.GetNextAvailablePortResponse], error) {
	// Get all servers
	servers, err := s.store.ListServers(ctx)
	if err != nil {
		s.log.Error("Failed to list servers: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get available port"))
	}

	// Build a map of used ports (only for non-proxied servers)
	usedPortsMap := make(map[int32]bool)
	for _, server := range servers {
		// Only count ports for servers that don't use proxy
		if server.ProxyHostname == "" && server.Port > 0 {
			usedPortsMap[int32(server.Port)] = true
		}
	}

	// Mark proxy listening ports as used (only if proxy is enabled)
	if s.config.Proxy.Enabled {
		for _, port := range s.config.Proxy.ListenPorts {
			usedPortsMap[int32(port)] = true
		}
	}

	// Find the next available port starting from 25565
	var nextPort int32 = 25565
	for usedPortsMap[nextPort] {
		nextPort++
		// Safety check to avoid infinite loop
		if nextPort > 65535 {
			return nil, connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("no available ports"))
		}
	}

	// Convert map to proto UsedPort array
	usedPorts := make([]*v1.UsedPort, 0, len(usedPortsMap))
	for port, inUse := range usedPortsMap {
		usedPorts = append(usedPorts, &v1.UsedPort{
			Port:  port,
			InUse: inUse,
		})
	}

	return connect.NewResponse(&v1.GetNextAvailablePortResponse{
		Port:      nextPort,
		UsedPorts: usedPorts,
	}), nil
}

// Reports host physical memory and per-server reservations
func (s *ServerService) GetHostMemory(ctx context.Context, req *connect.Request[v1.GetHostMemoryRequest]) (*connect.Response[v1.GetHostMemoryResponse], error) {
	var totalMB int64
	if s.docker != nil {
		if dockerClient := s.docker.GetDockerClient(); dockerClient != nil {
			if info, err := dockerClient.Info(ctx); err == nil {
				totalMB = info.MemTotal / 1024 / 1024
			} else {
				s.log.Error("Failed to read docker host info: %v", err)
			}
		}
	}

	servers, err := s.store.ListServers(ctx)
	if err != nil {
		s.log.Error("Failed to list servers: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get host memory"))
	}

	allocations := make([]*v1.ServerMemoryAllocation, 0, len(servers))
	for _, server := range servers {
		allocations = append(allocations, &v1.ServerMemoryAllocation{
			ServerId:   server.ID,
			ServerName: server.Name,
			Memory:     int32(server.Memory),
		})
	}

	return connect.NewResponse(&v1.GetHostMemoryResponse{
		TotalMb:     totalMB,
		Allocations: allocations,
	}), nil
}

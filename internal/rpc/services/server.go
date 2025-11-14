package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/minecraft"
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
	store        *storage.Store
	docker       *docker.Client
	config       *config.Config
	proxyManager *proxy.Manager
	log          *logger.Logger
	logStreamer  *docker.LogStreamer
}

// NewServerService creates a new server service
func NewServerService(store *storage.Store, dockerClient *docker.Client, cfg *config.Config, proxyManager *proxy.Manager, log *logger.Logger) *ServerService {
	// Initialize log streamer
	logStreamer := docker.NewLogStreamer(dockerClient.GetDockerClient(), log, 10000)

	return &ServerService{
		store:        store,
		docker:       dockerClient,
		config:       cfg,
		proxyManager: proxyManager,
		log:          log,
		logStreamer:  logStreamer,
	}
}

// Helper functions

// dbServerToProto converts a database Server to a proto Server
func dbServerToProto(server *storage.Server) *v1.Server {
	if server == nil {
		return nil
	}

	protoServer := &v1.Server{
		Id:              server.ID,
		Name:            server.Name,
		Description:     server.Description,
		ModLoader:       modLoaderToProto(server.ModLoader),
		McVersion:       server.MCVersion,
		Status:          serverStatusToProto(server.Status),
		Port:            int32(server.Port),
		ProxyHostname:   server.ProxyHostname,
		ProxyListenerId: server.ProxyListenerID,
		ProxyPort:       int32(server.ProxyPort),
		MaxPlayers:      int32(server.MaxPlayers),
		Memory:          int32(server.Memory),
		DataPath:        server.DataPath,
		ContainerId:     server.ContainerID,
		JavaVersion:     int32(0), // Convert string to int if needed
		DockerImage:     server.DockerImage,
		AutoStart:       server.AutoStart,
		Detached:        server.Detached,
		TpsCommand:      server.TPSCommand,
		MemoryUsage:     int64(server.MemoryUsage),
		CpuPercent:      server.CPUPercent,
		DiskUsage:       server.DiskUsage,
		DiskTotal:       server.DiskTotal,
		PlayersOnline:   int32(server.PlayersOnline),
		Tps:             server.TPS,
		AdditionalPorts: server.AdditionalPorts,
		DockerOverrides: server.DockerOverrides,
		CreatedAt:       timestamppb.New(server.CreatedAt),
		UpdatedAt:       timestamppb.New(server.UpdatedAt),
	}

	// Handle optional last_started
	if server.LastStarted != nil {
		protoServer.LastStarted = timestamppb.New(*server.LastStarted)
	}

	return protoServer
}

// serverStatusToProto converts a DB ServerStatus to proto ServerStatus
func serverStatusToProto(status storage.ServerStatus) v1.ServerStatus {
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
	default:
		return v1.ServerStatus_SERVER_STATUS_UNSPECIFIED
	}
}

// modLoaderToProto converts a DB ModLoader to proto ModLoader
func modLoaderToProto(modLoader storage.ModLoader) v1.ModLoader {
	switch modLoader {
	case storage.ModLoaderVanilla:
		return v1.ModLoader_MOD_LOADER_VANILLA
	case storage.ModLoaderForge:
		return v1.ModLoader_MOD_LOADER_FORGE
	case storage.ModLoaderFabric:
		return v1.ModLoader_MOD_LOADER_FABRIC
	case storage.ModLoaderQuilt:
		return v1.ModLoader_MOD_LOADER_QUILT
	case storage.ModLoaderPaper:
		return v1.ModLoader_MOD_LOADER_PAPER
	case storage.ModLoaderSpigot:
		return v1.ModLoader_MOD_LOADER_SPIGOT
	case storage.ModLoaderBukkit:
		return v1.ModLoader_MOD_LOADER_BUKKIT
	case storage.ModLoaderPurpur:
		return v1.ModLoader_MOD_LOADER_PURPUR
	case storage.ModLoaderSpongeVanilla:
		return v1.ModLoader_MOD_LOADER_SPONGE_VANILLA
	case storage.ModLoaderMohist:
		return v1.ModLoader_MOD_LOADER_MOHIST
	case storage.ModLoaderCatserver:
		return v1.ModLoader_MOD_LOADER_CATSERVER
	case storage.ModLoaderArclight:
		return v1.ModLoader_MOD_LOADER_ARCLIGHT
	case storage.ModLoaderAutoCurseForge:
		return v1.ModLoader_MOD_LOADER_AUTO_CURSEFORGE
	case storage.ModLoaderModrinth:
		return v1.ModLoader_MOD_LOADER_MODRINTH
	case storage.ModLoaderNeoForge:
		return v1.ModLoader_MOD_LOADER_NEOFORGE
	default:
		return v1.ModLoader_MOD_LOADER_UNSPECIFIED
	}
}

// protoToModLoader converts a proto ModLoader to DB ModLoader
func protoToModLoader(modLoader v1.ModLoader) storage.ModLoader {
	switch modLoader {
	case v1.ModLoader_MOD_LOADER_VANILLA:
		return storage.ModLoaderVanilla
	case v1.ModLoader_MOD_LOADER_FORGE:
		return storage.ModLoaderForge
	case v1.ModLoader_MOD_LOADER_FABRIC:
		return storage.ModLoaderFabric
	case v1.ModLoader_MOD_LOADER_QUILT:
		return storage.ModLoaderQuilt
	case v1.ModLoader_MOD_LOADER_PAPER:
		return storage.ModLoaderPaper
	case v1.ModLoader_MOD_LOADER_SPIGOT:
		return storage.ModLoaderSpigot
	case v1.ModLoader_MOD_LOADER_BUKKIT:
		return storage.ModLoaderBukkit
	case v1.ModLoader_MOD_LOADER_PURPUR:
		return storage.ModLoaderPurpur
	case v1.ModLoader_MOD_LOADER_SPONGE_VANILLA:
		return storage.ModLoaderSpongeVanilla
	case v1.ModLoader_MOD_LOADER_MOHIST:
		return storage.ModLoaderMohist
	case v1.ModLoader_MOD_LOADER_CATSERVER:
		return storage.ModLoaderCatserver
	case v1.ModLoader_MOD_LOADER_ARCLIGHT:
		return storage.ModLoaderArclight
	case v1.ModLoader_MOD_LOADER_AUTO_CURSEFORGE:
		return storage.ModLoaderAutoCurseForge
	case v1.ModLoader_MOD_LOADER_MODRINTH:
		return storage.ModLoaderModrinth
	case v1.ModLoader_MOD_LOADER_NEOFORGE:
		return storage.ModLoaderNeoForge
	default:
		return storage.ModLoaderVanilla
	}
}

// findMostRecentMinecraftVersion finds the most recent MC version from a list
func findMostRecentMinecraftVersion(versions []string) string {
	for i := len(versions) - 1; i >= 0; i-- {
		hasLetter := false
		for _, ch := range versions[i] {
			if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
				hasLetter = true
				break
			}
		}
		if !hasLetter {
			return versions[i]
		}
	}
	return versions[len(versions)-1] // Return last because obviously we don't have a choice now
}

// ListServers lists all servers
func (s *ServerService) ListServers(ctx context.Context, req *connect.Request[v1.ListServersRequest]) (*connect.Response[v1.ListServersResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetServer gets a specific server
func (s *ServerService) GetServer(ctx context.Context, req *connect.Request[v1.GetServerRequest]) (*connect.Response[v1.GetServerResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// CreateServer creates a new server
func (s *ServerService) CreateServer(ctx context.Context, req *connect.Request[v1.CreateServerRequest]) (*connect.Response[v1.CreateServerResponse], error) {
	reqMsg := req.Msg

	// If modpack is selected, load it and derive settings
	var modpackURL string
	modLoader := protoToModLoader(reqMsg.ModLoader)
	mcVersion := reqMsg.McVersion
	memory := int(reqMsg.Memory)

	if reqMsg.ModpackId != "" {
		modpack, err := s.store.GetIndexedModpack(ctx, reqMsg.ModpackId)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid modpack"))
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
		if mcVersion == "" {
			var gameVersions []string
			if err := json.Unmarshal([]byte(modpack.GameVersions), &gameVersions); err == nil && len(gameVersions) > 0 {
				mcVersion = findMostRecentMinecraftVersion(gameVersions)
			}
		}

		// Set minimum memory for modpacks
		if memory < 4096 {
			memory = 4096
		}
	}

	// Validate request
	if reqMsg.Name == "" || mcVersion == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name and MC version are required"))
	}

	port := int(reqMsg.Port)
	proxyHostname := reqMsg.ProxyHostname
	proxyListenerID := reqMsg.ProxyListenerId
	useBaseURL := reqMsg.UseBaseUrl

	// Handle proxy configuration
	if proxyHostname != "" {
		// If using base URL, append it to the hostname
		if useBaseURL {
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
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid or disabled proxy listener"))
			}
			// Set the proxy port to the listener's port
			port = listener.Port
		} else {
			// No listener specified, get the default one
			listeners, err := s.store.GetProxyListeners(ctx)
			if err != nil || len(listeners) == 0 {
				return nil, connect.NewError(connect.CodeInternal, errors.New("no proxy listeners configured"))
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
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("no enabled proxy listeners available"))
			}
			proxyListenerID = defaultListener.ID
			port = defaultListener.Port
		}
		// Skip port validation for proxy servers
	} else {
		// For non-proxy servers, must have a unique port
		if port == 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("port is required for non-proxy servers"))
		}

		// Check if port is already in use
		existing, err := s.store.GetServerByPort(ctx, port)
		if err != nil {
			s.log.Error("Failed to check port: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to check port availability"))
		}
		if existing != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("port already in use"))
		}

		// Also check if this port is used by the proxy
		if s.config.Proxy.Enabled {
			if slices.Contains(s.config.Proxy.ListenPorts, port) {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("port is already in use by the proxy server"))
			}
		}
	}

	// Determine Docker image based on MC version and mod loader if not specified
	dockerImage := reqMsg.DockerImage
	if dockerImage == "" {
		dockerImage = docker.GetOptimalDockerTag(mcVersion, modLoader, false)
	}

	// Validate additional ports
	usedPorts := make(map[string]bool)
	additionalPorts := reqMsg.AdditionalPorts
	for i, protoPort := range additionalPorts {
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
			additionalPorts[i].Protocol = "tcp"
			protocol = "tcp"
		} else if protocol != "tcp" && protocol != "udp" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid protocol %s (must be tcp or udp)", protocol))
		}

		// Check for duplicate ports in the same request
		portKey := fmt.Sprintf("%d/%s", protoPort.HostPort, protocol)
		if usedPorts[portKey] {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("duplicate host port %d/%s", protoPort.HostPort, protocol))
		}
		usedPorts[portKey] = true

		// Check if port conflicts with main server port or proxy ports
		if int(protoPort.HostPort) == port {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("additional port %d conflicts with main server port", protoPort.HostPort))
		}
		if s.config.Proxy.Enabled && slices.Contains(s.config.Proxy.ListenPorts, int(protoPort.HostPort)) {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port %d is already in use by the proxy server", protoPort.HostPort))
		}
	}

	// Convert proto additional ports to docker.AdditionalPort for JSON serialization
	var dockerAdditionalPorts []docker.AdditionalPort
	for _, p := range additionalPorts {
		dockerAdditionalPorts = append(dockerAdditionalPorts, docker.AdditionalPort{
			ContainerPort: int(p.ContainerPort),
			HostPort:      int(p.HostPort),
			Protocol:      p.Protocol,
			Description:   p.Description,
		})
	}

	// Convert additional ports to JSON string for storage
	additionalPortsJSON := ""
	if len(dockerAdditionalPorts) > 0 {
		portsBytes, err := json.Marshal(dockerAdditionalPorts)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to process additional ports"))
		}
		additionalPortsJSON = string(portsBytes)
	}

	// Convert docker overrides to JSON string for storage
	dockerOverridesJSON := ""
	if reqMsg.DockerOverrides != nil {
		dockerOverride := &docker.DockerOverrides{
			Environment:       reqMsg.DockerOverrides.Environment,
			Volumes:           reqMsg.DockerOverrides.Volumes,
			Capabilities:      reqMsg.DockerOverrides.Capabilities,
			Devices:           reqMsg.DockerOverrides.Devices,
			NetworkMode:       reqMsg.DockerOverrides.NetworkMode,
			Privileged:        reqMsg.DockerOverrides.Privileged,
			User:              reqMsg.DockerOverrides.User,
			MemoryLimit:       reqMsg.DockerOverrides.MemoryLimit,
			MemoryReservation: reqMsg.DockerOverrides.MemoryReservation,
			CPULimit:          reqMsg.DockerOverrides.CpuLimit,
		}
		overridesBytes, err := json.Marshal(dockerOverride)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to process docker overrides"))
		}
		dockerOverridesJSON = string(overridesBytes)
	}

	// Create server object
	serverUUID := uuid.New().String()
	serverDataDir := fmt.Sprintf("%s_%s", files.SanitizePathName(reqMsg.Name), serverUUID)
	serverDataPath := filepath.Join(s.config.Storage.DataDir, "servers", serverDataDir)
	server := &storage.Server{
		ID:              serverUUID,
		Name:            reqMsg.Name,
		Description:     reqMsg.Description,
		ModLoader:       modLoader,
		MCVersion:       mcVersion,
		Status:          storage.StatusCreating,
		Port:            port,
		ProxyHostname:   proxyHostname,
		ProxyListenerID: proxyListenerID,
		MaxPlayers:      int(reqMsg.MaxPlayers),
		Memory:          memory,
		DataPath:        serverDataPath,
		JavaVersion:     docker.GetRequiredJavaVersion(mcVersion, modLoader),
		DockerImage:     dockerImage,
		AutoStart:       reqMsg.AutoStart,
		Detached:        reqMsg.Detached,
		TPSCommand:      minecraft.GetTPSCommand(modLoader),
		AdditionalPorts: additionalPortsJSON,
		DockerOverrides: dockerOverridesJSON,
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

	// When using proxy, set the ports correctly
	if server.ProxyHostname != "" && proxyListenerID != "" {
		// Get the listener to set the correct proxy port
		listener, err := s.store.GetProxyListener(ctx, proxyListenerID)
		if err == nil && listener != nil {
			server.ProxyPort = listener.Port
			server.Port = 25565 // Internal container port
		}
	}

	// Create data directory
	if err := os.MkdirAll(server.DataPath, 0755); err != nil {
		s.log.Error("Failed to create data directory: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create server directory"))
	}

	// Save to database
	if err := s.store.CreateServer(ctx, server); err != nil {
		s.log.Error("Failed to create server: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create server"))
	}

	// Get the server config that was created by CreateServer
	serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
	if err != nil {
		s.log.Error("Failed to get server config: %v", err)
		serverConfig = s.store.CreateDefaultServerConfig(server.ID)
	}

	if serverConfig.MaxMemory == nil && serverConfig.Memory == nil && serverConfig.InitMemory == nil {
		strMax := fmt.Sprintf("%dM", int(float64(server.Memory)*0.75))
		serverConfig.MaxMemory = &strMax
		strMin := fmt.Sprintf("%dM", int(float64(server.Memory)*0.45))
		serverConfig.InitMemory = &strMin
	}

	if serverConfig.Memory != nil {
		serverConfig.MaxMemory = serverConfig.Memory
		serverConfig.InitMemory = serverConfig.Memory
	}

	if err := s.store.UpdateServerConfig(ctx, serverConfig); err != nil {
		s.log.Error("Failed to update server config with modpack settings: %v", err)
	}

	// If modpack was selected, configure it
	if reqMsg.ModpackId != "" {
		modpack, _ := s.store.GetIndexedModpack(ctx, reqMsg.ModpackId)
		if modpack != nil && modpack.Indexer == "manual" {
			// For manual modpacks, copy the zip file to the server directory
			modpackFile, err := s.store.GetIndexedModpackFiles(ctx, reqMsg.ModpackId)
			if err == nil && len(modpackFile) > 0 {
				sourcePath := modpackFile[0].DownloadURL
				destPath := filepath.Join(server.DataPath, "modpack.zip")

				// Copy the modpack file
				if sourceFile, err := os.Open(sourcePath); err == nil {
					defer sourceFile.Close()
					if destFile, err := os.Create(destPath); err == nil {
						defer destFile.Close()
						io.Copy(destFile, sourceFile)

						// Set CF_MODPACK_ZIP for manual modpack installation
						cfModpackZip := "/data/modpack.zip"
						serverConfig.CFModpackZip = &cfModpackZip

						// Set a dummy slug for the manual modpack
						cfSlug := "manual-" + modpack.ID
						serverConfig.CFSlug = &cfSlug
					}
				}
			}
		} else if modpackURL != "" && server.ModLoader == storage.ModLoaderAutoCurseForge {
			// If version is pinned, append /files/<id> to the URL
			if reqMsg.ModpackVersionId != "" {
				versionedURL := fmt.Sprintf("%s/files/%s", modpackURL, reqMsg.ModpackVersionId)
				serverConfig.CFPageURL = &versionedURL
			} else {
				serverConfig.CFPageURL = &modpackURL
			}
		} else if modpack != nil && modpack.Indexer == "modrinth" {
			var projectSpec string
			if reqMsg.ModpackVersionId != "" && reqMsg.ModpackVersionId != "latest" {
				projectSpec = fmt.Sprintf("%s:%s", modpack.IndexerID, reqMsg.ModpackVersionId)
				s.log.Info("Using specific Modrinth version: %s", projectSpec)
			} else {
				projectSpec = modpack.IndexerID
				s.log.Info("Using latest Modrinth version for project: %s", projectSpec)
			}
			serverConfig.ModrinthModpack = &projectSpec
			downloadDeps := "required"
			serverConfig.ModrinthDownloadDependencies = &downloadDeps

			// Only set version type when using latest
			if reqMsg.ModpackVersionId == "" || reqMsg.ModpackVersionId == "latest" {
				versionType := "release"
				serverConfig.ModrinthModpackVersionType = &versionType
			}
		}

		// Ensure config is updated with proper settings
		if err := s.store.UpdateServerConfig(ctx, serverConfig); err != nil {
			s.log.Error("Failed to update server config with modpack settings: %v", err)
		}
	}

	// Create Docker container asynchronously to prevent request timeout
	go func() {
		bgCtx := context.Background()

		s.log.Info("Starting async Docker container creation for server %s", server.ID)

		containerID, err := s.docker.CreateContainer(bgCtx, server, serverConfig)
		if err != nil {
			s.log.Error("Failed to create container: %v", err)
			server.Status = storage.StatusError
			if updateErr := s.store.UpdateServer(bgCtx, server); updateErr != nil {
				s.log.Error("Failed to update server status to error: %v", updateErr)
			}
			return
		}

		server.ContainerID = containerID
		s.log.Info("Container created successfully for server %s: %s", server.ID, containerID)

		if err := s.store.UpdateServer(bgCtx, server); err != nil {
			s.log.Error("Failed to update server with container ID: %v", err)
			return
		}

		// Start the container immediately if requested
		if reqMsg.StartImmediately {
			if err := s.docker.StartContainer(bgCtx, containerID); err != nil {
				s.log.Error("Failed to start container: %v", err)
				server.Status = storage.StatusError
			} else {
				server.Status = storage.StatusStarting
				if err := s.logStreamer.StartStreaming(containerID); err != nil {
					s.log.Error("Failed to start log streaming: %v", err)
				}
				now := time.Now()
				server.LastStarted = &now
				if err := s.store.ClearEphemeralConfigFields(bgCtx, server.ID); err != nil {
					s.log.Error("Failed to clear ephemeral config fields: %v", err)
				}
			}
			if err := s.store.UpdateServer(bgCtx, server); err != nil {
				s.log.Error("Failed to update server status: %v", err)
			}
		} else {
			server.Status = storage.StatusStopped
			if err := s.store.UpdateServer(bgCtx, server); err != nil {
				s.log.Error("Failed to update server status: %v", err)
			}
			s.log.Info("Server %s created but not started immediately", server.ID)
		}
	}()

	// Return immediately with the server in "creating" state
	return connect.NewResponse(&v1.CreateServerResponse{
		Server: dbServerToProto(server),
	}), nil
}

// UpdateServer updates a server
func (s *ServerService) UpdateServer(ctx context.Context, req *connect.Request[v1.UpdateServerRequest]) (*connect.Response[v1.UpdateServerResponse], error) {
	reqMsg := req.Msg

	server, err := s.store.GetServer(ctx, reqMsg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Check if container recreation is needed
	needsRecreation := false
	originalMemory := server.Memory
	originalModLoader := server.ModLoader
	originalMCVersion := server.MCVersion
	originalDockerImage := server.DockerImage

	// Update fields
	if reqMsg.Name != "" {
		server.Name = reqMsg.Name
	}
	if reqMsg.Description != "" {
		server.Description = reqMsg.Description
	}
	if reqMsg.MaxPlayers > 0 {
		server.MaxPlayers = int(reqMsg.MaxPlayers)
		needsRecreation = true
	}
	if reqMsg.Memory > 0 && int(reqMsg.Memory) != originalMemory {
		server.Memory = int(reqMsg.Memory)
		needsRecreation = true
		if err := s.store.UpdateServerConfigMemory(ctx, server.ID, int(reqMsg.Memory)); err != nil {
			s.log.Error("Failed to update server config memory: %v", err)
		}
	}
	if reqMsg.ModLoader != "" && storage.ModLoader(reqMsg.ModLoader) != originalModLoader {
		server.ModLoader = storage.ModLoader(reqMsg.ModLoader)
		server.TPSCommand = minecraft.GetTPSCommand(server.ModLoader)
		needsRecreation = true
	}
	if reqMsg.McVersion != "" && reqMsg.McVersion != originalMCVersion {
		server.MCVersion = reqMsg.McVersion
		needsRecreation = true
	}
	if reqMsg.DockerImage != "" && reqMsg.DockerImage != originalDockerImage {
		server.DockerImage = reqMsg.DockerImage
		needsRecreation = true
	}
	if reqMsg.AutoStart != nil {
		server.AutoStart = *reqMsg.AutoStart
	}
	if reqMsg.Detached != nil {
		server.Detached = *reqMsg.Detached
	}
	if reqMsg.TpsCommand != nil {
		server.TPSCommand = *reqMsg.TpsCommand
	}

	// Handle additional ports update
	if reqMsg.AdditionalPorts != nil {
		// Validate additional ports
		usedPorts := make(map[string]bool)
		additionalPorts := reqMsg.AdditionalPorts
		for i, protoPort := range additionalPorts {
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
				additionalPorts[i].Protocol = "tcp"
				protocol = "tcp"
			} else if protocol != "tcp" && protocol != "udp" {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid protocol %s (must be tcp or udp)", protocol))
			}

			// Check for duplicate ports in the same request
			portKey := fmt.Sprintf("%d/%s", protoPort.HostPort, protocol)
			if usedPorts[portKey] {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("duplicate host port %d/%s", protoPort.HostPort, protocol))
			}
			usedPorts[portKey] = true

			// Check if port conflicts with main server port
			if int(protoPort.HostPort) == server.Port {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("additional port %d conflicts with main server port", protoPort.HostPort))
			}
			// Check if port conflicts with proxy ports
			if s.config.Proxy.Enabled && slices.Contains(s.config.Proxy.ListenPorts, int(protoPort.HostPort)) {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port %d is already in use by the proxy server", protoPort.HostPort))
			}
		}

		// Convert proto additional ports to docker.AdditionalPort
		var dockerAdditionalPorts []docker.AdditionalPort
		for _, p := range additionalPorts {
			dockerAdditionalPorts = append(dockerAdditionalPorts, docker.AdditionalPort{
				ContainerPort: int(p.ContainerPort),
				HostPort:      int(p.HostPort),
				Protocol:      p.Protocol,
				Description:   p.Description,
			})
		}

		// Convert additional ports to JSON string for storage
		portsBytes, err := json.Marshal(dockerAdditionalPorts)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to process additional ports"))
		}
		server.AdditionalPorts = string(portsBytes)
		needsRecreation = true
	}

	// Handle docker overrides update
	if reqMsg.DockerOverrides != nil {
		dockerOverride := &docker.DockerOverrides{
			Environment:       reqMsg.DockerOverrides.Environment,
			Volumes:           reqMsg.DockerOverrides.Volumes,
			Capabilities:      reqMsg.DockerOverrides.Capabilities,
			Devices:           reqMsg.DockerOverrides.Devices,
			NetworkMode:       reqMsg.DockerOverrides.NetworkMode,
			Privileged:        reqMsg.DockerOverrides.Privileged,
			User:              reqMsg.DockerOverrides.User,
			MemoryLimit:       reqMsg.DockerOverrides.MemoryLimit,
			MemoryReservation: reqMsg.DockerOverrides.MemoryReservation,
			CPULimit:          reqMsg.DockerOverrides.CpuLimit,
		}
		overridesBytes, err := json.Marshal(dockerOverride)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to process docker overrides"))
		}
		server.DockerOverrides = string(overridesBytes)
		needsRecreation = true
	}

	// Handle modpack version update
	if reqMsg.ModpackId != "" {
		// Get server config
		serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
		if err != nil {
			s.log.Error("Failed to get server config: %v", err)
			serverConfig = s.store.CreateDefaultServerConfig(server.ID)
		}

		// Get modpack info
		modpack, err := s.store.GetIndexedModpack(ctx, reqMsg.ModpackId)
		if err == nil {
			// Update modpack URL
			modpackURL := modpack.WebsiteURL

			switch modpack.Indexer {
			case "fuego", "manual":
				// Update mod loader for CurseForge modpacks
				server.ModLoader = storage.ModLoaderAutoCurseForge
				needsRecreation = true

				if reqMsg.ModpackVersionId != "" {
					// Version pinning - append file slug to URL
					versionedURL := fmt.Sprintf("%s/files/%s", modpackURL, reqMsg.ModpackVersionId)
					serverConfig.CFPageURL = &versionedURL
				} else {
					// No version pinning - use base URL only
					serverConfig.CFPageURL = &modpackURL
				}
			case "modrinth":
				server.ModLoader = storage.ModLoaderModrinth
				needsRecreation = true

				var projectSpec string
				if reqMsg.ModpackVersionId != "" && reqMsg.ModpackVersionId != "latest" {
					projectSpec = fmt.Sprintf("%s:%s", modpack.IndexerID, reqMsg.ModpackVersionId)
					s.log.Info("Updating server with specific Modrinth version: %s", projectSpec)
				} else {
					projectSpec = modpack.IndexerID
					s.log.Info("Updating server with latest Modrinth version for project: %s", projectSpec)
				}
				serverConfig.ModrinthModpack = &projectSpec

				downloadDeps := "required"
				serverConfig.ModrinthDownloadDependencies = &downloadDeps

				// Only set version type when using latest
				if reqMsg.ModpackVersionId == "" || reqMsg.ModpackVersionId == "latest" {
					versionType := "release"
					serverConfig.ModrinthModpackVersionType = &versionType
				}
			}

			// Update server config
			if err := s.store.UpdateServerConfig(ctx, serverConfig); err != nil {
				s.log.Error("Failed to update server config with modpack settings: %v", err)
			}
		}
	}

	// Save server updates first
	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to update server"))
	}

	// If container needs recreation and exists, recreate it
	if needsRecreation && server.ContainerID != "" {
		// Check if server was running
		wasRunning := false
		status, err := s.docker.GetContainerStatus(ctx, server.ContainerID)
		if err != nil {
			s.log.Debug("Container %s not found during update, will create new one: %v", server.ContainerID, err)
		} else if status == storage.StatusRunning || status == storage.StatusUnhealthy {
			wasRunning = true
			// Stop the container
			if err := s.docker.StopContainer(ctx, server.ContainerID); err != nil {
				s.log.Error("Failed to stop container for recreation: %v", err)
			}
		}

		// Remove old container
		if err := s.docker.RemoveContainer(ctx, server.ContainerID); err != nil {
			s.log.Debug("Could not remove old container (may not exist): %v", err)
		}

		// Get server config for container creation
		serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
		if err != nil {
			s.log.Error("Failed to get server config: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get server configuration"))
		}

		// Create new container with updated settings
		newContainerID, err := s.docker.CreateContainer(ctx, server, serverConfig)
		if err != nil {
			s.log.Error("Failed to create new container: %v", err)
			server.Status = storage.StatusError
			server.ContainerID = ""
		} else {
			server.ContainerID = newContainerID
			server.Status = storage.StatusStopped

			// Start container if it was running before
			if wasRunning {
				if err := s.docker.StartContainer(ctx, newContainerID); err != nil {
					s.log.Error("Failed to restart container after recreation: %v", err)
					server.Status = storage.StatusError
				} else {
					server.Status = storage.StatusRunning
				}
			}
		}

		// Update server with new container ID and status
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server after container recreation: %v", err)
		}
	}

	return connect.NewResponse(&v1.UpdateServerResponse{
		Server: dbServerToProto(server),
	}), nil
}

// DeleteServer deletes a server
func (s *ServerService) DeleteServer(ctx context.Context, req *connect.Request[v1.DeleteServerRequest]) (*connect.Response[v1.DeleteServerResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Stop and remove container
	if server.ContainerID != "" {
		if err := s.docker.StopContainer(ctx, server.ContainerID); err != nil {
			s.log.Error("Failed to stop container: %v", err)
		}
		if err := s.docker.RemoveContainer(ctx, server.ContainerID); err != nil {
			s.log.Error("Failed to remove container: %v", err)
		}
	}

	// Delete from database
	if err := s.store.DeleteServer(ctx, req.Msg.Id); err != nil {
		s.log.Error("Failed to delete server: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to delete server"))
	}

	// Delete data directory
	if err := os.RemoveAll(server.DataPath); err != nil {
		s.log.Error("Failed to delete server data: %v", err)
	}

	return connect.NewResponse(&v1.DeleteServerResponse{}), nil
}

// StartServer starts a server
func (s *ServerService) StartServer(ctx context.Context, req *connect.Request[v1.StartServerRequest]) (*connect.Response[v1.StartServerResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	if server.ContainerID == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("server container not created"))
	}

	// Start container
	if err := s.docker.StartContainer(ctx, server.ContainerID); err != nil {
		s.log.Error("Failed to start container: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to start server"))
	}

	// Start log streaming for this container
	if err := s.logStreamer.StartStreaming(server.ContainerID); err != nil {
		s.log.Error("Failed to start log streaming: %v", err)
	}

	// Update server status
	now := time.Now()
	server.Status = storage.StatusStarting
	server.LastStarted = &now

	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
	}

	// Update proxy route if enabled
	if s.proxyManager != nil && server.ProxyHostname != "" {
		if err := s.proxyManager.UpdateServerRoute(server); err != nil {
			s.log.Error("Failed to update proxy route: %v", err)
		}
	}

	// Clear ephemeral configuration fields after starting the server
	if err := s.store.ClearEphemeralConfigFields(ctx, server.ID); err != nil {
		s.log.Error("Failed to clear ephemeral config fields: %v", err)
	}

	return connect.NewResponse(&v1.StartServerResponse{
		Status: "starting",
	}), nil
}

// StopServer stops a server
func (s *ServerService) StopServer(ctx context.Context, req *connect.Request[v1.StopServerRequest]) (*connect.Response[v1.StopServerResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	if server.ContainerID == "" {
		// If there's no container, server is already stopped
		server.Status = storage.StatusStopped
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server status: %v", err)
		}
		return connect.NewResponse(&v1.StopServerResponse{
			Status: "stopped",
		}), nil
	}

	// Stop container
	if err := s.docker.StopContainer(ctx, server.ContainerID); err != nil {
		s.log.Error("Failed to stop container: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to stop server"))
	}

	// Stop log streaming for this container
	s.logStreamer.StopStreaming(server.ContainerID)

	// Update server status
	server.Status = storage.StatusStopping

	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
	}

	// Remove proxy route if enabled
	if s.proxyManager != nil && server.ProxyHostname != "" {
		if err := s.proxyManager.RemoveServerRoute(server.ID); err != nil {
			s.log.Error("Failed to remove proxy route: %v", err)
		}
	}

	return connect.NewResponse(&v1.StopServerResponse{
		Status: "stopping",
	}), nil
}

// RestartServer restarts a server
func (s *ServerService) RestartServer(ctx context.Context, req *connect.Request[v1.RestartServerRequest]) (*connect.Response[v1.RestartServerResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// If container doesn't exist, create it and start it
	if server.ContainerID == "" {
		// Get server config for container creation
		serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
		if err != nil {
			s.log.Error("Failed to get server config: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get server configuration"))
		}

		// Create container
		containerID, err := s.docker.CreateContainer(ctx, server, serverConfig)
		if err != nil {
			s.log.Error("Failed to create container: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create server container"))
		}

		server.ContainerID = containerID
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server with container ID: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to update server"))
		}

		// Now start the container
		if err := s.docker.StartContainer(ctx, server.ContainerID); err != nil {
			s.log.Error("Failed to start container: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to start server"))
		}

		// Start log streaming for this new container
		if err := s.logStreamer.StartStreaming(server.ContainerID); err != nil {
			s.log.Error("Failed to start log streaming: %v", err)
		}

		// Update server status
		now := time.Now()
		server.Status = storage.StatusStarting
		server.LastStarted = &now

		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server status: %v", err)
		}

		// Clear ephemeral configuration fields after starting the server
		if err := s.store.ClearEphemeralConfigFields(ctx, server.ID); err != nil {
			s.log.Error("Failed to clear ephemeral config fields: %v", err)
		}

		return connect.NewResponse(&v1.RestartServerResponse{
			Status: "starting",
		}), nil
	}

	// Stop log streaming before restart
	s.logStreamer.StopStreaming(server.ContainerID)

	// Stop container
	if err := s.docker.StopContainer(ctx, server.ContainerID); err != nil {
		s.log.Error("Failed to stop container: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to stop server"))
	}

	// Update server status to stopping
	server.Status = storage.StatusStopping
	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
	}

	// Wait a bit for clean shutdown
	time.Sleep(2 * time.Second)

	// Start container
	if err := s.docker.StartContainer(ctx, server.ContainerID); err != nil {
		s.log.Error("Failed to start container: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to restart server"))
	}

	// Restart log streaming
	if err := s.logStreamer.StartStreaming(server.ContainerID); err != nil {
		s.log.Error("Failed to restart log streaming: %v", err)
	}

	// Update server status
	now := time.Now()
	server.Status = storage.StatusStarting
	server.LastStarted = &now

	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
	}

	// Clear ephemeral configuration fields after starting the server
	if err := s.store.ClearEphemeralConfigFields(ctx, server.ID); err != nil {
		s.log.Error("Failed to clear ephemeral config fields: %v", err)
	}

	return connect.NewResponse(&v1.RestartServerResponse{
		Status: "restarting",
	}), nil
}

// SendCommand sends a command to a server
func (s *ServerService) SendCommand(ctx context.Context, req *connect.Request[v1.SendCommandRequest]) (*connect.Response[v1.SendCommandResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetServerLogs gets server logs
func (s *ServerService) GetServerLogs(ctx context.Context, req *connect.Request[v1.GetServerLogsRequest]) (*connect.Response[v1.GetServerLogsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// ClearServerLogs clears server logs
func (s *ServerService) ClearServerLogs(ctx context.Context, req *connect.Request[v1.ClearServerLogsRequest]) (*connect.Response[v1.ClearServerLogsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetNextAvailablePort gets the next available port
func (s *ServerService) GetNextAvailablePort(ctx context.Context, req *connect.Request[v1.GetNextAvailablePortRequest]) (*connect.Response[v1.GetNextAvailablePortResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

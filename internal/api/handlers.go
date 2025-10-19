package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	models "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/minecraft"
	"github.com/nickheyer/discopanel/pkg/files"
)

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	servers, err := s.store.ListServers(ctx)
	if err != nil {
		s.log.Error("Failed to list servers: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to list servers")
		return
	}

	// Check if full stats are requested (for dashboard)
	fullStats := r.URL.Query().Get("full_stats") == "true"

	// Get all proxy listeners once for efficiency
	var listeners map[string]*models.ProxyListener
	if s.config.Proxy.Enabled {
		allListeners, err := s.store.GetProxyListeners(ctx)
		if err == nil {
			listeners = make(map[string]*models.ProxyListener)
			for _, l := range allListeners {
				listeners[l.ID] = l
			}
		}
	}

	// Get total disk space available
	diskTotal, err := files.GetDiskSpace(s.config.Storage.DataDir)
	if err != nil {
		fmt.Printf("unable to get disk space available")
		diskTotal = 0
	}

	// Update status from Docker
	for _, server := range servers {
		// If server uses proxy, ensure ProxyPort is populated from the listener
		// ProxyPort is the external port players connect to
		if server.ProxyHostname != "" && server.ProxyListenerID != "" && listeners != nil {
			if listener, ok := listeners[server.ProxyListenerID]; ok {
				server.ProxyPort = listener.Port
			}
		}

		if server.ContainerID != "" {
			status, err := s.docker.GetContainerStatus(ctx, server.ContainerID)
			if err == nil {
				server.Status = status
			}

			// Only get expensive stats if requested (dashboard refresh)
			if fullStats && (status == models.StatusRunning || status == models.StatusUnhealthy) {
				// Get container stats
				stats, err := s.docker.GetContainerStats(ctx, server.ContainerID)
				if err == nil {
					server.MemoryUsage = stats.MemoryUsage
					server.CPUPercent = stats.CPUPercent
				}

				// Calculate disk usage for world directory
				worldPath, err := files.FindWorldDir(server.DataPath)
				if err == nil {
					totalSize, err := files.CalculateDirSize(worldPath)
					if err == nil {
						server.DiskUsage = totalSize // Store as bytes
					}
				}

				server.DiskTotal = diskTotal

				// Get player count using rcon-cli
				output, err := s.docker.ExecCommand(ctx, server.ContainerID, "list")
				if err == nil && output != "" {
					count, _ := minecraft.ParsePlayerListFromOutput(output)
					server.PlayersOnline = count
				}

				// Get TPS if configured
				if server.TPSCommand != "" {
					for _, cmd := range strings.Split(server.TPSCommand, " ?? ") {
						cmd = strings.TrimSpace(cmd)
						if cmd == "" {
							continue
						}
						output, err := s.docker.ExecCommand(ctx, server.ContainerID, cmd)
						if err == nil && output != "" {
							tps := minecraft.ParseTPSFromOutput(output)
							if tps > 0 {
								server.TPS = tps
								break
							}
						}
					}
				}
			}
		}
	}

	s.respondJSON(w, http.StatusOK, servers)
}

func (s *Server) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		Name             string           `json:"name"`
		Description      string           `json:"description"`
		ModLoader        models.ModLoader `json:"mod_loader"`
		MCVersion        string           `json:"mc_version"`
		Port             int              `json:"port"`
		MaxPlayers       int              `json:"max_players"`
		Memory           int              `json:"memory"`
		DockerImage      string           `json:"docker_image"`
		AutoStart        bool             `json:"auto_start"`
		Detached         bool             `json:"detached"`
		StartImmediately bool             `json:"start_immediately"`
		ModpackID        string           `json:"modpack_id,omitempty"`
		ProxyHostname    string           `json:"proxy_hostname,omitempty"`
		ProxyListenerID  string           `json:"proxy_listener_id,omitempty"`
		UseBaseURL       bool             `json:"use_base_url,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// If modpack is selected, load it and derive settings
	var modpackURL string
	if req.ModpackID != "" {
		modpack, err := s.store.GetIndexedModpack(ctx, req.ModpackID)
		if err != nil {
			s.respondError(w, http.StatusBadRequest, "Invalid modpack")
			return
		}

		// Set the modpack URL
		modpackURL = modpack.WebsiteURL

		// Override mod loader based on indexer
		if modpack.Indexer == "fuego" || modpack.Indexer == "manual" {
			req.ModLoader = models.ModLoaderAutoCurseForge
		}

		// Get MC version from modpack if not explicitly set
		if req.MCVersion == "" {
			var gameVersions []string
			if err := json.Unmarshal([]byte(modpack.GameVersions), &gameVersions); err == nil && len(gameVersions) > 0 {
				req.MCVersion = gameVersions[0]
			}
		}

		// Set minimum memory for modpacks
		if req.Memory < 4096 {
			req.Memory = 4096
		}
	}

	// Validate request
	if req.Name == "" || req.MCVersion == "" {
		s.respondError(w, http.StatusBadRequest, "Name and MC version are required")
		return
	}

	// Handle proxy configuration
	if req.ProxyHostname != "" {
		// If using base URL, append it to the hostname
		if req.UseBaseURL {
			proxyConfig, _, err := s.store.GetProxyConfig(ctx)
			if err == nil && proxyConfig.BaseURL != "" {
				// Only append base URL if hostname doesn't already contain a domain
				if !strings.Contains(req.ProxyHostname, ".") {
					req.ProxyHostname = req.ProxyHostname + "." + proxyConfig.BaseURL
				}
			}
		}

		// Validate listener selection
		if req.ProxyListenerID != "" {
			listener, err := s.store.GetProxyListener(ctx, req.ProxyListenerID)
			if err != nil || !listener.Enabled {
				s.respondError(w, http.StatusBadRequest, "Invalid or disabled proxy listener")
				return
			}
			// Set the proxy port to the listener's port
			req.Port = listener.Port
		} else {
			// No listener specified, get the default one
			listeners, err := s.store.GetProxyListeners(ctx)
			if err != nil || len(listeners) == 0 {
				s.respondError(w, http.StatusInternalServerError, "No proxy listeners configured")
				return
			}
			// Find default or first enabled listener
			var defaultListener *models.ProxyListener
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
				s.respondError(w, http.StatusBadRequest, "No enabled proxy listeners available")
				return
			}
			req.ProxyListenerID = defaultListener.ID
			req.Port = defaultListener.Port
		}
		// Skip port validation for proxy servers
	} else {
		// For non-proxy servers, must have a unique port
		if req.Port == 0 {
			s.respondError(w, http.StatusBadRequest, "Port is required for non-proxy servers")
			return
		}

		// Check if port is already in use (only checks non-proxy servers due to our updated GetServerByPort)
		existing, err := s.store.GetServerByPort(ctx, req.Port)
		if err != nil {
			s.log.Error("Failed to check port: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Failed to check port availability")
			return
		}
		if existing != nil {
			s.respondError(w, http.StatusBadRequest, "Port already in use")
			return
		}

		// Also check if this port is used by the proxy
		if s.config.Proxy.Enabled {
			if slices.Contains(s.config.Proxy.ListenPorts, req.Port) {
				s.respondError(w, http.StatusBadRequest, "Port is already in use by the proxy server")
				return
			}
		}
	}

	// Determine Docker image based on MC version and mod loader if not specified
	if req.DockerImage == "" {
		req.DockerImage = docker.GetOptimalDockerTag(req.MCVersion, req.ModLoader, false)
	}

	// Create server object
	serverUUID := uuid.New().String()
	serverDataDir := fmt.Sprintf("%s_%s", files.SanitizePathName(req.Name), serverUUID)
	serverDataPath := filepath.Join(s.config.Storage.DataDir, "servers", serverDataDir)
	server := &models.Server{
		ID:              serverUUID,
		Name:            req.Name,
		Description:     req.Description,
		ModLoader:       req.ModLoader,
		MCVersion:       req.MCVersion,
		Status:          models.StatusCreating, // Set initial status to creating
		Port:            req.Port,
		ProxyHostname:   req.ProxyHostname,
		ProxyListenerID: req.ProxyListenerID,
		MaxPlayers:      req.MaxPlayers,
		Memory:          req.Memory,
		DataPath:        serverDataPath,
		JavaVersion:     docker.GetRequiredJavaVersion(req.MCVersion, req.ModLoader),
		DockerImage:     req.DockerImage,
		AutoStart:       req.AutoStart,
		Detached:        req.Detached,
		TPSCommand:      minecraft.GetTPSCommand(req.ModLoader),
	}

	// Set defaults
	if server.MaxPlayers == 0 {
		server.MaxPlayers = 20
	}
	if server.Memory == 0 {
		server.Memory = 4096
	}
	if server.ModLoader == "" {
		server.ModLoader = models.ModLoaderVanilla
	}

	// When using proxy, set the ports correctly:
	// - ProxyPort: the external port that players connect to (from the listener)
	// - Port: should be 25565 for container internal port when using proxy
	if server.ProxyHostname != "" && req.ProxyListenerID != "" {
		// Get the listener to set the correct proxy port
		listener, err := s.store.GetProxyListener(ctx, req.ProxyListenerID)
		if err == nil && listener != nil {
			server.ProxyPort = listener.Port // External port for players to connect
			server.Port = 25565              // Internal container port (always 25565 for proxied servers)
		}
	}

	// Create data directory
	if err := os.MkdirAll(server.DataPath, 0755); err != nil {
		s.log.Error("Failed to create data directory: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to create server directory")
		return
	}

	// Save to database
	if err := s.store.CreateServer(ctx, server); err != nil {
		s.log.Error("Failed to create server: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to create server")
		return
	}

	// Get the server config that was created by CreateServer
	serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
	if err != nil {
		s.log.Error("Failed to get server config: %v", err)
		serverConfig = s.store.CreateDefaultServerConfig(server.ID)
	}

	if serverConfig.MaxMemory == nil && serverConfig.Memory == nil && serverConfig.InitMemory == nil {
		strMax := fmt.Sprintf("%dM", int(float64(server.Memory)*0.75)) // DOCS WANT 25 PERCENT HEADROOM
		serverConfig.MaxMemory = &strMax
		strMin := fmt.Sprintf("%dM", int(float64(server.Memory)*0.45)) // ARBITRARY MIN, IDK 30 OFFSET SOUNDS RIGHT
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
	if req.ModpackID != "" {
		modpack, _ := s.store.GetIndexedModpack(ctx, req.ModpackID)
		if modpack != nil && modpack.Indexer == "manual" {
			// For manual modpacks, copy the zip file to the server directory
			modpackFile, err := s.store.GetIndexedModpackFiles(ctx, req.ModpackID)
			if err == nil && len(modpackFile) > 0 {
				sourcePath := modpackFile[0].DownloadURL // This contains the local path
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
		} else if modpackURL != "" && server.ModLoader == models.ModLoaderAutoCurseForge {
			serverConfig.CFPageURL = &modpackURL
		}

		// Ensure config is updated with proper settings
		if err := s.store.UpdateServerConfig(ctx, serverConfig); err != nil {
			s.log.Error("Failed to update server config with modpack settings: %v", err)
		}
	}

	// Create Docker container asynchronously to prevent request timeout
	// The container creation includes image pulling which can take minutes
	go func() {
		// Create a new context for the background operation
		bgCtx := context.Background()

		s.log.Info("Starting async Docker container creation for server %s", server.ID)

		containerID, err := s.docker.CreateContainer(bgCtx, server, serverConfig)
		if err != nil {
			s.log.Error("Failed to create container: %v", err)
			// Update server status to error
			server.Status = models.StatusError
			if updateErr := s.store.UpdateServer(bgCtx, server); updateErr != nil {
				s.log.Error("Failed to update server status to error: %v", updateErr)
			}
			return
		}

		server.ContainerID = containerID
		s.log.Info("Container created successfully for server %s: %s", server.ID, containerID)

		// Update server with container ID
		if err := s.store.UpdateServer(bgCtx, server); err != nil {
			s.log.Error("Failed to update server with container ID: %v", err)
			return
		}

		// Start the container immediately if requested
		if req.StartImmediately {
			if err := s.docker.StartContainer(bgCtx, containerID); err != nil {
				s.log.Error("Failed to start container: %v", err)
				server.Status = models.StatusError
			} else {
				server.Status = models.StatusStarting
				// Update last started time
				now := time.Now()
				server.LastStarted = &now
				// Clear ephemeral configuration fields after starting the server
				if err := s.store.ClearEphemeralConfigFields(bgCtx, server.ID); err != nil {
					s.log.Error("Failed to clear ephemeral config fields: %v", err)
				}
			}
			// Update status in database
			if err := s.store.UpdateServer(bgCtx, server); err != nil {
				s.log.Error("Failed to update server status: %v", err)
			}
		} else {
			// Update status to stopped once container is ready
			server.Status = models.StatusStopped
			if err := s.store.UpdateServer(bgCtx, server); err != nil {
				s.log.Error("Failed to update server status: %v", err)
			}
			s.log.Info("Server %s created but not started immediately", server.ID)
		}
	}()

	// Return immediately with the server in "creating" state
	// The client can poll the server status to check when it's ready
	s.respondJSON(w, http.StatusCreated, server)
}

func (s *Server) handleGetServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	id := vars["id"]

	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// If server uses proxy, ensure ProxyPort is populated from the listener
	// ProxyPort is the external port players connect to
	if server.ProxyHostname != "" && server.ProxyListenerID != "" {
		listener, err := s.store.GetProxyListener(ctx, server.ProxyListenerID)
		if err == nil && listener != nil {
			server.ProxyPort = listener.Port
		}
	}

	// Update status and stats from Docker
	if server.ContainerID != "" {
		status, err := s.docker.GetContainerStatus(ctx, server.ContainerID)
		if err == nil {
			server.Status = status

			// Only get stats if server is running or unhealthy
			if status == models.StatusRunning || status == models.StatusUnhealthy {
				stats, err := s.docker.GetContainerStats(ctx, server.ContainerID)
				if err == nil {
					server.MemoryUsage = stats.MemoryUsage
					server.CPUPercent = stats.CPUPercent
				} else {
					s.log.Debug("Failed to get container stats for %s: %v", server.ContainerID, err)
				}
			}

			if status == models.StatusRunning {

				// Calculate world directory size
				worldPath, err := files.FindWorldDir(server.DataPath)
				if err != nil {
					s.log.Error("Error unable to find world directory %v", err)
				} else {
					totalSize, err := files.CalculateDirSize(worldPath)
					if err != nil {
						s.log.Error("Error calculating world directory size for %s: %v", worldPath, err)
					} else {
						server.DiskUsage = totalSize // Store as bytes
					}
				}

				// Get total disk space
				diskTotal, err := files.GetDiskSpace(server.DataPath)
				if err != nil {
					s.log.Debug("Failed to get disk space for %s: %v", server.DataPath, err)
				} else {
					server.DiskTotal = diskTotal
				}

				// Get Minecraft server status (player count) using rcon-cli
				output, err := s.docker.ExecCommand(ctx, server.ContainerID, "list")
				if err == nil && output != "" {
					count, _ := minecraft.ParsePlayerListFromOutput(output)
					server.PlayersOnline = count
				}

				// Get TPS if server has a TPS command configured, fallback commands separated by " ?? "
				if server.TPSCommand != "" {
					for cmd := range strings.SplitSeq(server.TPSCommand, " ?? ") {
						cmd = strings.TrimSpace(cmd)
						if cmd == "" {
							continue
						}

						output, err := s.docker.ExecCommand(ctx, server.ContainerID, cmd)
						if err == nil && output != "" {
							tps := minecraft.ParseTPSFromOutput(output)
							if tps > 0 {
								server.TPS = tps
								break // Stop on first successful command
							}
						}
					}
				}
			}
		} else {
			s.log.Debug("Failed to get container status for %s: %v", server.ContainerID, err)
		}
	}

	s.respondJSON(w, http.StatusOK, server)
}

func (s *Server) handleUpdateServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	id := vars["id"]

	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		MaxPlayers  int    `json:"max_players"`
		Memory      int    `json:"memory"`
		ModLoader   string `json:"mod_loader"`
		MCVersion   string `json:"mc_version"`
		DockerImage string `json:"docker_image"`
		AutoStart   *bool  `json:"auto_start"`
		Detached    *bool  `json:"detached"`
		TPSCommand  string `json:"tps_command"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Check if container recreation is needed
	needsRecreation := false
	originalMemory := server.Memory
	originalModLoader := server.ModLoader
	originalMCVersion := server.MCVersion
	originalDockerImage := server.DockerImage

	// Update fields
	if req.Name != "" {
		server.Name = req.Name
	}
	if req.Description != "" {
		server.Description = req.Description
	}
	if req.MaxPlayers > 0 {
		server.MaxPlayers = req.MaxPlayers
		needsRecreation = true
	}
	if req.Memory > 0 && req.Memory != originalMemory {
		server.Memory = req.Memory
		needsRecreation = true
		if err := s.store.UpdateServerConfigMemory(ctx, server.ID, req.Memory); err != nil {
			s.log.Error("Failed to update server config memory: %v", err)
		}
	}
	if req.ModLoader != "" && models.ModLoader(req.ModLoader) != originalModLoader {
		server.ModLoader = models.ModLoader(req.ModLoader)
		server.TPSCommand = minecraft.GetTPSCommand(server.ModLoader)
		needsRecreation = true
	}
	if req.MCVersion != "" && req.MCVersion != originalMCVersion {
		server.MCVersion = req.MCVersion
		needsRecreation = true
	}
	if req.DockerImage != "" && req.DockerImage != originalDockerImage {
		server.DockerImage = req.DockerImage
		needsRecreation = true
	}
	if req.AutoStart != nil {
		server.AutoStart = *req.AutoStart
	}
	if req.Detached != nil {
		server.Detached = *req.Detached
	}
	if req.TPSCommand != "" {
		server.TPSCommand = req.TPSCommand
	}

	// Save server updates first
	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to update server")
		return
	}

	// If container needs recreation and exists, recreate it
	if needsRecreation && server.ContainerID != "" {
		// Check if server was running
		wasRunning := false
		status, err := s.docker.GetContainerStatus(ctx, server.ContainerID)
		if err == nil && (status == models.StatusRunning || status == models.StatusUnhealthy) {
			wasRunning = true
			// Stop the container
			if err := s.docker.StopContainer(ctx, server.ContainerID); err != nil {
				s.log.Error("Failed to stop container for recreation: %v", err)
			}
		}

		// Remove old container
		if err := s.docker.RemoveContainer(ctx, server.ContainerID); err != nil {
			s.log.Error("Failed to remove old container: %v", err)
		}

		// Get server config for container creation
		serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
		if err != nil {
			s.log.Error("Failed to get server config: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Failed to get server configuration")
			return
		}

		// Create new container with updated settings
		newContainerID, err := s.docker.CreateContainer(ctx, server, serverConfig)
		if err != nil {
			s.log.Error("Failed to create new container: %v", err)
			server.Status = models.StatusError
			server.ContainerID = ""
		} else {
			server.ContainerID = newContainerID
			server.Status = models.StatusStopped

			// Start container if it was running before
			if wasRunning {
				if err := s.docker.StartContainer(ctx, newContainerID); err != nil {
					s.log.Error("Failed to restart container after recreation: %v", err)
					server.Status = models.StatusError
				} else {
					server.Status = models.StatusRunning
				}
			}
		}

		// Update server with new container ID and status
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server after container recreation: %v", err)
		}
	}

	s.respondJSON(w, http.StatusOK, server)
}

func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	id := vars["id"]

	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
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
	if err := s.store.DeleteServer(ctx, id); err != nil {
		s.log.Error("Failed to delete server: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to delete server")
		return
	}

	// Delete data directory
	if err := os.RemoveAll(server.DataPath); err != nil {
		s.log.Error("Failed to delete server data: %v", err)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStartServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	id := vars["id"]

	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	if server.ContainerID == "" {
		s.respondError(w, http.StatusBadRequest, "Server container not created")
		return
	}

	// Start container
	if err := s.docker.StartContainer(ctx, server.ContainerID); err != nil {
		s.log.Error("Failed to start container: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to start server")
		return
	}

	// Start log streaming for this container
	if err := s.logStreamer.StartStreaming(server.ContainerID); err != nil {
		s.log.Error("Failed to start log streaming: %v", err)
	}

	// Update server status
	now := time.Now()
	server.Status = models.StatusStarting
	server.LastStarted = &now

	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
	}

	// Update proxy route if enabled
	if s.proxyManager != nil && server.ProxyHostname != "" {
		if err := s.proxyManager.UpdateServerRoute(server); err != nil {
			s.log.Error("Failed to update proxy route: %v", err)
			// Not critical, continue
		}
	}

	// Clear ephemeral configuration fields after starting the server
	if err := s.store.ClearEphemeralConfigFields(ctx, server.ID); err != nil {
		s.log.Error("Failed to clear ephemeral config fields: %v", err)
	}

	s.respondJSON(w, http.StatusOK, map[string]string{"status": "starting"})
}

func (s *Server) handleStopServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	id := vars["id"]

	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	if server.ContainerID == "" {
		// If there's no container, server is already stopped
		server.Status = models.StatusStopped
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server status: %v", err)
		}
		s.respondJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
		return
	}

	// Stop container
	if err := s.docker.StopContainer(ctx, server.ContainerID); err != nil {
		s.log.Error("Failed to stop container: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to stop server")
		return
	}

	// Stop log streaming for this container
	s.logStreamer.StopStreaming(server.ContainerID)

	// Update server status
	server.Status = models.StatusStopping

	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
	}

	// Remove proxy route if enabled
	if s.proxyManager != nil && server.ProxyHostname != "" {
		if err := s.proxyManager.RemoveServerRoute(server.ID); err != nil {
			s.log.Error("Failed to remove proxy route: %v", err)
			// Not critical, continue
		}
	}

	s.respondJSON(w, http.StatusOK, map[string]string{"status": "stopping"})
}

func (s *Server) handleRestartServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	id := vars["id"]

	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// If container doesn't exist, create it and start it
	if server.ContainerID == "" {
		// Get server config for container creation
		serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
		if err != nil {
			s.log.Error("Failed to get server config: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Failed to get server configuration")
			return
		}

		// Create container
		containerID, err := s.docker.CreateContainer(ctx, server, serverConfig)
		if err != nil {
			s.log.Error("Failed to create container: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Failed to create server container")
			return
		}

		server.ContainerID = containerID
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server with container ID: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Failed to update server")
			return
		}

		// Now start the container
		if err := s.docker.StartContainer(ctx, server.ContainerID); err != nil {
			s.log.Error("Failed to start container: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Failed to start server")
			return
		}

		// Start log streaming for this new container
		if err := s.logStreamer.StartStreaming(server.ContainerID); err != nil {
			s.log.Error("Failed to start log streaming: %v", err)
		}

		// Update server status
		now := time.Now()
		server.Status = models.StatusStarting
		server.LastStarted = &now

		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server status: %v", err)
		}

		// Clear ephemeral configuration fields after starting the server
		if err := s.store.ClearEphemeralConfigFields(ctx, server.ID); err != nil {
			s.log.Error("Failed to clear ephemeral config fields: %v", err)
		}

		s.respondJSON(w, http.StatusOK, map[string]string{"status": "starting"})
		return
	}

	// Stop log streaming before restart
	s.logStreamer.StopStreaming(server.ContainerID)

	// Stop container
	if err := s.docker.StopContainer(ctx, server.ContainerID); err != nil {
		s.log.Error("Failed to stop container: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to stop server")
		return
	}

	// Update server status to stopping
	server.Status = models.StatusStopping
	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
	}

	// Wait a bit for clean shutdown
	time.Sleep(2 * time.Second)

	// Start container
	if err := s.docker.StartContainer(ctx, server.ContainerID); err != nil {
		s.log.Error("Failed to start container: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to restart server")
		return
	}

	// Restart log streaming
	if err := s.logStreamer.StartStreaming(server.ContainerID); err != nil {
		s.log.Error("Failed to restart log streaming: %v", err)
	}

	// Update server status
	now := time.Now()
	server.Status = models.StatusStarting
	server.LastStarted = &now

	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
	}

	// Clear ephemeral configuration fields after starting the server
	if err := s.store.ClearEphemeralConfigFields(ctx, server.ID); err != nil {
		s.log.Error("Failed to clear ephemeral config fields: %v", err)
	}

	s.respondJSON(w, http.StatusOK, map[string]string{"status": "restarting"})
}

func (s *Server) handleGetServerLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	id := vars["id"]

	// Parse tail parameter
	tail := 100
	if t := r.URL.Query().Get("tail"); t != "" {
		if parsed, err := strconv.Atoi(t); err == nil {
			tail = parsed
		}
	}

	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	if server.ContainerID == "" {
		s.respondError(w, http.StatusBadRequest, "Server container not created")
		return
	}

	// Get logs + cmd history
	logs := s.logStreamer.GetFormattedLogs(server.ContainerID, tail)

	// If no logs in streamer, fall back to docker logs
	if logs == "" {
		logs, err = s.docker.GetContainerLogs(ctx, server.ContainerID, tail)
		if err != nil {
			s.log.Error("Failed to get container logs: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Failed to get server logs")
			return
		}
	}

	s.respondJSON(w, http.StatusOK, map[string]string{"logs": logs})
}

func (s *Server) handleGetNextAvailablePort(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get all servers
	servers, err := s.store.ListServers(ctx)
	if err != nil {
		s.log.Error("Failed to list servers: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to get available port")
		return
	}

	// Build a map of used ports (only for non-proxied servers)
	usedPorts := make(map[int]bool)
	for _, server := range servers {
		// Only count ports for servers that don't use proxy
		if server.ProxyHostname == "" && server.Port > 0 {
			usedPorts[server.Port] = true
		}
	}

	// Mark proxy listening ports as used
	for _, port := range s.config.Proxy.ListenPorts {
		usedPorts[port] = true
	}

	// Find the next available port starting from 25565
	nextPort := 25565
	for usedPorts[nextPort] {
		nextPort++
		// Safety check to avoid infinite loop
		if nextPort > 65535 {
			s.respondError(w, http.StatusInternalServerError, "No available ports")
			return
		}
	}

	s.respondJSON(w, http.StatusOK, map[string]any{
		"port":      nextPort,
		"usedPorts": usedPorts,
	})
}

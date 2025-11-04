package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nickheyer/discopanel/internal/api"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/proxy"
	"github.com/nickheyer/discopanel/pkg/containers"
	"github.com/nickheyer/discopanel/pkg/logger"
)

func main() {
	var configPath = flag.String("config", "./config.yaml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Init logger
	logConfig := &logger.Config{
		Enabled:    cfg.Logging.Enabled,
		FilePath:   cfg.Logging.FilePath,
		MaxSize:    cfg.Logging.MaxSize,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAge:     cfg.Logging.MaxAge,
		Compress:   cfg.Logging.Compress,
	}
	log := logger.NewWithConfig(logConfig)
	defer log.Close()

	// Create required directories
	dirs := []string{
		cfg.Storage.DataDir,
		cfg.Storage.BackupDir,
		cfg.Storage.TempDir,
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatal("Failed to create directory %s: %v", dir, err)
		}
	}

	// Initialize storage with connection pooling
	store, err := storage.NewSQLiteStore(cfg.Database.Path, storage.DBConfig{
		MaxOpenConns:    cfg.Database.MaxConnections,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: time.Duration(cfg.Database.ConnMaxLifetime) * time.Second,
	})
	if err != nil {
		log.Fatal("Failed to initialize storage: %v", err)
	}
	defer store.Close()

	// Initialize global settings with config defaults if they don't exist
	ctx := context.Background()
	_, isNew, err := store.GetGlobalSettings(ctx)
	if err != nil {
		log.Fatal("Failed to get global settings: %v", err)
	}

	// Check if global settings are empty (just created) and populate with config defaults
	if isNew || cfg.Minecraft.ResetGlobal {
		// Copy the config defaults to global settings
		globalConfig := config.LoadGlobalServerConfig(cfg)
		globalConfig.ID = storage.GlobalSettingsID
		globalConfig.ServerID = storage.GlobalSettingsID

		if err := store.UpdateGlobalSettings(ctx, &globalConfig); err != nil {
			log.Fatal("Failed to initialize global settings: %v", err)
		}
		log.Info("Initialized global settings from config file")
	}

	// Initialize container provider
	containerProvider, err := containers.NewProvider(ctx, cfg.Containers.Provider)
	if err != nil {
		log.Fatal("Failed to initialize container provider: %v", err)
	}
	defer containerProvider.Close()

	// Ensure network exists
	networkName := cfg.Containers.NetworkName
	if networkName == "" {
		networkName = "discopanel-network"
	}
	if err := containerProvider.EnsureNetwork(ctx, networkName); err != nil {
		log.Error("Failed to ensure network: %v", err)
	}

	// Clean up orphaned containers on startup
	log.Info("Checking for orphaned containers...")
	servers, err := store.ListServers(ctx)
	if err != nil {
		log.Error("Failed to list servers for cleanup: %v", err)
	} else {
		// Build map of tracked container IDs
		trackedIDs := make(map[string]bool)
		for _, server := range servers {
			if server.ContainerID != "" {
				trackedIDs[server.ContainerID] = true
			}
		}

		// Clean up orphaned containers
		if err := containerProvider.CleanupOrphanedContainers(ctx, trackedIDs); err != nil {
			log.Error("Failed to cleanup orphaned containers: %v", err)
		}
	}

	// Load proxy configuration from database
	proxyConfig, isNew, err := store.GetProxyConfig(ctx)
	if err != nil {
		log.Warn("Failed to load proxy config from database, using file config: %v", err)
	} else {
		if isNew {
			proxyConfig.Enabled = cfg.Proxy.Enabled
			proxyConfig.BaseURL = cfg.Proxy.BaseURL
			err = store.SaveProxyConfig(ctx, proxyConfig)
			if err != nil {
				log.Error("Failed to set proxy configs from startup configuration values: %v", err)
			}
		} else {
			cfg.Proxy.Enabled = proxyConfig.Enabled
			cfg.Proxy.BaseURL = proxyConfig.BaseURL
		}

		// Load listeners and build ports array
		listeners, err := store.GetProxyListeners(ctx)
		if err == nil && len(listeners) > 0 {
			listenPorts := make([]int, 0, len(listeners))
			for _, l := range listeners {
				if l.Enabled {
					listenPorts = append(listenPorts, l.Port)
				}
			}
			if len(listenPorts) > 0 {
				cfg.Proxy.ListenPorts = listenPorts
				cfg.Proxy.ListenPort = listenPorts[0]
			}
		}

		log.Info("Loaded proxy configuration from database: enabled=%v, base_url=%v, listeners=%d",
			cfg.Proxy.Enabled, cfg.Proxy.BaseURL, len(cfg.Proxy.ListenPorts))
	}

	// Initialize proxy manager
	proxyManager := proxy.NewManager(store, &cfg.Proxy, log, containerProvider)

	// Start proxy if enabled
	if err := proxyManager.Start(); err != nil {
		log.Error("Failed to start proxy manager: %v", err)
	}
	defer proxyManager.Stop()

	// Initialize API server with full configuration
	apiServer := api.NewServer(store, containerProvider, cfg, log)
	apiServer.SetProxyManager(proxyManager)

	// Auto-start servers that have auto_start enabled
	log.Info("Checking for servers with auto-start enabled...")
	autoStartServers, err := store.ListServers(ctx)
	if err != nil {
		log.Warn("Failed to auto-start server instances due to error: %v\n", err)
	}

	for i := range autoStartServers {
		if autoStartServers[i].AutoStart && !autoStartServers[i].Detached {
			server := autoStartServers[i]
			log.Info("Auto-starting server: %s", server.Name)
			go func() {
				// Wait a moment for everything to initialize
				time.Sleep(2 * time.Second)

				// Get server config
				_, err := store.GetServerConfig(ctx, server.ID)
				if err != nil {
					log.Error("Failed to get config for auto-start server %s: %v", server.Name, err)
					return
				}

				statusStr, err := containerProvider.GetContainerStatus(ctx, server.ContainerID)
				if err != nil {
					log.Error("Failed to find existing container for auto-start server %s: %v", server.Name, err)
					return
				}

				status := storage.ServerStatus(statusStr)
				if status == storage.StatusStopped {
					// Start the container
					if err := containerProvider.Start(ctx, server.ContainerID); err != nil {
						log.Error("Failed to start container for auto-start server %s: %v", server.Name, err)
						return
					}
				}

				// Start log streaming for this container (whether it was just started or already running)
				if status == storage.StatusRunning || status == storage.StatusStopped {
					if err := apiServer.StartLogStreaming(server.ContainerID); err != nil {
						log.Error("Failed to start log streaming for auto-started server %s: %v", server.Name, err)
					}
				}

				// Update server status
				server.Status = storage.StatusRunning
				now := time.Now()
				server.LastStarted = &now
				if err := store.UpdateServer(ctx, server); err != nil {
					log.Error("Failed to update auto-start server %s: %v", server.Name, err)
				}

				// Update proxy route if enabled
				if server.ProxyHostname != "" {
					if err := proxyManager.UpdateServerRoute(server); err != nil {
						log.Error("Failed to update proxy route for auto-started server %s: %v", server.Name, err)
					}
				}

				log.Info("Successfully auto-started server: %s", server.Name)
			}()
		}
	}

	// Start container status monitor
	stopMonitor := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Duration(cfg.Containers.SyncInterval) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Update status for all servers with containers
				ctx := context.Background()
				servers, err := store.ListServers(ctx)
				if err != nil {
					continue
				}

				for _, server := range servers {
					if server.ContainerID != "" {
						statusStr, err := containerProvider.GetContainerStatus(ctx, server.ContainerID)
						status := storage.ServerStatus(statusStr)
						if err == nil && server.Status != status {
							server.Status = status
							if err := store.UpdateServer(ctx, server); err != nil {
								log.Error("Failed to update server status: %v", err)
							}
						}
					}
				}
			case <-stopMonitor:
				return
			}
		}
	}()

	// Setup HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port),
		Handler:      apiServer.Router(),
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(cfg.Server.IdleTimeout) * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Info("Starting DiscoPanel on %s:%s", cfg.Server.Host, cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop managed containers if auto-stop is enabled
	log.Info("Checking for managed containers...")
	managedServers, lsErr := store.ListServers(ctx)
	if lsErr != nil {
		log.Error("Unable to list managed containers prior to shutdown: %v", lsErr)
	}

	for _, server := range managedServers {
		if server.Detached {
			log.Info("Skipping shutdown of detached server: %s", server.Name)
		} else if server.Status == storage.StatusRunning {
			log.Info("Stopping managed container for server: %s", server.Name)
			timeout := 30 * time.Second
			if err := containerProvider.Stop(ctx, server.ContainerID, &timeout); err != nil {
				log.Error("Failed to stop container %s: %v", server.ContainerID, err)
			}
		}
	}

	if err := srv.Shutdown(ctx); err != nil {
		log.Error("Server forced to shutdown: %v", err)
	}

	log.Info("Server stopped\n")
}

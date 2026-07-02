package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nickheyer/discopanel/internal/command"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/events"
	"github.com/nickheyer/discopanel/internal/lifecycle"
	"github.com/nickheyer/discopanel/internal/metrics"
	"github.com/nickheyer/discopanel/internal/module"
	"github.com/nickheyer/discopanel/internal/provisioner"
	"github.com/nickheyer/discopanel/internal/proxy"
	"github.com/nickheyer/discopanel/internal/rpc"
	"github.com/nickheyer/discopanel/internal/scheduler"
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

	// Initialize storage w/ migrations and seeding
	store, err := storage.NewSQLiteStore(cfg)
	if err != nil {
		log.Fatal("Failed to initialize storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Initialize Docker client with configuration
	dockerClient, err := docker.NewClient(cfg.Docker.Host, log, docker.ClientConfig{
		APIVersion:   cfg.Docker.Version,
		NetworkName:  cfg.Docker.NetworkName,
		RegistryURL:  cfg.Docker.RegistryURL,
		RuntimeImage: cfg.Docker.RuntimeImage,
		DNS:          cfg.Docker.DNS,
		Labels:       cfg.Docker.Labels,
	})
	if err != nil {
		log.Fatal("Failed to initialize Docker client: %v", err)
	}
	defer dockerClient.Close()

	// Ensure Docker network exists
	if err := dockerClient.EnsureNetwork(); err != nil {
		log.Error("Failed to ensure Docker network: %v", err)
	}

	// Clean up orphaned containers on startup
	log.Info("Checking for orphaned containers...")
	servers, err := store.ListServers(ctx)
	if err != nil {
		log.Error("Failed to list servers for cleanup: %v", err)
	}
	modules, err := store.ListModules(ctx)
	if err != nil {
		log.Error("Failed to list modules for cleanup: %v", err)
	}

	// Build map of tracked container IDs
	trackedIDs := make(map[string]bool)
	for _, server := range servers {
		if server.ContainerID != "" {
			trackedIDs[server.ContainerID] = true
		}
	}
	for _, module := range modules {
		if module.ContainerID != "" {
			trackedIDs[module.ContainerID] = true
		}
	}

	// Clean up orphaned containers
	if err := dockerClient.CleanupOrphanedContainers(ctx, trackedIDs, log); err != nil {
		log.Error("Failed to cleanup orphaned containers: %v", err)
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
	proxyManager := proxy.NewManager(store, cfg, log)

	// Start proxy if enabled
	if err := proxyManager.Start(); err != nil {
		log.Error("Failed to start proxy manager: %v", err)
	}
	defer proxyManager.Stop()

	// Initialize command sender
	sender := command.NewSender(store, cfg)

	// Initialize the central event bus
	eventBus := events.NewBus(log)

	// Initialize metrics collector - it is also the panel-side health source
	metricsCollector := metrics.NewCollector(store, dockerClient, sender, cfg, eventBus, log)
	dockerClient.SetHealthChecker(metricsCollector)

	// Initialize the provisioner and the lifecycle manager (the single owner
	// of server start/stop/pause transitions)
	prov := provisioner.New(store, dockerClient, cfg, log)
	lifecycleManager := lifecycle.NewManager(store, dockerClient, prov, sender, proxyManager, eventBus, cfg, log)
	lifecycleManager.SetPlayerCounter(metricsCollector)

	// The proxy answers status pings for paused servers and wakes them on login
	proxyManager.SetServerGate(lifecycleManager)

	// Initialize task scheduler
	taskScheduler := scheduler.NewScheduler(store, dockerClient, sender, lifecycleManager, cfg, metricsCollector, log, scheduler.Config{
		CheckInterval: time.Duration(cfg.Docker.SyncInterval) * time.Second, // Use same interval as container status monitor
	})

	// Start the scheduler
	if err := taskScheduler.Start(); err != nil {
		log.Error("Failed to start task scheduler: %v", err)
	}
	defer taskScheduler.Stop()

	// Initialize builtin module templates
	if err := module.InitBuiltinTemplates(store); err != nil {
		log.Error("Failed to initialize builtin module templates: %v", err)
	}

	// Initialize module manager
	moduleManager := module.NewManager(store, dockerClient, sender, cfg, proxyManager, log)
	if err := moduleManager.Start(); err != nil {
		log.Error("Failed to start module manager: %v", err)
	}
	defer moduleManager.Stop()

	// Register event consumers on the event bus - EVENT CONSUMERS REGISTER HERE...
	eventBus.Subscribe(moduleManager.HandleServerEvent)
	eventBus.Subscribe(taskScheduler.HandleServerEvent)
	eventBus.Subscribe(lifecycleManager.HandleServerEvent)

	// Start the metrics collector now that consumers are subscribed
	if err := metricsCollector.Start(); err != nil {
		log.Error("Failed to start metrics collector: %v", err)
	}
	defer metricsCollector.Stop()

	// Start the idle watcher (autopause/autostop policies)
	lifecycleManager.StartIdleWatcher()
	defer lifecycleManager.StopIdleWatcher()

	// Initialize RPC server with full configuration
	rpcServer := rpc.NewServer(store, dockerClient, sender, cfg, proxyManager, taskScheduler, lifecycleManager, metricsCollector, moduleManager, eventBus, log)

	// Provisioning progress lines land in the server console via the log streamer
	if streamer := rpcServer.LogStreamer(); streamer != nil {
		prov.SetProgressSink(func(serverID, message string) {
			if server, err := store.GetServer(context.Background(), serverID); err == nil && server.ContainerID != "" {
				streamer.AddCommandOutput(server.ContainerID, "[provision] "+message, true, time.Now())
			}
		})
	}

	// Print recovery key
	if key := rpcServer.RecoveryKey(); key != "" {
		fmt.Fprintf(os.Stderr, "\n=======================================================================\n")
		fmt.Fprintf(os.Stderr, "RECOVERY KEY (use to reset panel access if locked out)\n")
		fmt.Fprintf(os.Stderr, "%s\n", key)
		fmt.Fprintf(os.Stderr, "=======================================================================\n\n")
		keyPath := filepath.Join(cfg.Storage.DataDir, "recovery.key")
		if err := os.WriteFile(keyPath, []byte(key), 0600); err != nil {
			log.Error("Failed to write recovery key file: %v", err)
		}
	}

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

				// Already-running containers just need their log stream reattached
				if server.ContainerID != "" {
					if status, err := dockerClient.GetContainerStatus(ctx, server.ContainerID); err == nil &&
						(status == storage.StatusRunning || status == storage.StatusStarting) {
						if err := rpcServer.StartLogStreaming(server.ContainerID); err != nil {
							log.Error("Failed to start log streaming for running server %s: %v", server.Name, err)
						}
						if server.ProxyHostname != "" {
							if err := proxyManager.UpdateServerRoute(server); err != nil {
								log.Error("Failed to update proxy route for %s: %v", server.Name, err)
							}
						}
						return
					}
				}

				startCtx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
				defer cancel()
				if err := lifecycleManager.Start(startCtx, server.ID); err != nil {
					log.Error("Failed to auto-start server %s: %v", server.Name, err)
					return
				}
				log.Info("Successfully auto-started server: %s", server.Name)
			}()
		}
	}

	// Clean expired sessions on startup, then periodically
	if err := store.CleanExpiredSessions(ctx); err != nil {
		log.Error("Failed to clean expired sessions on startup: %v", err)
	}
	stopSessionCleanup := make(chan struct{})
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := store.CleanExpiredSessions(context.Background()); err != nil {
					log.Error("Failed to clean expired sessions: %v", err)
				}
			case <-stopSessionCleanup:
				return
			}
		}
	}()

	// Start container status monitor
	stopMonitor := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Duration(cfg.Docker.SyncInterval) * time.Second)
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
						status, err := dockerClient.GetContainerStatus(ctx, server.ContainerID)
						if err == nil && server.Status != status {
							oldStatus := server.Status
							server.Status = status
							if err := store.UpdateServer(ctx, server); err != nil {
								log.Error("Failed to update server status: %v", err)
							}
							// Update proxy route if status changed and server has proxy configured
							if server.ProxyHostname != "" && oldStatus != status {
								if err := proxyManager.UpdateServerRoute(server); err != nil {
									log.Error("Failed to update proxy route for %s: %v", server.Name, err)
								}
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
		Handler:      rpcServer.Handler(),
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
	close(stopSessionCleanup)

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
			if _, err := dockerClient.StopContainer(ctx, server.ContainerID, 25); err != nil {
				log.Error("Failed to stop container %s: %v", server.ContainerID, err)
			}
		}
	}

	if err := srv.Shutdown(ctx); err != nil {
		log.Error("Server forced to shutdown: %v", err)
	}

	log.Info("Server stopped\n")
}

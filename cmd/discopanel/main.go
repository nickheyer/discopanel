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
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/pkg/logger"
)

func main() {
	var configPath = flag.String("config", "", "Path to configuration file")
	flag.Parse()

	// Initialize logger
	log := logger.New()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal("Failed to load configuration: %v", err)
	}

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

	// Initialize Docker client with configuration
	dockerClient, err := docker.NewClient(cfg.Docker.Host, docker.ClientConfig{
		APIVersion:    cfg.Docker.Version,
		NetworkName:   cfg.Docker.NetworkName,
		NetworkSubnet: cfg.Docker.NetworkSubnet,
		RegistryURL:   cfg.Docker.RegistryURL,
	})
	if err != nil {
		log.Fatal("Failed to initialize Docker client: %v", err)
	}
	defer dockerClient.Close()

	// Ensure Docker network exists
	if err := dockerClient.EnsureNetwork(); err != nil {
		log.Error("Failed to ensure Docker network: %v", err)
	}

	// Initialize API server with full configuration
	apiServer := api.NewServer(store, dockerClient, cfg, log)

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

	if err := srv.Shutdown(ctx); err != nil {
		log.Error("Server forced to shutdown: %v", err)
	}

	log.Info("Server stopped")
}

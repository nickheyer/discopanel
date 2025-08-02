package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nickheyer/discopanel/internal/api"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/storage"
	"github.com/nickheyer/discopanel/pkg/logger"
)

func main() {
	var (
		port       = flag.String("port", "8080", "HTTP server port")
		dbPath     = flag.String("db", "./discopanel.db", "Database file path")
		dataDir    = flag.String("data", "./data", "Data directory for server files")
		dockerHost = flag.String("docker", "unix:///var/run/docker.sock", "Docker daemon host")
	)
	flag.Parse()

	// Initialize logger
	log := logger.New()

	// Initialize storage
	store, err := storage.NewSQLiteStore(*dbPath)
	if err != nil {
		log.Fatal("Failed to initialize storage: %v", err)
	}
	defer store.Close()

	// Initialize Docker client
	dockerClient, err := docker.NewClient(*dockerHost)
	if err != nil {
		log.Fatal("Failed to initialize Docker client: %v", err)
	}
	defer dockerClient.Close()

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatal("Failed to create data directory: %v", err)
	}

	// Initialize API server
	apiServer := api.NewServer(store, dockerClient, *dataDir, log)

	// Setup HTTP server
	srv := &http.Server{
		Addr:         ":" + *port,
		Handler:      apiServer.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Info("Starting DiscoPanel on port %s", *port)
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

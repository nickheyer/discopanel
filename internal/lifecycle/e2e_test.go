package lifecycle_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nickheyer/discopanel/internal/command"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/events"
	"github.com/nickheyer/discopanel/internal/lifecycle"
	"github.com/nickheyer/discopanel/internal/metrics"
	"github.com/nickheyer/discopanel/internal/provisioner"
	"github.com/nickheyer/discopanel/pkg/logger"
)

// TestE2EVanillaServer provisions, boots, health-checks, RCONs, and stops a
// real vanilla server through the full lifecycle stack. It needs Docker, a
// locally available discopanel-runtime image, and network access:
//
//	DISCO_E2E=1 go test ./internal/lifecycle -run TestE2EVanillaServer -v -timeout 20m
//
// Override the tested Minecraft version with DISCO_E2E_MC_VERSION and the
// mod loader with DISCO_E2E_LOADER (e.g. fabric).
func TestE2EVanillaServer(t *testing.T) {
	if os.Getenv("DISCO_E2E") != "1" {
		t.Skip("set DISCO_E2E=1 to run the docker end-to-end test")
	}

	mcVersion := os.Getenv("DISCO_E2E_MC_VERSION")
	if mcVersion == "" {
		mcVersion = "1.21.1"
	}
	loader := storage.ModLoader(os.Getenv("DISCO_E2E_LOADER"))
	if loader == "" {
		loader = storage.ModLoaderVanilla
	}

	tmp := t.TempDir()
	log := logger.New()

	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	cfg.Database.Path = filepath.Join(tmp, "e2e.db")
	cfg.Storage.DataDir = tmp
	cfg.Storage.BackupDir = filepath.Join(tmp, "backups")
	cfg.Storage.TempDir = filepath.Join(tmp, "tmp")

	store, err := storage.NewSQLiteStore(cfg)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()

	dockerClient, err := docker.NewClient(cfg.Docker.Host, log, docker.ClientConfig{
		NetworkName: cfg.Docker.NetworkName,
	})
	if err != nil {
		t.Fatalf("docker: %v", err)
	}
	defer dockerClient.Close()
	if err := dockerClient.EnsureNetwork(); err != nil {
		t.Fatalf("network: %v", err)
	}

	sender := command.NewSender(store, dockerClient, cfg)
	bus := events.NewBus(log)
	collector := metrics.NewCollector(store, dockerClient, sender, cfg, bus, log, metrics.CollectorConfig{
		StatsInterval:       5 * time.Second,
		RCONInterval:        10 * time.Second,
		DiskInterval:        60 * time.Second,
		SLPInterval:         5 * time.Second,
		SLPTimeout:          5 * time.Second,
		SLPEnabled:          true,
		HealthStartupGrace:  10 * time.Minute,
		HealthFailThreshold: 3,
	})
	dockerClient.SetHealthChecker(collector)
	if err := collector.Start(); err != nil {
		t.Fatalf("collector: %v", err)
	}
	defer collector.Stop()

	prov := provisioner.New(store, dockerClient, cfg, log)
	prov.SetProgressSink(func(serverID, message string) {
		t.Logf("[provision] %s", message)
	})
	manager := lifecycle.NewManager(store, dockerClient, prov, sender, nil, bus, cfg, log)
	manager.SetPlayerCounter(collector)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	server := &storage.Server{
		ID:        "e2e-test",
		Name:      "e2e-test",
		ModLoader: loader,
		MCVersion: mcVersion,
		Status:    storage.StatusStopped,
		Port:      25599,
		Memory:    2048,
		DataPath:  filepath.Join(tmp, "servers", "e2e"),
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("create server: %v", err)
	}

	// Cleanup by container name: it must not depend on the store, whose
	// deferred Close runs before t.Cleanup callbacks.
	containerName := "discopanel-server-" + server.ID
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		dockerClient.StopContainer(cleanupCtx, containerName, 10)
		if err := dockerClient.RemoveContainer(cleanupCtx, containerName); err != nil {
			t.Logf("cleanup: could not remove container %s: %v", containerName, err)
		}
	})

	// Provision + start.
	if err := manager.Start(ctx, server.ID); err != nil {
		t.Fatalf("lifecycle start: %v", err)
	}

	// Wait until panel-side health (SLP) reports the server running.
	deadline := time.Now().Add(10 * time.Minute)
	for {
		s, err := store.GetServer(ctx, server.ID)
		if err != nil {
			t.Fatalf("get server: %v", err)
		}
		status, err := dockerClient.GetContainerStatus(ctx, s.ContainerID)
		if err != nil {
			t.Fatalf("container status: %v", err)
		}
		if status == storage.StatusRunning {
			t.Logf("server is running and answering SLP pings")
			break
		}
		if status == storage.StatusStopped || status == storage.StatusError {
			t.Fatalf("server reached terminal status %s before becoming healthy", status)
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not become healthy in time (last status %s)", status)
		}
		time.Sleep(3 * time.Second)
	}

	// RCON must work through the panel's native client.
	output, err := sender.SendCommand(ctx, server.ID, "list")
	if err != nil {
		t.Fatalf("rcon list: %v", err)
	}
	t.Logf("rcon list output: %q", output)

	// Graceful stop.
	if err := manager.Stop(ctx, server.ID); err != nil {
		t.Fatalf("lifecycle stop: %v", err)
	}
	s, err := store.GetServer(ctx, server.ID)
	if err != nil {
		t.Fatalf("get server after stop: %v", err)
	}
	if s.Status != storage.StatusStopped {
		t.Fatalf("expected stopped status, got %s", s.Status)
	}
	t.Logf("server stopped cleanly")
}

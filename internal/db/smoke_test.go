package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nickheyer/discopanel/pkg/config"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestProtoModelSmoke(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Path = filepath.Join(t.TempDir(), "smoke.db")
	cfg.Database.AutoMigrate = true
	store, err := NewSQLiteStore(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	server := &v1.Server{
		Id:             "srv-1",
		Name:           "smoke",
		ModLoader:      v1.ModLoader_MOD_LOADER_FABRIC,
		McVersion:      "1.21.1",
		Status:         v1.ServerStatus_SERVER_STATUS_STOPPED,
		Port:           25565,
		DataPath:       t.TempDir(),
		AgentTokenHash: "sekrit-hash",
		AdditionalPorts: []*v1.AdditionalPort{
			{Name: "map", ContainerPort: 8100, HostPort: 8100, Protocol: "tcp"},
		},
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("create: %v", err)
	}
	if server.CreatedAt == nil || server.UpdatedAt == nil {
		t.Fatal("timestamp hooks did not fire")
	}

	got, err := store.GetServer(ctx, "srv-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ModLoader != v1.ModLoader_MOD_LOADER_FABRIC || got.Status != v1.ServerStatus_SERVER_STATUS_STOPPED {
		t.Fatalf("enums wrong: %v %v", got.ModLoader, got.Status)
	}
	if got.AgentTokenHash != "sekrit-hash" {
		t.Fatal("private column lost")
	}
	if len(got.AdditionalPorts) != 1 || got.AdditionalPorts[0].HostPort != 8100 {
		t.Fatalf("json serializer lost ports: %+v", got.AdditionalPorts)
	}
	if got.CreatedAt.AsTime().IsZero() {
		t.Fatal("created_at not persisted")
	}

	// Properties row was synced at create
	props, err := store.GetServerProperties(ctx, "srv-1")
	if err != nil {
		t.Fatalf("props: %v", err)
	}
	if props.ServerPort == nil || *props.ServerPort != 25565 {
		t.Fatalf("props sync wrong: %+v", props.ServerPort)
	}

	// Status update through map path
	if err := store.UpdateServerFields(ctx, "srv-1", map[string]any{"status": v1.ServerStatus_SERVER_STATUS_RUNNING}); err != nil {
		t.Fatalf("status: %v", err)
	}
	got, _ = store.GetServer(ctx, "srv-1")
	if got.Status != v1.ServerStatus_SERVER_STATUS_RUNNING {
		t.Fatalf("status not updated: %v", got.Status)
	}

	// Relation preload via session
	user := &v1.User{Id: "u1", Username: "nick", AuthProvider: "local", IsActive: true, PasswordHash: "h"}
	if err := store.CreateUser(ctx, user); err != nil {
		t.Fatalf("user: %v", err)
	}
	sess := &v1.Session{Id: "s1", UserId: "u1", Token: "tok", ExpiresAt: timestamppb.New(timestamppb.Now().AsTime().Add(3600e9))}
	if err := store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("session: %v", err)
	}
	loaded, err := store.GetSession(ctx, "tok", time.Now().UTC())
	if err != nil {
		t.Fatalf("session load: %v", err)
	}
	if loaded.User == nil || loaded.User.Username != "nick" {
		t.Fatalf("preload failed: %+v", loaded.User)
	}

	// Redact clears secrets for the wire
	clone := got.Redact()
	if clone.AgentTokenHash != "" {
		t.Fatal("redact failed")
	}
}

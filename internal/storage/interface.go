package storage

import (
	"context"
	"github.com/nickheyer/discopanel/internal/models"
)

type Store interface {
	// Server operations
	CreateServer(ctx context.Context, server *models.Server) error
	GetServer(ctx context.Context, id string) (*models.Server, error)
	ListServers(ctx context.Context) ([]*models.Server, error)
	UpdateServer(ctx context.Context, server *models.Server) error
	DeleteServer(ctx context.Context, id string) error
	GetServerByPort(ctx context.Context, port int) (*models.Server, error)

	// Server config operations
	GetServerConfig(ctx context.Context, serverID string) (*models.ServerConfig, error)
	UpdateServerConfig(ctx context.Context, config *models.ServerConfig) error

	// Mod operations
	AddMod(ctx context.Context, mod *models.Mod) error
	GetMod(ctx context.Context, id string) (*models.Mod, error)
	ListServerMods(ctx context.Context, serverID string) ([]*models.Mod, error)
	UpdateMod(ctx context.Context, mod *models.Mod) error
	DeleteMod(ctx context.Context, id string) error

	// Database management
	Close() error
	Migrate() error
}
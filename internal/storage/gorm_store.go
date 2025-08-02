package storage

import (
	"context"
	"fmt"

	"github.com/nickheyer/discopanel/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type GormStore struct {
	db *gorm.DB
}

func NewSQLiteStore(dbPath string) (*GormStore, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &GormStore{db: db}
	
	if err := store.Migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return store, nil
}

func (s *GormStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (s *GormStore) Migrate() error {
	// Auto-migrate all models
	err := s.db.AutoMigrate(
		&models.Server{},
		&models.ServerConfig{},
		&models.Mod{},
	)
	if err != nil {
		return fmt.Errorf("failed to auto-migrate: %w", err)
	}

	// Create indexes
	if err := s.db.Exec("CREATE INDEX IF NOT EXISTS idx_servers_port ON servers(port)").Error; err != nil {
		return err
	}

	return nil
}

// Server operations
func (s *GormStore) CreateServer(ctx context.Context, server *models.Server) error {
	err := s.db.WithContext(ctx).Create(server).Error
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Create default server config
	config := &models.ServerConfig{
		ID:       server.ID + "-config",
		ServerID: server.ID,
	}
	
	return s.db.WithContext(ctx).Create(config).Error
}

func (s *GormStore) GetServer(ctx context.Context, id string) (*models.Server, error) {
	var server models.Server
	err := s.db.WithContext(ctx).First(&server, "id = ?", id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("server not found")
		}
		return nil, err
	}
	return &server, nil
}

func (s *GormStore) ListServers(ctx context.Context) ([]*models.Server, error) {
	var servers []*models.Server
	err := s.db.WithContext(ctx).Order("created_at DESC").Find(&servers).Error
	return servers, err
}

func (s *GormStore) UpdateServer(ctx context.Context, server *models.Server) error {
	return s.db.WithContext(ctx).Save(server).Error
}

func (s *GormStore) DeleteServer(ctx context.Context, id string) error {
	// Delete with associations
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete mods
		if err := tx.Where("server_id = ?", id).Delete(&models.Mod{}).Error; err != nil {
			return err
		}
		
		// Delete config
		if err := tx.Where("server_id = ?", id).Delete(&models.ServerConfig{}).Error; err != nil {
			return err
		}
		
		// Delete server
		return tx.Delete(&models.Server{}, "id = ?", id).Error
	})
}

func (s *GormStore) GetServerByPort(ctx context.Context, port int) (*models.Server, error) {
	var server models.Server
	err := s.db.WithContext(ctx).Where("port = ?", port).First(&server).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &server, nil
}

// Server config operations
func (s *GormStore) GetServerConfig(ctx context.Context, serverID string) (*models.ServerConfig, error) {
	var config models.ServerConfig
	err := s.db.WithContext(ctx).Where("server_id = ?", serverID).First(&config).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("server config not found")
		}
		return nil, err
	}
	return &config, nil
}

func (s *GormStore) UpdateServerConfig(ctx context.Context, config *models.ServerConfig) error {
	return s.db.WithContext(ctx).Save(config).Error
}

// Mod operations
func (s *GormStore) AddMod(ctx context.Context, mod *models.Mod) error {
	return s.db.WithContext(ctx).Create(mod).Error
}

func (s *GormStore) GetMod(ctx context.Context, id string) (*models.Mod, error) {
	var mod models.Mod
	err := s.db.WithContext(ctx).First(&mod, "id = ?", id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("mod not found")
		}
		return nil, err
	}
	return &mod, nil
}

func (s *GormStore) ListServerMods(ctx context.Context, serverID string) ([]*models.Mod, error) {
	var mods []*models.Mod
	err := s.db.WithContext(ctx).Where("server_id = ?", serverID).Order("name").Find(&mods).Error
	return mods, err
}

func (s *GormStore) UpdateMod(ctx context.Context, mod *models.Mod) error {
	return s.db.WithContext(ctx).Save(mod).Error
}

func (s *GormStore) DeleteMod(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&models.Mod{}, "id = ?", id).Error
}
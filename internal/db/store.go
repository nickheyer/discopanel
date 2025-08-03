package db

import (
	"context"
	"fmt"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DBConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

type Store struct {
	db *gorm.DB
}

func NewSQLiteStore(dbPath string, config ...DBConfig) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Get underlying SQL database to configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database handle: %w", err)
	}

	// Apply connection pool configuration if provided
	if len(config) > 0 {
		cfg := config[0]
		if cfg.MaxOpenConns > 0 {
			sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
		}
		if cfg.MaxIdleConns > 0 {
			sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
		}
		if cfg.ConnMaxLifetime > 0 {
			sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
		}
	}

	store := &Store{db: db}

	if err := store.Migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return store, nil
}

func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (s *Store) Migrate() error {
	// Auto-migrate all models
	err := s.db.AutoMigrate(
		&Server{},
		&ServerConfig{},
		&Mod{},
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
func (s *Store) CreateServer(ctx context.Context, server *Server) error {
	err := s.db.WithContext(ctx).Create(server).Error
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Create default server config
	config := &ServerConfig{
		ID:       server.ID + "-config",
		ServerID: server.ID,
	}

	return s.db.WithContext(ctx).Create(config).Error
}

func (s *Store) GetServer(ctx context.Context, id string) (*Server, error) {
	var server Server
	err := s.db.WithContext(ctx).First(&server, "id = ?", id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("server not found")
		}
		return nil, err
	}
	return &server, nil
}

func (s *Store) ListServers(ctx context.Context) ([]*Server, error) {
	var servers []*Server
	err := s.db.WithContext(ctx).Order("created_at DESC").Find(&servers).Error
	return servers, err
}

func (s *Store) UpdateServer(ctx context.Context, server *Server) error {
	return s.db.WithContext(ctx).Save(server).Error
}

func (s *Store) DeleteServer(ctx context.Context, id string) error {
	// Delete with associations
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete mods
		if err := tx.Where("server_id = ?", id).Delete(&Mod{}).Error; err != nil {
			return err
		}

		// Delete config
		if err := tx.Where("server_id = ?", id).Delete(&ServerConfig{}).Error; err != nil {
			return err
		}

		// Delete server
		return tx.Delete(&Server{}, "id = ?", id).Error
	})
}

func (s *Store) GetServerByPort(ctx context.Context, port int) (*Server, error) {
	var server Server
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
func (s *Store) GetServerConfig(ctx context.Context, serverID string) (*ServerConfig, error) {
	var config ServerConfig
	err := s.db.WithContext(ctx).Where("server_id = ?", serverID).First(&config).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("server config not found")
		}
		return nil, err
	}
	return &config, nil
}

func (s *Store) UpdateServerConfig(ctx context.Context, config *ServerConfig) error {
	return s.db.WithContext(ctx).Save(config).Error
}

// Mod operations
func (s *Store) AddMod(ctx context.Context, mod *Mod) error {
	return s.db.WithContext(ctx).Create(mod).Error
}

func (s *Store) GetMod(ctx context.Context, id string) (*Mod, error) {
	var mod Mod
	err := s.db.WithContext(ctx).First(&mod, "id = ?", id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("mod not found")
		}
		return nil, err
	}
	return &mod, nil
}

func (s *Store) ListServerMods(ctx context.Context, serverID string) ([]*Mod, error) {
	var mods []*Mod
	err := s.db.WithContext(ctx).Where("server_id = ?", serverID).Order("name").Find(&mods).Error
	return mods, err
}

func (s *Store) UpdateMod(ctx context.Context, mod *Mod) error {
	return s.db.WithContext(ctx).Save(mod).Error
}

func (s *Store) DeleteMod(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&Mod{}, "id = ?", id).Error
}

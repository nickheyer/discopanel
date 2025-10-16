package db

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
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
		&IndexedModpack{},
		&IndexedModpackFile{},
		&ModpackFavorite{},
		&ProxyConfig{},
		&ProxyListener{},
		&User{},
		&AuthConfig{},
		&Session{},
		&ScheduledJob{},
	)
	if err != nil {
		return fmt.Errorf("failed to auto-migrate: %w", err)
	}

	// Create indexes
	if err := s.db.Exec("CREATE INDEX IF NOT EXISTS idx_servers_port ON servers(port)").Error; err != nil {
		return err
	}
	if err := s.db.Exec("CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at)").Error; err != nil {
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

	// Create and sync server config
	return s.SyncServerConfigWithServer(ctx, server)
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
	if err := s.db.WithContext(ctx).Save(server).Error; err != nil {
		return err
	}
	// Sync config with updated server settings
	return s.SyncServerConfigWithServer(ctx, server)
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
	// Only check servers that don't have a proxy hostname (i.e., servers that actually bind to the port)
	err := s.db.WithContext(ctx).Where("port = ? AND (proxy_hostname IS NULL OR proxy_hostname = '')", port).First(&server).Error
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

func (s *Store) SaveServerConfig(ctx context.Context, config *ServerConfig) error {
	return s.db.WithContext(ctx).Save(config).Error
}

// UpdateServerConfigMemory updates memory settings in ServerConfig
func (s *Store) UpdateServerConfigMemory(ctx context.Context, serverID string, memory string) error {
	config, err := s.GetServerConfig(ctx, serverID)
	if err != nil {
		return err
	}

	// Update memory and max memory (they're the same I THINK)
	config.Memory = &memory
	config.MaxMemory = &memory

	// Only update InitMemory if it's not already set
	if config.InitMemory == nil {
		memoryValue, _ := strconv.Atoi(strings.TrimSuffix(memory, "M"))
		initMemoryValue := max(memoryValue/4, 1024) // Minimum 1G
		initMemoryStr := fmt.Sprintf("%dM", initMemoryValue)
		config.InitMemory = &initMemoryStr
	}

	return s.SaveServerConfig(ctx, config)
}

// ClearEphemeralConfigFields clears all ephemeral configuration fields
func (s *Store) ClearEphemeralConfigFields(ctx context.Context, serverID string) error {
	config, err := s.GetServerConfig(ctx, serverID)
	if err != nil {
		return err
	}

	// Clear ephemeral fields
	config.CFForceReinstallModloader = nil

	return s.SaveServerConfig(ctx, config)
}

// SyncServerConfigWithServer updates system fields in ServerConfig based on Server settings
func (s *Store) SyncServerConfigWithServer(ctx context.Context, server *Server) error {
	// Get or create config
	config, err := s.GetServerConfig(ctx, server.ID)
	if err != nil {
		if err.Error() == "server config not found" {
			config = s.CreateDefaultServerConfig(server.ID)
		} else {
			return err
		}
	}

	// Helper functions
	stringPtr := func(s string) *string { return &s }
	intPtr := func(i int) *int { return &i }

	// Update system fields
	// Set memory as the max, with init at 1/4 of max for better JVM performance
	maxMemory := fmt.Sprintf("%dM", server.Memory)
	initMemory := fmt.Sprintf("%dM", server.Memory/4)
	if server.Memory/4 < 512 {
		initMemory = "512M" // Minimum 512MB initial
	}

	config.Memory = stringPtr(maxMemory)      // This is used by the container as -Xmx
	config.InitMemory = stringPtr(initMemory) // -Xms
	config.MaxMemory = stringPtr(maxMemory)   // -Xmx
	config.Type = stringPtr(string(server.ModLoader))
	config.Version = stringPtr(server.MCVersion)
	config.ServerPort = intPtr(server.Port)

	return s.SaveServerConfig(ctx, config)
}

func (s *Store) CreateDefaultServerConfig(serverID string) *ServerConfig {
	// Helper functions to create pointers
	boolPtr := func(b bool) *bool { return &b }
	stringPtr := func(s string) *string { return &s }
	intPtr := func(i int) *int { return &i }

	// Start with basic defaults
	rconPassword := "discopanel_default"
	if serverID != "" && len(serverID) >= 8 {
		rconPassword = fmt.Sprintf("discopanel_%s", serverID[:8])
	}

	config := &ServerConfig{
		ID:           serverID + "-config",
		ServerID:     serverID,
		EULA:         stringPtr("TRUE"),
		EnableRCON:   boolPtr(true),
		RCONPassword: stringPtr(rconPassword),
		Memory:       stringPtr("2G"),
		Version:      stringPtr("LATEST"),
		Type:         stringPtr("VANILLA"),
		Difficulty:   stringPtr("easy"),
		Mode:         stringPtr("survival"),
		MaxPlayers:   intPtr(20),
	}

	// Don't try to get global settings if we're creating the global settings themselves
	if serverID == GlobalSettingsID {
		return config
	}

	// Get global settings and copy non-nil values
	var globalSettings ServerConfig
	err := s.db.Where("id = ?", GlobalSettingsID).First(&globalSettings).Error
	if err == nil {
		// Use reflection to copy non-nil values from global settings
		globalValue := reflect.ValueOf(&globalSettings).Elem()
		configValue := reflect.ValueOf(config).Elem()
		configType := configValue.Type()

		for i := 0; i < configType.NumField(); i++ {
			field := configType.Field(i)
			// Skip these fields as they're server-specific
			if field.Name == "ID" || field.Name == "ServerID" || field.Name == "UpdatedAt" ||
				field.Name == "Server" || field.Name == "RCONPassword" ||
				field.Name == "Type" || field.Name == "Version" || field.Name == "Memory" ||
				field.Name == "InitMemory" || field.Name == "MaxMemory" || field.Name == "ServerPort" {
				continue
			}

			globalField := globalValue.FieldByName(field.Name)
			if globalField.IsValid() && globalField.Kind() == reflect.Ptr && !globalField.IsNil() {
				configValue.Field(i).Set(globalField)
			}
		}
	}

	return config
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

// Indexed Modpack operations
func (s *Store) UpsertIndexedModpack(ctx context.Context, modpack *IndexedModpack) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing IndexedModpack
		err := tx.Where("id = ?", modpack.ID).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			return tx.Create(modpack).Error
		}
		if err != nil {
			return err
		}
		modpack.IndexedAt = existing.IndexedAt
		return tx.Save(modpack).Error
	})
}

func (s *Store) GetIndexedModpack(ctx context.Context, id string) (*IndexedModpack, error) {
	var modpack IndexedModpack
	err := s.db.WithContext(ctx).First(&modpack, "id = ?", id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("modpack not found")
		}
		return nil, err
	}
	return &modpack, nil
}

func (s *Store) SearchIndexedModpacks(ctx context.Context, query string, gameVersion string, modLoader string, indexer string, offset, limit int) ([]*IndexedModpack, int64, error) {
	db := s.db.WithContext(ctx).Model(&IndexedModpack{})

	if query != "" {
		db = db.Where("name LIKE ? OR summary LIKE ?", "%"+query+"%", "%"+query+"%")
	}

	if gameVersion != "" {
		db = db.Where("game_versions LIKE ?", "%"+gameVersion+"%")
	}

	if modLoader != "" {
		db = db.Where("mod_loaders LIKE ?", "%"+modLoader+"%")
	}

	if indexer != "" {
		db = db.Where("indexer = ?", indexer)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var modpacks []*IndexedModpack
	err := db.Order("download_count DESC").
		Offset(offset).
		Limit(limit).
		Find(&modpacks).Error

	return modpacks, total, err
}

func (s *Store) ListIndexedModpacks(ctx context.Context, offset, limit int) ([]*IndexedModpack, int64, error) {
	var total int64
	if err := s.db.WithContext(ctx).Model(&IndexedModpack{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var modpacks []*IndexedModpack
	err := s.db.WithContext(ctx).
		Order("download_count DESC").
		Offset(offset).
		Limit(limit).
		Find(&modpacks).Error

	return modpacks, total, err
}

// ScheduledJob operations
func (s *Store) CreateScheduledJob(ctx context.Context, job *ScheduledJob) error {
	if job.ID == "" {
		job.ID = fmt.Sprintf("sched-%d", time.Now().UnixNano())
	}
	return s.db.WithContext(ctx).Create(job).Error
}

func (s *Store) ListScheduledJobs(ctx context.Context, serverID string) ([]*ScheduledJob, error) {
	var jobs []*ScheduledJob
	db := s.db.WithContext(ctx).Order("created_at DESC")
	if serverID != "" {
		db = db.Where("server_id = ?", serverID)
	}
	err := db.Find(&jobs).Error
	return jobs, err
}

func (s *Store) GetScheduledJob(ctx context.Context, id string) (*ScheduledJob, error) {
	var job ScheduledJob
	err := s.db.WithContext(ctx).First(&job, "id = ?", id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("scheduled job not found")
		}
		return nil, err
	}
	return &job, nil
}

func (s *Store) UpdateScheduledJob(ctx context.Context, job *ScheduledJob) error {
	return s.db.WithContext(ctx).Save(job).Error
}

func (s *Store) DeleteScheduledJob(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&ScheduledJob{}, "id = ?", id).Error
}

// Indexed Modpack File operations
func (s *Store) UpsertIndexedModpackFile(ctx context.Context, file *IndexedModpackFile) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing IndexedModpackFile
		err := tx.Where("id = ?", file.ID).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			return tx.Create(file).Error
		}
		if err != nil {
			return err
		}
		return tx.Save(file).Error
	})
}

func (s *Store) GetIndexedModpackFiles(ctx context.Context, modpackID string) ([]*IndexedModpackFile, error) {
	var files []*IndexedModpackFile
	err := s.db.WithContext(ctx).
		Where("modpack_id = ?", modpackID).
		Order("file_date DESC").
		Find(&files).Error
	return files, err
}

// Modpack Favorite operations
func (s *Store) AddModpackFavorite(ctx context.Context, modpackID string) error {
	favorite := &ModpackFavorite{
		ID:        fmt.Sprintf("fav-%s-%d", modpackID, time.Now().Unix()),
		ModpackID: modpackID,
	}
	return s.db.WithContext(ctx).Create(favorite).Error
}

func (s *Store) RemoveModpackFavorite(ctx context.Context, modpackID string) error {
	return s.db.WithContext(ctx).Where("modpack_id = ?", modpackID).Delete(&ModpackFavorite{}).Error
}

func (s *Store) IsModpackFavorited(ctx context.Context, modpackID string) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&ModpackFavorite{}).Where("modpack_id = ?", modpackID).Count(&count).Error
	return count > 0, err
}

func (s *Store) ListFavoriteModpacks(ctx context.Context) ([]*IndexedModpack, error) {
	var favorites []*ModpackFavorite
	err := s.db.WithContext(ctx).
		Preload("Modpack").
		Order("created_at DESC").
		Find(&favorites).Error
	if err != nil {
		return nil, err
	}

	modpacks := make([]*IndexedModpack, 0, len(favorites))
	for _, fav := range favorites {
		if fav.Modpack != nil {
			modpacks = append(modpacks, fav.Modpack)
		}
	}

	return modpacks, nil
}

// Global Settings operations (using ServerConfig with a special ID)
const GlobalSettingsID = "global-settings"

func (s *Store) GetGlobalSettings(ctx context.Context) (*ServerConfig, bool, error) {
	var config ServerConfig
	isNew := false
	err := s.db.WithContext(ctx).Where("id = ?", GlobalSettingsID).First(&config).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// Create EMPTY global settings - no defaults!
			config = ServerConfig{
				ID:       GlobalSettingsID,
				ServerID: GlobalSettingsID,
			}
			if err := s.db.WithContext(ctx).Create(&config).Error; err != nil {
				return nil, isNew, err
			}

			isNew = true
			return &config, isNew, nil
		}
		return nil, isNew, err
	}
	return &config, isNew, nil
}

func (s *Store) UpdateGlobalSettings(ctx context.Context, config *ServerConfig) error {
	config.ID = GlobalSettingsID
	config.ServerID = GlobalSettingsID
	return s.db.WithContext(ctx).Save(config).Error
}

// ProxyConfig operations
func (s *Store) GetProxyConfig(ctx context.Context) (*ProxyConfig, bool, error) {
	var config ProxyConfig
	err := s.db.WithContext(ctx).First(&config).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// Return default config if none exists
			return &ProxyConfig{
				ID:      "default",
				Enabled: false,
				BaseURL: "",
			}, true, nil
		}
		return nil, false, err
	}
	return &config, false, nil
}

func (s *Store) SaveProxyConfig(ctx context.Context, config *ProxyConfig) error {
	if config.ID == "" {
		config.ID = "default"
	}

	// Use Save to create or update
	return s.db.WithContext(ctx).Save(config).Error
}

// ProxyListener operations
func (s *Store) GetProxyListeners(ctx context.Context) ([]*ProxyListener, error) {
	var listeners []*ProxyListener
	err := s.db.WithContext(ctx).Order("is_default DESC, port ASC").Find(&listeners).Error
	if err != nil {
		return nil, err
	}

	// If no listeners exist, create a default one
	if len(listeners) == 0 {
		defaultListener := &ProxyListener{
			ID:        "default",
			Port:      25565,
			Name:      "Primary",
			IsDefault: true,
			Enabled:   true,
		}
		if err := s.CreateProxyListener(ctx, defaultListener); err != nil {
			return nil, err
		}
		listeners = []*ProxyListener{defaultListener}
	}

	return listeners, nil
}

func (s *Store) GetProxyListener(ctx context.Context, id string) (*ProxyListener, error) {
	var listener ProxyListener
	err := s.db.WithContext(ctx).First(&listener, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &listener, nil
}

func (s *Store) GetProxyListenerByPort(ctx context.Context, port int) (*ProxyListener, error) {
	var listener ProxyListener
	err := s.db.WithContext(ctx).First(&listener, "port = ?", port).Error
	if err != nil {
		return nil, err
	}
	return &listener, nil
}

func (s *Store) CreateProxyListener(ctx context.Context, listener *ProxyListener) error {
	if listener.ID == "" {
		listener.ID = uuid.New().String()
	}
	return s.db.WithContext(ctx).Create(listener).Error
}

func (s *Store) UpdateProxyListener(ctx context.Context, listener *ProxyListener) error {
	return s.db.WithContext(ctx).Save(listener).Error
}

func (s *Store) DeleteProxyListener(ctx context.Context, id string) error {
	// Don't delete if servers are using it
	var count int64
	s.db.Model(&Server{}).Where("proxy_listener_id = ?", id).Count(&count)
	if count > 0 {
		return fmt.Errorf("cannot delete listener: %d servers are using it", count)
	}

	return s.db.WithContext(ctx).Delete(&ProxyListener{}, "id = ?", id).Error
}

// User operations
func (s *Store) CreateUser(ctx context.Context, user *User) error {
	if user.ID == "" {
		user.ID = uuid.New().String()
	}
	return s.db.WithContext(ctx).Create(user).Error
}

func (s *Store) GetUser(ctx context.Context, id string) (*User, error) {
	var user User
	err := s.db.WithContext(ctx).First(&user, "id = ?", id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("user not found")
		}
		return nil, err
	}
	return &user, nil
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	var user User
	err := s.db.WithContext(ctx).First(&user, "username = ?", username).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("user not found")
		}
		return nil, err
	}
	return &user, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	var user User
	err := s.db.WithContext(ctx).First(&user, "email = ?", email).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("user not found")
		}
		return nil, err
	}
	return &user, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]*User, error) {
	var users []*User
	err := s.db.WithContext(ctx).Order("created_at DESC").Find(&users).Error
	return users, err
}

func (s *Store) UpdateUser(ctx context.Context, user *User) error {
	return s.db.WithContext(ctx).Save(user).Error
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete sessions
		if err := tx.Where("user_id = ?", id).Delete(&Session{}).Error; err != nil {
			return err
		}
		// Delete user
		return tx.Delete(&User{}, "id = ?", id).Error
	})
}

func (s *Store) CountUsers(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&User{}).Count(&count).Error
	return count, err
}

// AuthConfig operations
func (s *Store) GetAuthConfig(ctx context.Context) (*AuthConfig, bool, error) {
	var config AuthConfig
	err := s.db.WithContext(ctx).First(&config).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// Return default config if none exists
			return &AuthConfig{
				ID:                 "default",
				Enabled:            false,
				SessionTimeout:     86400, // 24 hours
				RequireEmailVerify: false,
				AllowRegistration:  false,
			}, true, nil
		}
		return nil, false, err
	}
	return &config, false, nil
}

func (s *Store) SaveAuthConfig(ctx context.Context, config *AuthConfig) error {
	if config.ID == "" {
		config.ID = "default"
	}
	return s.db.WithContext(ctx).Save(config).Error
}

// Session operations
func (s *Store) CreateSession(ctx context.Context, session *Session) error {
	if session.ID == "" {
		session.ID = uuid.New().String()
	}
	return s.db.WithContext(ctx).Create(session).Error
}

func (s *Store) GetSession(ctx context.Context, token string) (*Session, error) {
	var session Session
	err := s.db.WithContext(ctx).Preload("User").Where("token = ? AND expires_at > ?", token, time.Now()).First(&session).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("session not found or expired")
		}
		return nil, err
	}
	return &session, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	return s.db.WithContext(ctx).Where("token = ?", token).Delete(&Session{}).Error
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID string) error {
	return s.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&Session{}).Error
}

func (s *Store) CleanExpiredSessions(ctx context.Context) error {
	return s.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&Session{}).Error
}

package db

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/google/uuid"
	"github.com/nickheyer/discopanel/internal/config"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Store struct {
	db  *gorm.DB
	cfg *config.Config
}

func NewSQLiteStore(cfg *config.Config) (*Store, error) {
	dsn := cfg.Database.Path
	// Pragmas reduce locked database errors under load
	if dsn != ":memory:" && !strings.Contains(dsn, "?") {
		dsn += "?_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL"
	}
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database handle: %w", err)
	}

	if cfg.Database.MaxConnections > 0 {
		sqlDB.SetMaxOpenConns(cfg.Database.MaxConnections)
	}
	if cfg.Database.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	}
	if cfg.Database.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(time.Duration(cfg.Database.ConnMaxLifetime) * time.Second)
	}

	store := &Store{db: db, cfg: cfg}

	if cfg.Database.AutoMigrate {
		if err := store.Migrate(); err != nil {
			return nil, fmt.Errorf("failed to migrate database: %w", err)
		}
	}

	return store, nil
}

func (s *Store) DB() *gorm.DB {
	return s.db
}

func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Server operations
func (s *Store) CreateServer(ctx context.Context, server *Server) error {
	err := s.db.WithContext(ctx).Create(server).Error
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Create and sync server properties
	return s.SyncServerPropertiesWithServer(ctx, server)
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

// Resolves the server owning an agent token's SHA-256 hash
func (s *Store) GetServerByAgentTokenHash(ctx context.Context, hash string) (*Server, error) {
	if hash == "" {
		return nil, fmt.Errorf("server not found: %w", gorm.ErrRecordNotFound)
	}
	var server Server
	err := s.db.WithContext(ctx).First(&server, "agent_token_hash = ?", hash).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("server not found: %w", err)
		}
		return nil, err
	}
	return &server, nil
}

func (s *Store) UpdateServer(ctx context.Context, server *Server) error {
	if err := s.db.WithContext(ctx).Save(server).Error; err != nil {
		return err
	}
	// Sync properties with updated server settings
	return s.SyncServerPropertiesWithServer(ctx, server)
}

func (s *Store) DeleteServer(ctx context.Context, id string) error {
	// Delete with associations
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete mods
		if err := tx.Where("server_id = ?", id).Delete(&Mod{}).Error; err != nil {
			return err
		}

		// Delete properties
		if err := tx.Where("server_id = ?", id).Delete(&ServerProperties{}).Error; err != nil {
			return err
		}

		// Delete metrics history
		if err := tx.Where("server_id = ?", id).Delete(&MetricsSample{}).Error; err != nil {
			return err
		}

		// Delete server
		return tx.Delete(&Server{}, "id = ?", id).Error
	})
}

// Metrics history operations

// Inserts one batch of telemetry points
func (s *Store) AddMetricsSamples(ctx context.Context, samples []*MetricsSample) error {
	if len(samples) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Create(samples).Error
}

// Uses datetime() since stored timestamps may lack UTC offset

// Scan target for bucketed metrics aggregation
type metricsBucketRow struct {
	ServerID   string  `gorm:"column:server_id"`
	Bucket     int64   `gorm:"column:bucket"`
	TPS        float64 `gorm:"column:tps"`
	MSPT       float64 `gorm:"column:mspt"`
	Players    int     `gorm:"column:players"`
	CPUPercent float64 `gorm:"column:cpu_percent"`
	MemoryMB   float64 `gorm:"column:memory_mb"`
	HeapUsedMB float64 `gorm:"column:heap_used_mb"`
	DiskBytes  int64   `gorm:"column:disk_bytes"`
}

// Returns ordered samples, aggregated into buckets when bucketSeconds is positive
func (s *Store) GetMetricsHistory(ctx context.Context, serverID string, from, to time.Time, bucketSeconds int) ([]*MetricsSample, error) {
	if bucketSeconds <= 0 {
		var samples []*MetricsSample
		err := s.db.WithContext(ctx).
			Where("server_id = ? AND datetime(timestamp) >= datetime(?) AND datetime(timestamp) <= datetime(?)",
				serverID, from.UTC(), to.UTC()).
			Order("datetime(timestamp) ASC").
			Find(&samples).Error
		return samples, err
	}
	query := `
		SELECT server_id,
			(CAST(strftime('%s', timestamp) AS INTEGER) / ?) * ? AS bucket,
			AVG(tps) AS tps, AVG(mspt) AS mspt, MAX(players) AS players,
			AVG(cpu_percent) AS cpu_percent, AVG(memory_mb) AS memory_mb,
			AVG(heap_used_mb) AS heap_used_mb, MAX(disk_bytes) AS disk_bytes
		FROM metrics_samples
		WHERE server_id = ? AND datetime(timestamp) >= datetime(?) AND datetime(timestamp) <= datetime(?)
		GROUP BY bucket
		ORDER BY bucket ASC`
	var rows []metricsBucketRow
	err := s.db.WithContext(ctx).
		Raw(query, bucketSeconds, bucketSeconds, serverID, from.UTC(), to.UTC()).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	samples := make([]*MetricsSample, 0, len(rows))
	for _, r := range rows {
		samples = append(samples, &MetricsSample{
			ServerID:   r.ServerID,
			Resolution: bucketSeconds,
			Timestamp:  time.Unix(r.Bucket, 0).UTC(),
			TPS:        r.TPS,
			MSPT:       r.MSPT,
			Players:    r.Players,
			CPUPercent: r.CPUPercent,
			MemoryMB:   r.MemoryMB,
			HeapUsedMB: r.HeapUsedMB,
			DiskBytes:  r.DiskBytes,
		})
	}
	return samples, nil
}

// Folds raw samples older than cutoff into buckets
func (s *Store) RollupMetricsSamples(ctx context.Context, olderThan time.Time, bucketSeconds int) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		insert := `
			INSERT INTO metrics_samples (server_id, resolution, timestamp, tps, mspt, players, cpu_percent, memory_mb, heap_used_mb, disk_bytes)
			SELECT server_id, ?,
				datetime((CAST(strftime('%s', timestamp) AS INTEGER) / ?) * ?, 'unixepoch'),
				AVG(tps), AVG(mspt), MAX(players), AVG(cpu_percent), AVG(memory_mb), AVG(heap_used_mb), MAX(disk_bytes)
			FROM metrics_samples
			WHERE resolution = 0 AND datetime(timestamp) < datetime(?)
			GROUP BY server_id, CAST(strftime('%s', timestamp) AS INTEGER) / ?`
		if err := tx.Exec(insert, bucketSeconds, bucketSeconds, bucketSeconds, olderThan.UTC(), bucketSeconds).Error; err != nil {
			return err
		}
		return tx.Where("resolution = 0 AND datetime(timestamp) < datetime(?)", olderThan.UTC()).
			Delete(&MetricsSample{}).Error
	})
}

// Removes samples of one resolution older than the cutoff
func (s *Store) PruneMetricsSamples(ctx context.Context, resolution int, olderThan time.Time) error {
	return s.db.WithContext(ctx).
		Where("resolution = ? AND datetime(timestamp) < datetime(?)", resolution, olderThan.UTC()).
		Delete(&MetricsSample{}).Error
}

func (s *Store) GetServerByPort(ctx context.Context, port int) (*Server, error) {
	var server Server
	// Skip servers with a proxy hostname, they bind indirectly
	err := s.db.WithContext(ctx).Where("port = ? AND (proxy_hostname IS NULL OR proxy_hostname = '')", port).First(&server).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &server, nil
}

// Server properties operations
func (s *Store) GetServerProperties(ctx context.Context, serverID string) (*ServerProperties, error) {
	var config ServerProperties
	err := s.db.WithContext(ctx).Where("server_id = ?", serverID).First(&config).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("server properties not found: %w", err)
		}
		return nil, err
	}
	return &config, nil
}

func (s *Store) UpdateServerProperties(ctx context.Context, config *ServerProperties) error {
	return s.db.WithContext(ctx).Save(config).Error
}

func (s *Store) SaveServerProperties(ctx context.Context, config *ServerProperties) error {
	return s.db.WithContext(ctx).Save(config).Error
}

// Clears all ephemeral property fields
func (s *Store) ClearEphemeralPropertyFields(ctx context.Context, serverID string) error {
	config, err := s.GetServerProperties(ctx, serverID)
	if err != nil {
		return err
	}

	// Clear ephemeral fields
	config.ForceProvision = nil

	return s.SaveServerProperties(ctx, config)
}

// Syncs system fields in ServerProperties from Server settings
func (s *Store) SyncServerPropertiesWithServer(ctx context.Context, server *Server) error {
	// Get or create config
	config, err := s.GetServerProperties(ctx, server.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			config = s.CreateDefaultServerProperties(server.ID)
		} else {
			return err
		}
	}

	intPtr := func(i int) *int { return &i }
	config.ServerPort = intPtr(server.Port)
	config.MaxPlayers = intPtr(server.MaxPlayers)
	config.SyncMemoryFromServer(server)

	return s.SaveServerProperties(ctx, config)
}

func (s *Store) CreateDefaultServerProperties(serverID string) *ServerProperties {
	boolPtr := func(b bool) *bool { return &b }
	stringPtr := func(s string) *string { return &s }
	intPtr := func(i int) *int { return &i }

	// Start with basic defaults
	rconPassword := "discopanel_default"
	if serverID != "" && len(serverID) >= 8 {
		rconPassword = fmt.Sprintf("discopanel_%s", serverID[:8])
	}

	config := &ServerProperties{
		ID:           serverID + "-config",
		ServerID:     serverID,
		EULA:         stringPtr("TRUE"),
		EnableRCON:   boolPtr(true),
		RCONPassword: stringPtr(rconPassword),
		RCONPort:     intPtr(25575),
		Difficulty:   stringPtr("easy"),
		Mode:         stringPtr("survival"),
		MaxPlayers:   intPtr(20),
	}

	// Skip global settings lookup when creating global settings
	if serverID == GlobalSettingsID {
		return config
	}

	// Get global settings and copy non-nil values
	var globalSettings ServerProperties
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
				field.Name == "InitMemory" || field.Name == "MaxMemory" || field.Name == "ServerPort" ||
				field.Name == "MaxPlayers" {
				continue
			}

			globalField := globalValue.FieldByName(field.Name)
			if globalField.IsValid() && globalField.Kind() == reflect.Pointer && !globalField.IsNil() {
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

func (s *Store) GetModpackBySlug(ctx context.Context, slug string) (*IndexedModpack, error) {
	var modpack IndexedModpack
	err := s.db.WithContext(ctx).Where("slug = ?", slug).First(&modpack).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &modpack, nil
}

func (s *Store) GetModpackByWebsiteURL(ctx context.Context, url string) (*IndexedModpack, error) {
	var modpack IndexedModpack
	err := s.db.WithContext(ctx).Where("website_url = ?", url).First(&modpack).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
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

// Checks if any servers are using the specified modpack
func (s *Store) CheckModpackInUse(ctx context.Context, modpackID string) ([]*Server, error) {
	var servers []*Server
	var configs []*ServerProperties

	// For manual modpacks, the CFSlug is set to "manual-{modpackID}"
	cfSlug := "manual-" + modpackID

	// Find all configs that reference this modpack
	if err := s.db.WithContext(ctx).Where("cf_slug = ?", cfSlug).Find(&configs).Error; err != nil {
		return nil, err
	}

	// Get the associated servers
	if len(configs) > 0 {
		serverIDs := make([]string, 0, len(configs))
		for _, config := range configs {
			serverIDs = append(serverIDs, config.ServerID)
		}
		if err := s.db.WithContext(ctx).Where("id IN ?", serverIDs).Find(&servers).Error; err != nil {
			return nil, err
		}
	}

	return servers, nil
}

// Deletes a modpack and all related records
func (s *Store) DeleteIndexedModpack(ctx context.Context, modpackID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete modpack files
		if err := tx.Where("modpack_id = ?", modpackID).Delete(&IndexedModpackFile{}).Error; err != nil {
			return err
		}

		// Delete favorites
		if err := tx.Where("modpack_id = ?", modpackID).Delete(&ModpackFavorite{}).Error; err != nil {
			return err
		}

		// Delete the modpack itself
		return tx.Delete(&IndexedModpack{}, "id = ?", modpackID).Error
	})
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

// Global Settings operations (using ServerProperties with a special ID)
const GlobalSettingsID = "global-settings"

func (s *Store) GetGlobalSettings(ctx context.Context) (*ServerProperties, bool, error) {
	var config ServerProperties
	isNew := false
	err := s.db.WithContext(ctx).Where("id = ?", GlobalSettingsID).First(&config).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// Create empty global settings, no defaults
			config = ServerProperties{
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

func (s *Store) UpdateGlobalSettings(ctx context.Context, config *ServerProperties) error {
	config.ID = GlobalSettingsID
	config.ServerID = GlobalSettingsID
	return s.db.WithContext(ctx).Save(config).Error
}

func (s *Store) SeedGlobalSettings() error {
	ctx := context.Background()
	_, isNew, err := s.GetGlobalSettings(ctx)
	if err != nil {
		return err
	}
	if isNew || s.cfg.Minecraft.ResetGlobal {
		gc := s.CreateDefaultServerProperties(GlobalSettingsID)
		if len(s.cfg.Minecraft.GlobalConfig) > 0 {
			mapstructure.WeakDecode(s.cfg.Minecraft.GlobalConfig, gc)
			gc.ID = GlobalSettingsID + "-config"
			gc.ServerID = GlobalSettingsID
		}
		return s.UpdateGlobalSettings(ctx, gc)
	}
	return nil
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

// Finds first available port for a proxy listener
func (s *Store) FindAvailableListenerPort(ctx context.Context) (int, error) {
	const startPort = 25565
	const maxPort = 65535

	// Get existing proxy listeners
	listeners, err := s.GetProxyListeners(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get proxy listeners: %w", err)
	}

	// Get all servers
	servers, err := s.ListServers(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list servers: %w", err)
	}

	// Build set of used ports from listeners and non-proxied servers
	usedPorts := make(map[int]bool)
	for _, l := range listeners {
		usedPorts[l.Port] = true
	}
	for _, srv := range servers {
		if srv.ProxyHostname == "" && srv.Port > 0 {
			usedPorts[srv.Port] = true
		}
	}

	// Find first available port
	for port := startPort; port <= maxPort; port++ {
		if usedPorts[port] {
			continue
		}

		// Checks for a non-proxied module already bound to this port
		conflict, err := s.CheckPortAvailability(ctx, port, "tcp", false, "", "")
		if err != nil {
			return 0, fmt.Errorf("failed to check port availability: %w", err)
		}
		if conflict != nil {
			continue
		}

		return port, nil
	}

	return 0, fmt.Errorf("no available ports found starting from %d", startPort)
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

func (s *Store) GetUserByUsernameAndProvider(ctx context.Context, username, provider string) (*User, error) {
	var user User
	err := s.db.WithContext(ctx).First(&user, "username = ? AND auth_provider = ?", username, provider).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("user not found")
		}
		return nil, err
	}
	return &user, nil
}

func (s *Store) GetUserByOIDCSubject(ctx context.Context, subject string) (*User, error) {
	var user User
	err := s.db.WithContext(ctx).First(&user, "oidc_subject = ?", subject).Error
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
		if err := tx.Where("user_id = ?", id).Delete(&Session{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&APIToken{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&UserRole{}).Error; err != nil {
			return err
		}
		return tx.Delete(&User{}, "id = ?", id).Error
	})
}

func (s *Store) CountUsers(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&User{}).Count(&count).Error
	return count, err
}

// Role operations
func (s *Store) SeedSystemRoles() error {
	roles := []Role{
		{ID: "role-admin", Name: "admin", Description: "Full system access", IsSystem: true},
		{ID: "role-user", Name: "user", Description: "Standard user access", IsSystem: true, IsDefault: true},
		{ID: "role-anonymous", Name: "anonymous", Description: "Unauthenticated user access", IsSystem: true},
	}
	for _, role := range roles {
		var existing Role
		if err := s.db.Where("name = ?", role.Name).First(&existing).Error; err == gorm.ErrRecordNotFound {
			if err := s.db.Create(&role).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) CreateRole(ctx context.Context, role *Role) error {
	if role.ID == "" {
		role.ID = uuid.New().String()
	}
	return s.db.WithContext(ctx).Create(role).Error
}

func (s *Store) GetRole(ctx context.Context, id string) (*Role, error) {
	var role Role
	err := s.db.WithContext(ctx).First(&role, "id = ?", id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("role not found")
		}
		return nil, err
	}
	return &role, nil
}

func (s *Store) ListRoles(ctx context.Context) ([]*Role, error) {
	var roles []*Role
	err := s.db.WithContext(ctx).Order("is_system DESC, name ASC").Find(&roles).Error
	return roles, err
}

func (s *Store) GetDefaultRoles(ctx context.Context) ([]*Role, error) {
	var roles []*Role
	err := s.db.WithContext(ctx).Where("is_default = ?", true).Find(&roles).Error
	return roles, err
}

func (s *Store) UpdateRole(ctx context.Context, role *Role) error {
	return s.db.WithContext(ctx).Save(role).Error
}

func (s *Store) DeleteRole(ctx context.Context, id string) error {
	var role Role
	if err := s.db.WithContext(ctx).First(&role, "id = ?", id).Error; err != nil {
		return err
	}
	if role.IsSystem {
		return fmt.Errorf("cannot delete system role")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_name = ?", role.Name).Delete(&UserRole{}).Error; err != nil {
			return err
		}
		return tx.Delete(&Role{}, "id = ?", id).Error
	})
}

// UserRole operations
func (s *Store) AssignRole(ctx context.Context, userID, roleName, source string) error {
	var existing UserRole
	err := s.db.WithContext(ctx).Where("user_id = ? AND role_name = ?", userID, roleName).First(&existing).Error
	if err == nil {
		return nil // Already assigned
	}
	ur := &UserRole{
		ID:       uuid.New().String(),
		UserID:   userID,
		RoleName: roleName,
		Source:   source,
	}
	return s.db.WithContext(ctx).Create(ur).Error
}

func (s *Store) UnassignRole(ctx context.Context, userID, roleName string) error {
	return s.db.WithContext(ctx).Where("user_id = ? AND role_name = ?", userID, roleName).Delete(&UserRole{}).Error
}

func (s *Store) GetUserRoleNames(ctx context.Context, userID string) ([]string, error) {
	var names []string
	err := s.db.WithContext(ctx).
		Model(&UserRole{}).
		Select("user_roles.role_name").
		Joins("LEFT JOIN roles ON roles.name = user_roles.role_name").
		Where("user_roles.user_id = ?", userID).
		Order("roles.is_system DESC, roles.name ASC").
		Pluck("user_roles.role_name", &names).Error
	if err != nil {
		return nil, err
	}
	return names, nil
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

func (s *Store) CleanExpiredSessions(ctx context.Context) error {
	return s.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&Session{}).Error
}

// Deletes all sessions, used when JWT secret changes
func (s *Store) CleanAllSessions(ctx context.Context) error {
	return s.db.WithContext(ctx).Where("1 = 1").Delete(&Session{}).Error
}

// Deletes all sessions, tokens, roles, invites, and users
func (s *Store) ResetAllUsers(ctx context.Context) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&Session{}).Error; err != nil {
			return fmt.Errorf("failed to delete sessions: %w", err)
		}
		if err := tx.Where("1 = 1").Delete(&APIToken{}).Error; err != nil {
			return fmt.Errorf("failed to delete api tokens: %w", err)
		}
		if err := tx.Where("1 = 1").Delete(&UserRole{}).Error; err != nil {
			return fmt.Errorf("failed to delete user roles: %w", err)
		}
		if err := tx.Where("1 = 1").Delete(&RegistrationInvite{}).Error; err != nil {
			return fmt.Errorf("failed to delete registration invites: %w", err)
		}
		if err := tx.Where("1 = 1").Delete(&User{}).Error; err != nil {
			return fmt.Errorf("failed to delete users: %w", err)
		}
		return nil
	})
}

// APIToken operations
func (s *Store) CreateAPIToken(ctx context.Context, token *APIToken) error {
	if token.ID == "" {
		token.ID = uuid.New().String()
	}
	return s.db.WithContext(ctx).Create(token).Error
}

func (s *Store) GetAPITokenByHash(ctx context.Context, hash string) (*APIToken, error) {
	var token APIToken
	err := s.db.WithContext(ctx).Where("token_hash = ?", hash).First(&token).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("api token not found")
		}
		return nil, err
	}
	return &token, nil
}

func (s *Store) ListAPITokensByUser(ctx context.Context, userID string) ([]APIToken, error) {
	var tokens []APIToken
	err := s.db.WithContext(ctx).Where("user_id = ? AND (is_module_token = ? OR is_module_token IS NULL)", userID, false).Order("created_at DESC").Find(&tokens).Error
	return tokens, err
}

func (s *Store) DeleteAPIToken(ctx context.Context, id, userID string) error {
	result := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&APIToken{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("api token not found")
	}
	return nil
}

func (s *Store) DeleteAPITokenByID(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Where("id = ?", id).Delete(&APIToken{}).Error
}

func (s *Store) UpdateAPITokenLastUsed(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Model(&APIToken{}).Where("id = ?", id).Update("last_used_at", time.Now()).Error
}

// RegistrationInvite operations
func (s *Store) CreateRegistrationInvite(ctx context.Context, invite *RegistrationInvite) error {
	if invite.ID == "" {
		invite.ID = uuid.New().String()
	}
	return s.db.WithContext(ctx).Create(invite).Error
}

func (s *Store) GetRegistrationInvite(ctx context.Context, id string) (*RegistrationInvite, error) {
	var invite RegistrationInvite
	err := s.db.WithContext(ctx).First(&invite, "id = ?", id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("invite not found")
		}
		return nil, err
	}
	return &invite, nil
}

func (s *Store) GetRegistrationInviteByCode(ctx context.Context, code string) (*RegistrationInvite, error) {
	var invite RegistrationInvite
	err := s.db.WithContext(ctx).First(&invite, "code = ?", code).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("invite not found")
		}
		return nil, err
	}
	return &invite, nil
}

func (s *Store) ListRegistrationInvites(ctx context.Context) ([]*RegistrationInvite, error) {
	var invites []*RegistrationInvite
	err := s.db.WithContext(ctx).Order("created_at DESC").Find(&invites).Error
	return invites, err
}

func (s *Store) IncrementInviteUseCount(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Model(&RegistrationInvite{}).Where("id = ?", id).
		Update("use_count", gorm.Expr("use_count + 1")).Error
}

func (s *Store) DeleteRegistrationInvite(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&RegistrationInvite{}, "id = ?", id).Error
}

// SystemSetting operations

func (s *Store) GetSystemSetting(ctx context.Context, key string) (string, error) {
	var setting SystemSetting
	err := s.db.WithContext(ctx).First(&setting, "key = ?", key).Error
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (s *Store) SetSystemSetting(ctx context.Context, key, value string) error {
	setting := SystemSetting{Key: key, Value: value}
	return s.db.WithContext(ctx).Save(&setting).Error
}

// ScheduledTask operations
func (s *Store) CreateScheduledTask(ctx context.Context, task *ScheduledTask) error {
	if task.ID == "" {
		task.ID = uuid.New().String()
	}
	return s.db.WithContext(ctx).Create(task).Error
}

func (s *Store) GetScheduledTask(ctx context.Context, id string) (*ScheduledTask, error) {
	var task ScheduledTask
	err := s.db.WithContext(ctx).First(&task, "id = ?", id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("scheduled task not found")
		}
		return nil, err
	}
	return &task, nil
}

func (s *Store) ListScheduledTasks(ctx context.Context, serverID string) ([]*ScheduledTask, error) {
	var tasks []*ScheduledTask
	query := s.db.WithContext(ctx)
	if serverID != "" {
		query = query.Where("server_id = ?", serverID)
	}
	err := query.Order("created_at DESC").Find(&tasks).Error
	return tasks, err
}

func (s *Store) ListAllScheduledTasks(ctx context.Context) ([]*ScheduledTask, error) {
	var tasks []*ScheduledTask
	err := s.db.WithContext(ctx).Order("next_run ASC NULLS LAST").Find(&tasks).Error
	return tasks, err
}

func (s *Store) ListDueScheduledTasks(ctx context.Context, before time.Time) ([]*ScheduledTask, error) {
	var tasks []*ScheduledTask
	err := s.db.WithContext(ctx).
		Where("status = ? AND next_run IS NOT NULL AND next_run <= ?", TaskStatusEnabled, before).
		Order("next_run ASC").
		Find(&tasks).Error
	return tasks, err
}

func (s *Store) UpdateScheduledTask(ctx context.Context, task *ScheduledTask) error {
	return s.db.WithContext(ctx).Save(task).Error
}

func (s *Store) DeleteScheduledTask(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete executions first
		if err := tx.Where("task_id = ?", id).Delete(&TaskExecution{}).Error; err != nil {
			return err
		}
		// Delete task
		return tx.Delete(&ScheduledTask{}, "id = ?", id).Error
	})
}

func (s *Store) UpdateTaskNextRun(ctx context.Context, taskID string, nextRun *time.Time, lastRun *time.Time) error {
	updates := map[string]interface{}{
		"next_run": nextRun,
	}
	if lastRun != nil {
		updates["last_run"] = lastRun
	}
	return s.db.WithContext(ctx).Model(&ScheduledTask{}).Where("id = ?", taskID).Updates(updates).Error
}

// TaskExecution operations
func (s *Store) CreateTaskExecution(ctx context.Context, execution *TaskExecution) error {
	if execution.ID == "" {
		execution.ID = uuid.New().String()
	}
	return s.db.WithContext(ctx).Create(execution).Error
}

func (s *Store) GetTaskExecution(ctx context.Context, id string) (*TaskExecution, error) {
	var execution TaskExecution
	err := s.db.WithContext(ctx).First(&execution, "id = ?", id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("task execution not found")
		}
		return nil, err
	}
	return &execution, nil
}

func (s *Store) ListTaskExecutions(ctx context.Context, taskID string, limit int) ([]*TaskExecution, error) {
	var executions []*TaskExecution
	query := s.db.WithContext(ctx).Where("task_id = ?", taskID).Order("started_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&executions).Error
	return executions, err
}

func (s *Store) ListServerTaskExecutions(ctx context.Context, serverID string, limit int) ([]*TaskExecution, error) {
	var executions []*TaskExecution
	query := s.db.WithContext(ctx).Where("server_id = ?", serverID).Order("started_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&executions).Error
	return executions, err
}

func (s *Store) UpdateTaskExecution(ctx context.Context, execution *TaskExecution) error {
	return s.db.WithContext(ctx).Save(execution).Error
}

func (s *Store) DeleteTaskExecutions(ctx context.Context, taskID string) error {
	return s.db.WithContext(ctx).Where("task_id = ?", taskID).Delete(&TaskExecution{}).Error
}

func (s *Store) CleanOldTaskExecutions(ctx context.Context, olderThan time.Time, keepMinimum int) error {
	// Get all task IDs
	var taskIDs []string
	if err := s.db.WithContext(ctx).Model(&ScheduledTask{}).Pluck("id", &taskIDs).Error; err != nil {
		return err
	}

	for _, taskID := range taskIDs {
		// Count total executions for this task
		var count int64
		if err := s.db.WithContext(ctx).Model(&TaskExecution{}).Where("task_id = ?", taskID).Count(&count).Error; err != nil {
			continue
		}

		// Only delete when count exceeds the minimum to keep
		if count > int64(keepMinimum) {
			// Finds IDs of the most recent executions to keep
			var keepIDs []string
			s.db.WithContext(ctx).Model(&TaskExecution{}).
				Where("task_id = ?", taskID).
				Order("started_at DESC").
				Limit(keepMinimum).
				Pluck("id", &keepIDs)

			// Delete old executions that are not in the keep list
			s.db.WithContext(ctx).
				Where("task_id = ? AND started_at < ? AND id NOT IN ?", taskID, olderThan, keepIDs).
				Delete(&TaskExecution{})
		}
	}
	return nil
}

// ModuleTemplate operations
func (s *Store) CreateModuleTemplate(ctx context.Context, template *ModuleTemplate) error {
	if template.ID == "" {
		template.ID = uuid.New().String()
	}
	return s.db.WithContext(ctx).Create(template).Error
}

func (s *Store) GetModuleTemplate(ctx context.Context, id string) (*ModuleTemplate, error) {
	var template ModuleTemplate
	err := s.db.WithContext(ctx).First(&template, "id = ?", id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("module template not found")
		}
		return nil, err
	}
	return &template, nil
}

func (s *Store) GetModuleTemplateByName(ctx context.Context, name string) (*ModuleTemplate, error) {
	var template ModuleTemplate
	err := s.db.WithContext(ctx).First(&template, "name = ?", name).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("module template not found")
		}
		return nil, err
	}
	return &template, nil
}

func (s *Store) ListModuleTemplates(ctx context.Context) ([]*ModuleTemplate, error) {
	var templates []*ModuleTemplate
	err := s.db.WithContext(ctx).Order("type ASC, name ASC").Find(&templates).Error
	return templates, err
}

func (s *Store) ListBuiltinModuleTemplates(ctx context.Context) ([]*ModuleTemplate, error) {
	var templates []*ModuleTemplate
	err := s.db.WithContext(ctx).Where("type = ?", ModuleTemplateTypeBuiltin).Order("name ASC").Find(&templates).Error
	return templates, err
}

func (s *Store) UpdateModuleTemplate(ctx context.Context, template *ModuleTemplate) error {
	return s.db.WithContext(ctx).Save(template).Error
}

func (s *Store) DeleteModuleTemplate(ctx context.Context, id string) error {
	// Check if any modules use this template
	var count int64
	s.db.Model(&Module{}).Where("template_id = ?", id).Count(&count)
	if count > 0 {
		return fmt.Errorf("cannot delete template: %d modules are using it", count)
	}
	return s.db.WithContext(ctx).Delete(&ModuleTemplate{}, "id = ?", id).Error
}

// Creates or updates a module template by ID
func (s *Store) UpsertModuleTemplate(ctx context.Context, template *ModuleTemplate) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing ModuleTemplate
		err := tx.Where("id = ?", template.ID).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			return tx.Create(template).Error
		}
		if err != nil {
			return err
		}
		// Preserve created_at when updating
		template.CreatedAt = existing.CreatedAt
		// Selects all columns to update the JSON-serialized fields too
		return tx.Model(&existing).Select("*").Omit("created_at").Updates(template).Error
	})
}

// Module operations
func (s *Store) CreateModule(ctx context.Context, module *Module) error {
	if module.ID == "" {
		module.ID = uuid.New().String()
	}
	return s.db.WithContext(ctx).Create(module).Error
}

func (s *Store) GetModule(ctx context.Context, id string) (*Module, error) {
	var module Module
	err := s.db.WithContext(ctx).First(&module, "id = ?", id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("module not found")
		}
		return nil, err
	}
	return &module, nil
}

func (s *Store) ListModules(ctx context.Context) ([]*Module, error) {
	var modules []*Module
	err := s.db.WithContext(ctx).Order("created_at DESC").Find(&modules).Error
	return modules, err
}

func (s *Store) ListServerModules(ctx context.Context, serverID string) ([]*Module, error) {
	var modules []*Module
	err := s.db.WithContext(ctx).Where("server_id = ?", serverID).Order("name ASC").Find(&modules).Error
	return modules, err
}

func (s *Store) ListModulesByTemplate(ctx context.Context, templateID string) ([]*Module, error) {
	var modules []*Module
	err := s.db.WithContext(ctx).Where("template_id = ?", templateID).Order("created_at DESC").Find(&modules).Error
	return modules, err
}

func (s *Store) UpdateModule(ctx context.Context, module *Module) error {
	return s.db.WithContext(ctx).Save(module).Error
}

func (s *Store) DeleteModule(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&Module{}, "id = ?", id).Error
}

// Finds a module that uses the specified host port
func (s *Store) GetModuleByHostPort(ctx context.Context, port int) (*Module, error) {
	var modules []*Module
	if err := s.db.WithContext(ctx).Find(&modules).Error; err != nil {
		return nil, err
	}

	for _, module := range modules {
		for _, p := range module.Ports {
			if p != nil && int(p.HostPort) == port {
				return module, nil
			}
		}
	}

	return nil, nil
}

// Describes a port conflict between modules
type PortConflict struct {
	Module   *Module
	Port     int
	Protocol string
	Reason   string
}

// Checks port availability across proxied and non-proxied modules
func (s *Store) CheckPortAvailability(ctx context.Context, hostPort int, protocol string, proxyEnabled bool, hostname string, excludeModuleID string) (*PortConflict, error) {
	var modules []*Module
	if err := s.db.WithContext(ctx).Find(&modules).Error; err != nil {
		return nil, err
	}

	for _, module := range modules {
		if module.ID == excludeModuleID {
			continue
		}

		for _, p := range module.Ports {
			if p == nil || int(p.HostPort) != hostPort {
				continue
			}

			existingProtocol := p.Protocol
			if existingProtocol == "" {
				existingProtocol = "tcp"
			}

			// TCP and UDP use separate port spaces
			if existingProtocol != protocol {
				continue
			}

			// Non-proxied ports bind directly to host
			if !proxyEnabled || !p.ProxyEnabled {
				return &PortConflict{
					Module:   module,
					Port:     hostPort,
					Protocol: protocol,
					Reason:   "port is bound directly to host by another module",
				}, nil
			}

			// Proxied UDP is exclusive, no hostname routing
			if protocol == "udp" {
				return &PortConflict{
					Module:   module,
					Port:     hostPort,
					Protocol: protocol,
					Reason:   "UDP proxy port already in use",
				}, nil
			}

			// Proxied TCP allows hostname-based routing, no conflict here
		}
	}

	return nil, nil
}

func (s *Store) GetModuleByContainerID(ctx context.Context, containerID string) (*Module, error) {
	var module Module
	err := s.db.WithContext(ctx).Where("container_id = ?", containerID).First(&module).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &module, nil
}

func (s *Store) ListAutoStartModules(ctx context.Context) ([]*Module, error) {
	var modules []*Module
	err := s.db.WithContext(ctx).Where("auto_start = ? AND detached = ?", true, false).Find(&modules).Error
	return modules, err
}

func (s *Store) ListModulesFollowingServerLifecycle(ctx context.Context, serverID string) ([]*Module, error) {
	var modules []*Module
	err := s.db.WithContext(ctx).Where("server_id = ? AND follow_server_lifecycle = ?", serverID, true).Find(&modules).Error
	return modules, err
}

// Returns enabled tasks subscribed to an event for a server
func (s *Store) ListEventTriggeredTasks(ctx context.Context, serverID string, eventType v1.TriggeredEventType) ([]*ScheduledTask, error) {
	var tasks []*ScheduledTask
	err := s.db.WithContext(ctx).
		Where("server_id = ? AND status = ? AND schedule = ?",
			serverID, TaskStatusEnabled, ScheduleTypeEvent).
		Find(&tasks).Error
	if err != nil {
		return nil, err
	}
	matching := make([]*ScheduledTask, 0, len(tasks))
	for _, t := range tasks {
		for _, e := range t.EventTriggers {
			if e == eventType {
				matching = append(matching, t)
				break
			}
		}
	}
	return matching, nil
}

// Keeps the per-server ledger bounded
const maxServerActions = 2000

// Appends one action row to the server's ledger
func (s *Store) AppendServerAction(ctx context.Context, action *ServerAction) error {
	if action.Timestamp.IsZero() {
		action.Timestamp = time.Now()
	}
	if err := s.db.WithContext(ctx).Create(action).Error; err != nil {
		return err
	}
	if action.ID%128 == 0 {
		s.pruneServerActions(ctx, action.ServerID)
	}
	return nil
}

func (s *Store) pruneServerActions(ctx context.Context, serverID string) {
	s.db.WithContext(ctx).Exec(
		"DELETE FROM server_actions WHERE server_id = ? AND id NOT IN (SELECT id FROM server_actions WHERE server_id = ? ORDER BY id DESC LIMIT ?)",
		serverID, serverID, maxServerActions)
}

// Returns ledger rows oldest first, after_id pages forward
func (s *Store) GetServerActions(ctx context.Context, serverID string, afterID uint) ([]ServerAction, error) {
	var actions []ServerAction
	q := s.db.WithContext(ctx).Where("server_id = ?", serverID)
	if afterID > 0 {
		q = q.Where("id > ?", afterID)
	}
	err := q.Order("id asc").Limit(maxServerActions).Find(&actions).Error
	return actions, err
}

// Saves or refreshes a finding dismissal
func (s *Store) UpsertFindingDismissal(ctx context.Context, serverID, findingID, contentHash string) error {
	d := &FindingDismissal{
		ServerID:    serverID,
		FindingID:   findingID,
		ContentHash: contentHash,
		DismissedAt: time.Now(),
	}
	return s.db.WithContext(ctx).Save(d).Error
}

func (s *Store) DeleteFindingDismissal(ctx context.Context, serverID, findingID string) error {
	return s.db.WithContext(ctx).
		Where("server_id = ? AND finding_id = ?", serverID, findingID).
		Delete(&FindingDismissal{}).Error
}

// Dismissals for one server keyed by finding id
func (s *Store) GetFindingDismissals(ctx context.Context, serverID string) (map[string]FindingDismissal, error) {
	var rows []FindingDismissal
	if err := s.db.WithContext(ctx).Where("server_id = ?", serverID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]FindingDismissal, len(rows))
	for _, r := range rows {
		out[r.FindingID] = r
	}
	return out, nil
}

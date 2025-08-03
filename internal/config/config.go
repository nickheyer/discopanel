package config

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Database  DatabaseConfig  `mapstructure:"database"`
	Docker    DockerConfig    `mapstructure:"docker"`
	Storage   StorageConfig   `mapstructure:"storage"`
	Minecraft MinecraftConfig `mapstructure:"minecraft"`
	Proxy     ProxyConfig     `mapstructure:"proxy"`
}

type ServerConfig struct {
	Port         string `mapstructure:"port"`
	Host         string `mapstructure:"host"`
	ReadTimeout  int    `mapstructure:"read_timeout"`
	WriteTimeout int    `mapstructure:"write_timeout"`
	IdleTimeout  int    `mapstructure:"idle_timeout"`
}

type DatabaseConfig struct {
	Path            string `mapstructure:"path"`
	MaxConnections  int    `mapstructure:"max_connections"`
	MaxIdleConns    int    `mapstructure:"max_idle_conns"`
	ConnMaxLifetime int    `mapstructure:"conn_max_lifetime"`
}

type DockerConfig struct {
	Host          string `mapstructure:"host"`
	Version       string `mapstructure:"version"`
	NetworkName   string `mapstructure:"network_name"`
	NetworkSubnet string `mapstructure:"network_subnet"`
	RegistryURL   string `mapstructure:"registry_url"`
}

type StorageConfig struct {
	DataDir       string `mapstructure:"data_dir"`
	BackupDir     string `mapstructure:"backup_dir"`
	TempDir       string `mapstructure:"temp_dir"`
	MaxUploadSize int64  `mapstructure:"max_upload_size"`
}

type MinecraftConfig struct {
	DefaultMemory    string            `mapstructure:"default_memory"`
	DefaultMaxMemory string            `mapstructure:"default_max_memory"`
	DefaultPort      int               `mapstructure:"default_port"`
	RconPortStart    int               `mapstructure:"rcon_port_start"`
	Images           map[string]string `mapstructure:"images"`
}

type ProxyConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	BaseURL      string `mapstructure:"base_url"`
	PortRangeMin int    `mapstructure:"port_range_min"`
	PortRangeMax int    `mapstructure:"port_range_max"`
}

func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set config name and type
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	// Add config paths
	if configPath != "" {
		v.AddConfigPath(configPath)
	}
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	v.AddConfigPath("/etc/discopanel")

	// Set defaults
	setDefaults(v)

	// Enable environment variables
	v.SetEnvPrefix("DISCOPANEL")
	v.AutomaticEnv()

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found; use defaults and environment
	}

	// Unmarshal config
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	// Validate and expand paths
	if err := validateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("config validation error: %w", err)
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.port", "8080")
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.read_timeout", 15)
	v.SetDefault("server.write_timeout", 15)
	v.SetDefault("server.idle_timeout", 60)

	// Database defaults
	v.SetDefault("database.path", "./discopanel.db")
	v.SetDefault("database.max_connections", 25)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.conn_max_lifetime", 300)

	// Docker defaults
	v.SetDefault("docker.host", "unix:///var/run/docker.sock")
	v.SetDefault("docker.version", "1.41")
	v.SetDefault("docker.network_name", "discopanel-network")
	v.SetDefault("docker.network_subnet", "172.20.0.0/16")
	v.SetDefault("docker.registry_url", "")

	// Storage defaults
	dataDir, err := filepath.Abs("./data")
	if err != nil {
		panic("Unable to find data dir")
	}
	v.SetDefault("storage.data_dir", dataDir)
	v.SetDefault("storage.backup_dir", "./backups")
	v.SetDefault("storage.temp_dir", "./tmp")
	v.SetDefault("storage.max_upload_size", 524288000) // 500MB

	// Minecraft defaults
	v.SetDefault("minecraft.default_memory", "1G")
	v.SetDefault("minecraft.default_max_memory", "2G")
	v.SetDefault("minecraft.default_port", 25565)
	v.SetDefault("minecraft.rcon_port_start", 25575)
	v.SetDefault("minecraft.images", map[string]string{
		"vanilla": "itzg/minecraft-server:latest",
		"forge":   "itzg/minecraft-server:latest",
		"fabric":  "itzg/minecraft-server:latest",
		"paper":   "itzg/minecraft-server:latest",
		"spigot":  "itzg/minecraft-server:latest",
		"bukkit":  "itzg/minecraft-server:latest",
	})

	// Proxy defaults
	v.SetDefault("proxy.enabled", false)
	v.SetDefault("proxy.base_url", "")
	v.SetDefault("proxy.port_range_min", 25565)
	v.SetDefault("proxy.port_range_max", 25665)
}

func validateConfig(cfg *Config) error {
	// Expand paths to absolute
	var err error
	cfg.Database.Path, err = filepath.Abs(cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("invalid database path: %w", err)
	}

	cfg.Storage.DataDir, err = filepath.Abs(cfg.Storage.DataDir)
	if err != nil {
		return fmt.Errorf("invalid data directory: %w", err)
	}

	cfg.Storage.BackupDir, err = filepath.Abs(cfg.Storage.BackupDir)
	if err != nil {
		return fmt.Errorf("invalid backup directory: %w", err)
	}

	cfg.Storage.TempDir, err = filepath.Abs(cfg.Storage.TempDir)
	if err != nil {
		return fmt.Errorf("invalid temp directory: %w", err)
	}

	// Validate port ranges
	if cfg.Proxy.PortRangeMin >= cfg.Proxy.PortRangeMax {
		return fmt.Errorf("proxy port range min must be less than max")
	}

	return nil
}

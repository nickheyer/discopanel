package minecraft

import (
	"fmt"
	"os"
	"path/filepath"

	models "github.com/RandomTechrate/discopanel-fork/internal/db"
	"gopkg.in/yaml.v3"
)

// ConfigFile represents a generic configuration file type
type ConfigFile string

const (
	ConfigServerProperties ConfigFile = "server.properties"
	ConfigBukkit           ConfigFile = "bukkit.yml"
	ConfigSpigot           ConfigFile = "spigot.yml"
	ConfigPaper            ConfigFile = "paper.yml"
	ConfigPaperGlobal      ConfigFile = "config/paper-global.yml"
	ConfigPaperWorld       ConfigFile = "config/paper-world-defaults.yml"
)

// GetConfigFiles returns the configuration files for a specific mod loader
func GetConfigFiles(loader models.ModLoader) []ConfigFile {
	switch loader {
	case models.ModLoaderVanilla:
		return []ConfigFile{ConfigServerProperties}
	case models.ModLoaderSpigot:
		return []ConfigFile{ConfigServerProperties, ConfigBukkit, ConfigSpigot}
	case models.ModLoaderPaper:
		return []ConfigFile{
			ConfigServerProperties,
			ConfigBukkit,
			ConfigSpigot,
			ConfigPaper,
			ConfigPaperGlobal,
			ConfigPaperWorld,
		}
	case models.ModLoaderForge, models.ModLoaderFabric, models.ModLoaderNeoForge:
		// These typically just use server.properties
		return []ConfigFile{ConfigServerProperties}
	default:
		return []ConfigFile{ConfigServerProperties}
	}
}

// LoadYAMLConfig loads a YAML configuration file
func LoadYAMLConfig(serverDataPath string, configFile ConfigFile) (map[string]any, error) {
	configPath := filepath.Join(serverDataPath, string(configFile))

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config map[string]any
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return config, nil
}

// SaveYAMLConfig saves a YAML configuration file
func SaveYAMLConfig(serverDataPath string, configFile ConfigFile, config map[string]any) error {
	configPath := filepath.Join(serverDataPath, string(configFile))

	// Ensure directory exists
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetAllConfigs returns all configuration files for a server
func GetAllConfigs(serverDataPath string, loader models.ModLoader) (map[string]any, error) {
	configs := make(map[string]any)

	// Always include server.properties
	if props, err := LoadServerProperties(serverDataPath); err == nil {
		configs["server.properties"] = props
	}

	// Load other config files based on mod loader
	configFiles := GetConfigFiles(loader)
	for _, configFile := range configFiles {
		if configFile == ConfigServerProperties {
			continue // Already loaded
		}

		if config, err := LoadYAMLConfig(serverDataPath, configFile); err == nil {
			configs[string(configFile)] = config
		}
	}

	return configs, nil
}

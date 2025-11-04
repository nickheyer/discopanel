package minecraft

import (
	"maps"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	models "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/containers"
)

const (
	// Container images manifest URL from itzg/docker-minecraft-server repo
	containerImagesURL = "https://raw.githubusercontent.com/itzg/docker-minecraft-server/refs/heads/master/images.json"

	// Cache for 1 hour
	imagesCacheDuration = time.Hour
)

type ContainerImageTag struct {
	Tag           string   `json:"tag"`           // Container tag name (e.g., "latest", "java21", etc.)
	Java          string   `json:"java"`          // Java version number
	Distribution  string   `json:"distribution"`  // Linux distribution (ubuntu, alpine, oracle)
	JVM           string   `json:"jvm"`           // JVM type (hotspot, graalvm)
	Architectures []string `json:"architectures"` // Supported architectures
	Deprecated    bool     `json:"deprecated"`    // Whether this tag is deprecated
	LTS           bool     `json:"lts"`           // Whether this is an LTS version
	JDK           bool     `json:"jdk"`           // Whether this includes JDK
	Notes         string   `json:"notes"`         // Additional notes about the tag
}

// Cached container images data
type imagesCache struct {
	mu            sync.RWMutex
	images        []ContainerImageTag
	lastFetchTime time.Time
}

var imageCache = &imagesCache{}

// CreateContainer creates a container for a Minecraft server
func CreateContainer(ctx context.Context, provider containers.ContainerProvider, server *models.Server, serverConfig *models.ServerConfig) (string, error) {
	imageName := "itzg/minecraft-server:latest"
	if server.ContainerImage != "" {
		imageName = "itzg/minecraft-server:" + server.ContainerImage
	}

	// Build environment variables
	env := BuildEnvFromConfig(serverConfig)
	envMap := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	// Override SERVER_PORT when using proxy
	if server.ProxyHostname != "" {
		envMap["SERVER_PORT"] = "25565"
	}

	// Handle path translation
	dataPath := server.DataPath
	if hostDataPath := os.Getenv("DISCOPANEL_HOST_DATA_PATH"); hostDataPath != "" {
		containerDataDir := os.Getenv("DISCOPANEL_DATA_DIR")
		if containerDataDir == "" {
			containerDataDir = "/app/data"
		}
		relPath, err := filepath.Rel(containerDataDir, server.DataPath)
		if err == nil {
			dataPath = filepath.Join(hostDataPath, relPath)
		}
	}

	// Ensure directory exists
	if err := os.MkdirAll(server.DataPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create server data directory: %w", err)
	}

	containerName := fmt.Sprintf("discopanel-server-%s", server.ID)

	// Build mounts
	mounts := []containers.Mount{
		{
			HostPath:      dataPath,
			ContainerPath: "/data",
			ReadOnly:      false,
		},
	}

	// Parse container overrides if present
	type ContainerOverrides struct {
		Environment map[string]string `json:"environment,omitempty"`
		Volumes     []struct {
			Source   string `json:"source"`
			Target   string `json:"target"`
			ReadOnly bool   `json:"read_only,omitempty"`
		} `json:"volumes,omitempty"`
		Command []string `json:"command,omitempty"`
	}

	var overrides ContainerOverrides
	if server.ContainerOverrides != "" {
		if err := json.Unmarshal([]byte(server.ContainerOverrides), &overrides); err == nil {
			// Apply environment overrides
			maps.Copy(envMap, overrides.Environment)

			// Apply additional volumes
			for _, vol := range overrides.Volumes {
				mounts = append(mounts, containers.Mount{
					HostPath:      vol.Source,
					ContainerPath: vol.Target,
					ReadOnly:      vol.ReadOnly,
				})
			}
		}
	}

	// Build configuration options
	opts := []containers.ConfigOption{
		containers.WithMounts(mounts...),
		containers.WithPullConfig(containers.PullIfNotExists),
	}

	if len(overrides.Command) > 0 {
		opts = append(opts, containers.WithCommand(overrides.Command...))
	}

	// Create container config
	cfg := containers.NewContainerConfig(containerName, imageName, envMap, opts...)

	// Create the container
	containerID, err := provider.Create(ctx, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	return containerID, nil
}

// ExecCommand executes a command inside the container and returns the output
func ExecCommand(ctx context.Context, provider containers.ContainerProvider, containerID string, command string) (string, error) {
	return provider.Exec(ctx, containerID, []string{"rcon-cli", command})
}

// MapContainerStatus maps container status string to ServerStatus
func MapContainerStatus(status string) models.ServerStatus {
	switch status {
	case "running":
		return models.StatusRunning
	case "restarting":
		return models.StatusStarting
	case "exited", "dead":
		return models.StatusStopped
	case "created", "paused", "removing":
		return models.StatusStopped
	default:
		return models.StatusError
	}
}

// fetchContainerImages fetches the container images manifest from itzg
func fetchContainerImages() ([]ContainerImageTag, error) {
	// Check cache first
	imageCache.mu.RLock()
	if len(imageCache.images) > 0 && time.Since(imageCache.lastFetchTime) < imagesCacheDuration {
		images := imageCache.images
		imageCache.mu.RUnlock()
		return images, nil
	}
	imageCache.mu.RUnlock()

	// Fetch new manifest
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(containerImagesURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch container images manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch container images manifest: status code %d", resp.StatusCode)
	}

	var images []ContainerImageTag
	if err := json.NewDecoder(resp.Body).Decode(&images); err != nil {
		return nil, fmt.Errorf("failed to decode container images manifest: %w", err)
	}

	// Update cache
	imageCache.mu.Lock()
	imageCache.images = images
	imageCache.lastFetchTime = time.Now()
	imageCache.mu.Unlock()

	return images, nil
}

// GetContainerImages returns the available container image tags for Minecraft servers
func GetContainerImages() []ContainerImageTag {
	images, err := fetchContainerImages()
	if err != nil {
		fmt.Printf("Error: failed to fetch container images: %v\n", err)
		return []ContainerImageTag{}
	}

	// Filter out deprecated images
	var activeImages []ContainerImageTag
	for _, img := range images {
		if !img.Deprecated {
			activeImages = append(activeImages, img)
		}
	}
	return activeImages
}

// GetRequiredJavaVersion gets required Java version for a Minecraft version
func GetRequiredJavaVersion(mcVersion string, modLoader models.ModLoader) string {
	// Fetch the Java version from the Minecraft version metadata
	javaVersion, err := GetJavaVersion(mcVersion)
	if err != nil {
		// If we can't determine the Java version, return 0 to indicate error
		return "0"
	}
	return javaVersion
}

// Gets ideal container tag for a given Minecraft version + mod loader
func GetOptimalImageTag(mcVersion string, modLoader models.ModLoader, preferGraalVM bool) string {
	javaVersion := GetRequiredJavaVersion(mcVersion, modLoader)
	if javaVersion == "0" || javaVersion == "" {
		// Could not determine Java version, use stable
		return "stable"
	}

	// Fetch container images from API
	images, err := fetchContainerImages()
	if err != nil {
		// Could not fetch container images, use stable
		return "stable"
	}

	// Find matching tag
	for _, tag := range images {
		if tag.Java == javaVersion && !tag.Deprecated {
			if preferGraalVM && strings.Contains(tag.Tag, "graalvm") {
				return tag.Tag
			}
			// Return first matching non-special tag (not graalvm, alpine, or jdk)
			if !strings.Contains(tag.Tag, "graalvm") && !strings.Contains(tag.Tag, "alpine") && !strings.Contains(tag.Tag, "jdk") {
				return tag.Tag
			}
		}
	}

	// No matching tag found, construct one
	return fmt.Sprintf("java%s", javaVersion)
}

// BuildEnvFromConfig builds environment variables from ServerConfig struct
func BuildEnvFromConfig(config *models.ServerConfig) []string {
	env := []string{
		"DUMP_SERVER_PROPERTIES=true",
	}

	configValue := reflect.ValueOf(config).Elem()
	configType := configValue.Type()

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		envTag := field.Tag.Get("env")

		if envTag == "" || envTag == "-" {
			continue
		}

		fieldValue := configValue.Field(i)

		if fieldValue.Kind() == reflect.Pointer {
			if fieldValue.IsNil() {
				continue
			}
			fieldValue = fieldValue.Elem()
		}

		switch fieldValue.Kind() {
		case reflect.String:
			if str := fieldValue.String(); str != "" {
				env = append(env, fmt.Sprintf("%s=%s", envTag, str))
			}
		case reflect.Int, reflect.Int32, reflect.Int64:
			env = append(env, fmt.Sprintf("%s=%d", envTag, fieldValue.Int()))
		case reflect.Bool:
			env = append(env, fmt.Sprintf("%s=%v", envTag, fieldValue.Bool()))
		}
	}

	return env
}

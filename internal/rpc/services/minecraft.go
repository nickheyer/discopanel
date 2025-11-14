package services

import (
	"context"

	"connectrpc.com/connect"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/minecraft"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
)

// Compile-time check that MinecraftService implements the interface
var _ discopanelv1connect.MinecraftServiceHandler = (*MinecraftService)(nil)

// MinecraftService implements the Minecraft service
type MinecraftService struct {
	store  *storage.Store
	docker *docker.Client
	log    *logger.Logger
}

// NewMinecraftService creates a new minecraft service
func NewMinecraftService(store *storage.Store, docker *docker.Client, log *logger.Logger) *MinecraftService {
	return &MinecraftService{
		store:  store,
		docker: docker,
		log:    log,
	}
}

// GetMinecraftVersions gets available Minecraft versions
func (s *MinecraftService) GetMinecraftVersions(ctx context.Context, req *connect.Request[v1.GetMinecraftVersionsRequest]) (*connect.Response[v1.GetMinecraftVersionsResponse], error) {
	versions := minecraft.GetVersions()
	latest := minecraft.GetLatestVersion()

	// Convert to protobuf format
	protoVersions := make([]*v1.MinecraftVersion, len(versions))
	for i, version := range versions {
		versionInfo, err := minecraft.GetVersionInfo(version)
		if err != nil {
			// If we can't get info, create a basic version entry
			protoVersions[i] = &v1.MinecraftVersion{
				Id:   version,
				Type: "release",
			}
			continue
		}

		protoVersions[i] = &v1.MinecraftVersion{
			Id:          versionInfo.ID,
			Type:        versionInfo.Type,
			ReleaseTime: versionInfo.ReleaseTime.Format("2006-01-02T15:04:05Z"),
			Url:         versionInfo.URL,
		}
	}

	return connect.NewResponse(&v1.GetMinecraftVersionsResponse{
		Versions: protoVersions,
		Latest:   latest,
	}), nil
}

// GetModLoaders gets available mod loaders
func (s *MinecraftService) GetModLoaders(ctx context.Context, req *connect.Request[v1.GetModLoadersRequest]) (*connect.Response[v1.GetModLoadersResponse], error) {
	modLoaders := minecraft.GetAllModLoaders()

	// Convert to protobuf format
	protoModLoaders := make([]*v1.ModLoaderInfo, len(modLoaders))
	for i, loader := range modLoaders {
		// Determine category and support
		category := determineLoaderCategory(loader.Name)
		supportsMods := len(loader.ModsDirectory) > 0 && loader.ModsDirectory != "plugins"
		supportsPlugins := loader.ModsDirectory == "plugins"

		protoModLoaders[i] = &v1.ModLoaderInfo{
			Name:            loader.Name,
			DisplayName:     loader.DisplayName,
			Description:     getLoaderDescription(loader.Name),
			SupportsMods:    supportsMods,
			SupportsPlugins: supportsPlugins,
			Category:        category,
		}
	}

	return connect.NewResponse(&v1.GetModLoadersResponse{
		Modloaders: protoModLoaders,
	}), nil
}

// GetDockerImages gets available Docker images
func (s *MinecraftService) GetDockerImages(ctx context.Context, req *connect.Request[v1.GetDockerImagesRequest]) (*connect.Response[v1.GetDockerImagesResponse], error) {
	dockerImages := s.docker.GetDockerImages()

	// Convert to protobuf format
	protoImages := make([]*v1.DockerImage, len(dockerImages))
	for i, img := range dockerImages {
		protoImages[i] = &v1.DockerImage{
			Tag:         img.Tag,
			DisplayName: buildImageDisplayName(img),
			Description: buildImageDescription(img),
			Recommended: img.LTS && !img.Deprecated,
		}
	}

	return connect.NewResponse(&v1.GetDockerImagesResponse{
		Images: protoImages,
	}), nil
}

// determineLoaderCategory determines the category of a mod loader
func determineLoaderCategory(name string) string {
	switch name {
	case "VANILLA":
		return "vanilla"
	case "FORGE", "NEOFORGE":
		return "forge"
	case "FABRIC", "QUILT":
		return "fabric"
	case "BUKKIT", "SPIGOT", "PAPER", "PURPUR", "PUFFERFISH":
		return "bukkit"
	case "MAGMA", "MAGMA_MAINTAINED", "KETTING", "MOHIST", "YOUER", "BANNER", "CATSERVER", "ARCLIGHT":
		return "hybrid"
	case "SPONGEVANILLA":
		return "sponge"
	case "AUTO_CURSEFORGE", "CURSEFORGE", "FTBA", "MODRINTH":
		return "modpack"
	default:
		return "other"
	}
}

// getLoaderDescription provides a description for each mod loader
func getLoaderDescription(name string) string {
	descriptions := map[string]string{
		"VANILLA":          "Official Minecraft server without modifications",
		"FORGE":            "Popular mod loader for Minecraft",
		"NEOFORGE":         "Fork of Forge with enhanced features",
		"FABRIC":           "Lightweight mod loader with modern API",
		"QUILT":            "Fork of Fabric with additional features",
		"BUKKIT":           "Classic plugin API for Minecraft servers",
		"SPIGOT":           "Optimized Bukkit fork",
		"PAPER":            "High-performance Spigot fork",
		"PURPUR":           "Extended Paper with additional features",
		"PUFFERFISH":       "Optimized Paper fork",
		"MAGMA":            "Forge + Bukkit hybrid server",
		"MAGMA_MAINTAINED": "Community-maintained Magma fork",
		"KETTING":          "Forge + Bukkit hybrid for modern versions",
		"MOHIST":           "Forge + Bukkit hybrid server",
		"YOUER":            "Forge + Bukkit hybrid server",
		"BANNER":           "Forge + Bukkit hybrid server",
		"CATSERVER":        "Forge + Bukkit hybrid server",
		"ARCLIGHT":         "Forge + Bukkit hybrid server",
		"SPONGEVANILLA":    "Sponge API implementation",
		"LIMBO":            "Lightweight lobby server",
		"NANOLIMBO":        "Ultra-lightweight lobby server",
		"CRUCIBLE":         "Thermos fork with bug fixes",
		"GLOWSTONE":        "Standalone server implementation",
		"CUSTOM":           "Custom server jar",
		"AUTO_CURSEFORGE":  "Automatically install CurseForge modpacks",
		"CURSEFORGE":       "CurseForge modpack loader",
		"FTBA":             "Feed The Beast modpack loader",
		"MODRINTH":         "Modrinth modpack loader",
	}

	if desc, ok := descriptions[name]; ok {
		return desc
	}
	return ""
}

// buildImageDisplayName creates a display name for a Docker image
func buildImageDisplayName(img docker.DockerImageTag) string {
	name := "Java " + img.Java
	if img.JVM == "graalvm" {
		name += " (GraalVM)"
	}
	if img.Distribution == "alpine" {
		name += " Alpine"
	} else if img.Distribution == "oracle" {
		name += " Oracle"
	}
	if img.LTS {
		name += " LTS"
	}
	if img.Deprecated {
		name += " [DEPRECATED]"
	}
	return name
}

// buildImageDescription creates a description for a Docker image
func buildImageDescription(img docker.DockerImageTag) string {
	desc := "Java " + img.Java
	if img.JVM != "" {
		desc += " with " + img.JVM
	}
	if img.Distribution != "" && img.Distribution != "ubuntu" {
		desc += " on " + img.Distribution
	}
	if len(img.Architectures) > 0 {
		desc += " - Supports: "
		for i, arch := range img.Architectures {
			if i > 0 {
				desc += ", "
			}
			desc += arch
		}
	}
	if img.Notes != "" {
		desc += " - " + img.Notes
	}
	return desc
}

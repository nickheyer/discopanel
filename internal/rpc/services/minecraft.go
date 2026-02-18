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
	// Get all versions (includes all types)
	allVersionIDs := minecraft.GetAllVersions()

	// Convert to proto format by fetching info for each version
	versions := make([]*v1.MinecraftVersion, 0, len(allVersionIDs))
	for _, versionID := range allVersionIDs {
		versionInfo, err := minecraft.GetVersionInfo(versionID)
		if err != nil {
			continue // Skip versions we can't get info for
		}

		versions = append(versions, &v1.MinecraftVersion{
			Id:          versionInfo.ID,
			Type:        versionInfo.Type,
			ReleaseTime: versionInfo.ReleaseTime.Format("2006-01-02T15:04:05Z"),
			Url:         versionInfo.URL,
		})
	}

	// Get latest version
	latest := minecraft.GetLatestVersion()

	return connect.NewResponse(&v1.GetMinecraftVersionsResponse{
		Versions: versions,
		Latest:   latest,
	}), nil
}

// GetModLoaders gets available mod loaders
func (s *MinecraftService) GetModLoaders(ctx context.Context, req *connect.Request[v1.GetModLoadersRequest]) (*connect.Response[v1.GetModLoadersResponse], error) {
	// Get all mod loaders from the minecraft package
	modLoaders := minecraft.GetAllModLoaders()

	// Convert to proto format
	protoLoaders := make([]*v1.ModLoaderInfo, 0, len(modLoaders))
	for _, loader := range modLoaders {
		// Determine support capabilities
		supportsMods := loader.ModsDirectory != ""
		supportsPlugins := loader.ModsDirectory == "plugins"

		protoLoaders = append(protoLoaders, &v1.ModLoaderInfo{
			Name:            loader.Name,
			DisplayName:     loader.DisplayName,
			Description:     loader.Description,
			SupportsMods:    supportsMods,
			SupportsPlugins: supportsPlugins,
			Category:        loader.Category,
		})
	}

	return connect.NewResponse(&v1.GetModLoadersResponse{
		Modloaders: protoLoaders,
	}), nil
}

// GetDockerImages gets available Docker images
func (s *MinecraftService) GetDockerImages(ctx context.Context, req *connect.Request[v1.GetDockerImagesRequest]) (*connect.Response[v1.GetDockerImagesResponse], error) {
	// Get Docker images from docker client
	dockerImages := s.docker.GetDockerImages()

	// Convert to proto format
	protoImages := make([]*v1.DockerImage, 0, len(dockerImages))
	for _, img := range dockerImages {
		// Create display name
		displayName := img.Tag
		if img.Java != "" {
			displayName = "Java " + img.Java + ": " + img.JVM
			if img.Distribution != "" {
				displayName += " (" + img.Distribution + ")"
			}

		}

		// Create description
		description := ""
		if img.LTS {
			description = "LTS version"
		}
		if img.JDK {
			if description != "" {
				description += ", "
			}
			description += "includes JDK"
		}
		if img.Notes != "" {
			if description != "" {
				description += ". "
			}
			description += img.Notes
		}

		// Mark recommended images
		recommended := img.LTS && !img.Deprecated

		protoImages = append(protoImages, &v1.DockerImage{
			Tag:         img.Tag,
			DisplayName: displayName,
			Description: description,
			Recommended: recommended,
		})
	}

	return connect.NewResponse(&v1.GetDockerImagesResponse{
		Images: protoImages,
	}), nil
}

// ValidateDockerImage validates a custom Docker image
func (s *MinecraftService) ValidateDockerImage(ctx context.Context, req *connect.Request[v1.ValidateDockerImageRequest]) (*connect.Response[v1.ValidateDockerImageResponse], error) {
	imageName := req.Msg.GetImage()

	normalizedImage, err := s.docker.ValidateImageExists(ctx, imageName)
	if err != nil {
		return connect.NewResponse(&v1.ValidateDockerImageResponse{
			Valid: false,
			Error: err.Error(),
		}), nil
	}

	return connect.NewResponse(&v1.ValidateDockerImageResponse{
		Valid:           true,
		NormalizedImage: normalizedImage,
	}), nil
}

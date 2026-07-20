package services

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/provisioner"
	"github.com/nickheyer/discopanel/pkg/logger"
	"github.com/nickheyer/discopanel/pkg/minecraft"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	"google.golang.org/protobuf/proto"
)

// Compile-time check that MinecraftService implements the interface
var _ discopanelv1connect.MinecraftServiceHandler = (*MinecraftService)(nil)

// Implements the Minecraft service
type MinecraftService struct {
	store  *storage.Store
	docker *docker.Client
	log    *logger.Logger
}

// Creates a new minecraft service
func NewMinecraftService(store *storage.Store, docker *docker.Client, log *logger.Logger) *MinecraftService {
	return &MinecraftService{
		store:  store,
		docker: docker,
		log:    log,
	}
}

// Gets available Minecraft versions
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

// Gets available mod loaders
func (s *MinecraftService) GetModLoaders(ctx context.Context, req *connect.Request[v1.GetModLoadersRequest]) (*connect.Response[v1.GetModLoadersResponse], error) {
	rows := minecraft.Loaders()
	protoLoaders := make([]*v1.ModLoaderInfo, 0, len(rows))
	for _, row := range rows {
		info, _ := proto.Clone(row.Info).(*v1.ModLoaderInfo)
		info.Provisionable = provisioner.HasNativeInstaller(info.Loader)
		protoLoaders = append(protoLoaders, info)
	}

	return connect.NewResponse(&v1.GetModLoadersResponse{
		Modloaders: protoLoaders,
	}), nil
}

// Lists the published discopanel-runtime image variants
func (s *MinecraftService) GetDockerImages(ctx context.Context, req *connect.Request[v1.GetDockerImagesRequest]) (*connect.Response[v1.GetDockerImagesResponse], error) {
	runtimeImages := docker.RuntimeImages()

	protoImages := make([]*v1.DockerImage, 0, len(runtimeImages))
	for i, img := range runtimeImages {
		display := fmt.Sprintf("Java %d (discopanel-runtime)", img.JavaMajor)
		desc := fmt.Sprintf("Minimal Temurin %d JRE runtime; server files are provisioned by DiscoPanel", img.JavaMajor)
		if img.Graal {
			display = fmt.Sprintf("Java %d GraalVM (discopanel-runtime)", img.JavaMajor)
			desc = fmt.Sprintf("Oracle GraalVM %d JIT runtime; often faster ticks on modded servers, worth benchmarking per pack", img.JavaMajor)
		}
		protoImages = append(protoImages, &v1.DockerImage{
			Tag:         img.Tag,
			DisplayName: display,
			Description: desc,
			Recommended: i == 0,
		})
	}

	return connect.NewResponse(&v1.GetDockerImagesResponse{
		Images: protoImages,
	}), nil
}

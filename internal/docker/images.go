package docker

import (
	"fmt"
	"strconv"

	models "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/minecraft"
)

// DefaultRuntimeImage is the discopanel-runtime image repository. A different
// repository (e.g. a private mirror) can be set via docker.runtime_image.
const DefaultRuntimeImage = "nickheyer/discopanel-runtime"

// SupportedJavaVersions lists the Java majors the runtime image is published
// for. Must stay in sync with RUNTIME_JAVA_VERSIONS in the Makefile and the
// build matrix in .github/workflows/modules.yml.
var SupportedJavaVersions = []int{8, 11, 17, 21, 25}

// RequiredJavaMajor resolves the Java major version a Minecraft version needs,
// rounded up to the nearest published runtime image. Mojang metadata is the
// source of truth; unknown versions fall back to the newest runtime.
func RequiredJavaMajor(mcVersion string) int {
	required := 0
	if v, err := minecraft.GetJavaVersion(mcVersion); err == nil {
		required, _ = strconv.Atoi(v)
	}
	if required <= 0 {
		return SupportedJavaVersions[len(SupportedJavaVersions)-1]
	}
	for _, supported := range SupportedJavaVersions {
		if supported >= required {
			return supported
		}
	}
	return SupportedJavaVersions[len(SupportedJavaVersions)-1]
}

// GetRequiredJavaVersion returns the runtime Java major for a Minecraft
// version as a string (stored on the Server row and shown in the UI).
func GetRequiredJavaVersion(mcVersion string, modLoader models.ModLoader) string {
	_ = modLoader // all loaders run on the Mojang-required Java version
	return strconv.Itoa(RequiredJavaMajor(mcVersion))
}

// RuntimeImageTag returns the image tag for a Java major (e.g. "java21").
func RuntimeImageTag(javaMajor int) string {
	return fmt.Sprintf("java%d", javaMajor)
}

// OptimalRuntimeTag returns the runtime image tag for a Minecraft version.
func OptimalRuntimeTag(mcVersion string) string {
	return RuntimeImageTag(RequiredJavaMajor(mcVersion))
}

// runtimeRepository returns the configured runtime image repository.
func (c *Client) runtimeRepository() string {
	if c.config.RuntimeImage != "" {
		return c.config.RuntimeImage
	}
	return DefaultRuntimeImage
}

// RuntimeImage returns the full runtime image reference for a Java major.
func (c *Client) RuntimeImage(javaMajor int) string {
	return c.runtimeRepository() + ":" + RuntimeImageTag(javaMajor)
}

// RuntimeImageForTag returns the full image reference for a stored tag.
func (c *Client) RuntimeImageForTag(tag string) string {
	return c.runtimeRepository() + ":" + tag
}

// DesiredImage returns the runtime image a server should run on: the stored
// tag when explicitly chosen, else derived from the MC version's Java need.
func (c *Client) DesiredImage(server *models.Server) string {
	if server.DockerImage != "" {
		return c.RuntimeImageForTag(server.DockerImage)
	}
	return c.RuntimeImage(RequiredJavaMajor(server.MCVersion))
}

// IsValidRuntimeTag reports whether a stored tag matches a published runtime
// image (guards against stale itzg-era tags like "stable" or "latest").
func IsValidRuntimeTag(tag string) bool {
	for _, v := range SupportedJavaVersions {
		if tag == RuntimeImageTag(v) {
			return true
		}
	}
	return false
}

// RuntimeImageInfo describes one published runtime image variant.
type RuntimeImageInfo struct {
	Tag       string
	JavaMajor int
}

// RuntimeImages lists the published runtime image variants, newest first.
func RuntimeImages() []RuntimeImageInfo {
	images := make([]RuntimeImageInfo, 0, len(SupportedJavaVersions))
	for i := len(SupportedJavaVersions) - 1; i >= 0; i-- {
		v := SupportedJavaVersions[i]
		images = append(images, RuntimeImageInfo{Tag: RuntimeImageTag(v), JavaMajor: v})
	}
	return images
}

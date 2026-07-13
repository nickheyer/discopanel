package docker

import (
	"fmt"
	"strconv"

	models "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/minecraft"
)

// Default discopanel-runtime repo, overridable via docker.runtime_image
const DefaultRuntimeImage = "nickheyer/discopanel-runtime"

// Java majors the runtime image publishes, sync with Makefile
var SupportedJavaVersions = []int{8, 11, 17, 21, 25}

// Majors with a published GraalVM variant, sync with Makefile
var GraalJavaVersions = []int{21, 25}

// Resolves needed Java major, rounded up to nearest published image
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

// Returns the runtime Java major as a string for storage
func GetRequiredJavaVersion(mcVersion string, modLoader models.ModLoader) string {
	_ = modLoader // All loaders run on the Mojang-required Java version
	return strconv.Itoa(RequiredJavaMajor(mcVersion))
}

// Returns the image tag for a Java major
func RuntimeImageTag(javaMajor int) string {
	return fmt.Sprintf("java%d", javaMajor)
}

// Returns the GraalVM variant tag for a Java major
func GraalRuntimeImageTag(javaMajor int) string {
	return fmt.Sprintf("java%d-graal", javaMajor)
}

// Returns the runtime image tag for a Minecraft version
func OptimalRuntimeTag(mcVersion string) string {
	return RuntimeImageTag(RequiredJavaMajor(mcVersion))
}

// Returns the configured runtime image repository
func (c *Client) runtimeRepository() string {
	if c.config.RuntimeImage != "" {
		return c.config.RuntimeImage
	}
	return DefaultRuntimeImage
}

// Returns the full runtime image reference for a Java major
func (c *Client) RuntimeImage(javaMajor int) string {
	return c.runtimeRepository() + ":" + RuntimeImageTag(javaMajor)
}

// Returns the full image reference for a stored tag
func (c *Client) RuntimeImageForTag(tag string) string {
	return c.runtimeRepository() + ":" + tag
}

// Returns the stored tag, else one derived from MC version
func (c *Client) DesiredImage(server *models.Server) string {
	if server.DockerImage != "" {
		return c.RuntimeImageForTag(server.DockerImage)
	}
	return c.RuntimeImage(RequiredJavaMajor(server.MCVersion))
}

// Reports whether a tag matches a published runtime image
func IsValidRuntimeTag(tag string) bool {
	for _, v := range SupportedJavaVersions {
		if tag == RuntimeImageTag(v) {
			return true
		}
	}
	for _, v := range GraalJavaVersions {
		if tag == GraalRuntimeImageTag(v) {
			return true
		}
	}
	return false
}

// Describes one published runtime image variant
type RuntimeImageInfo struct {
	Tag       string
	JavaMajor int
	Graal     bool
}

// Lists runtime image variants, newest first, GraalVM last
func RuntimeImages() []RuntimeImageInfo {
	images := make([]RuntimeImageInfo, 0, len(SupportedJavaVersions)+len(GraalJavaVersions))
	for i := len(SupportedJavaVersions) - 1; i >= 0; i-- {
		v := SupportedJavaVersions[i]
		images = append(images, RuntimeImageInfo{Tag: RuntimeImageTag(v), JavaMajor: v})
	}
	for i := len(GraalJavaVersions) - 1; i >= 0; i-- {
		v := GraalJavaVersions[i]
		images = append(images, RuntimeImageInfo{Tag: GraalRuntimeImageTag(v), JavaMajor: v, Graal: true})
	}
	return images
}

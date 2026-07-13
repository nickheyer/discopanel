package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strconv"

	models "github.com/nickheyer/discopanel/internal/db"
	"google.golang.org/protobuf/proto"
)

// Create-time config fingerprint, compared at start to trigger recreate
const LabelConfigHash = "discopanel.config.hash"

// Fingerprints create-time server inputs, excludes image which drifts separately
func (c *Client) DesiredConfigHash(server *models.Server, serverConfig *models.ServerProperties) string {
	h := sha256.New()
	w := func(parts ...string) {
		for _, p := range parts {
			_, _ = io.WriteString(h, p)
			_, _ = h.Write([]byte{0})
		}
	}

	// V2 added SYS_NICE, cpu shares, local log driver, no tty
	w("v2")
	for _, e := range buildEnvFromConfig(serverConfig) {
		w("env", e)
	}

	useProxy := server.ProxyHostname != ""
	containerPort := server.Port
	if useProxy {
		containerPort = DefaultMinecraftPort
	}
	w("port", strconv.Itoa(server.Port), strconv.Itoa(containerPort), strconv.FormatBool(useProxy))
	for _, p := range server.AdditionalPorts {
		w("extra-port", strconv.Itoa(int(p.GetHostPort())), strconv.Itoa(int(p.GetContainerPort())), p.GetProtocol())
	}

	w("data", TranslateToHostPath(server.DataPath))
	w("memory", strconv.Itoa(server.Memory))
	w("dns", c.config.DNS)

	if server.DockerOverrides != nil {
		if raw, err := (proto.MarshalOptions{Deterministic: true}).Marshal(server.DockerOverrides); err == nil {
			w("overrides")
			_, _ = h.Write(raw)
		}
	}

	return hex.EncodeToString(h.Sum(nil))[:32]
}

// Returns creation-time config hash, empty if predating the label
func (c *Client) ContainerConfigHash(ctx context.Context, containerID string) (string, error) {
	inspect, err := c.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", err
	}
	if inspect.Config == nil {
		return "", nil
	}
	return inspect.Config.Labels[LabelConfigHash], nil
}

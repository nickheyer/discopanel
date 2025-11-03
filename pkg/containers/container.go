package containers

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"
)

// Configuration passed to the ContainerProvider.Create method.
type ContainerConfig struct {
	name             string
	image            string
	env              map[string]string
	command          []string
	mounts           []Mount
	dnsServers       []net.IP
	dnsSearchDomains []string
	extraHosts       []ExtraHost
	pullConfig       ShouldPull
}

type ShouldPull func(img string, exists bool) bool

// Represents a single host path to bind mount in the container
type Mount struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

type ExtraHost struct {
	HostName string
	IP       string
}

// Set the container name, the image to use, and the environment to pass to the container.
func NewContainerConfig(name, image string, env map[string]string, opts ...ConfigOption) *ContainerConfig {
	if env == nil {
		env = map[string]string{}
	}

	cfg := &ContainerConfig{
		name:  name,
		image: image,
		env:   env,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	return cfg
}

// Sets the environment variable given by the key to val for the container.
func (cfg *ContainerConfig) SetEnv(key, val string) {
	cfg.env[key] = val
}

// Adds the given host mount to the container config
func (cfg *ContainerConfig) AddMount(mount Mount) {
	cfg.mounts = append(cfg.mounts, mount)
}

// Adds a DNS server to use.
func (cfg *ContainerConfig) AddDNSServer(server net.IP) {
	cfg.dnsServers = append(cfg.dnsServers, server)
}

// Sets DNS search domain to use.
func (cfg *ContainerConfig) AddDNSSearchDomain(domain string) {
	cfg.dnsSearchDomains = append(cfg.dnsSearchDomains, domain)
}

// AddExtraHost adds an additional host that should be resolvable in the container.
func (cfg *ContainerConfig) AddExtraHost(host ExtraHost) {
	cfg.extraHosts = append(cfg.extraHosts, host)
}

type ConfigOption func(config *ContainerConfig)

// Sets the command to execute in the container
func WithCommand(cmd ...string) ConfigOption {
	return func(config *ContainerConfig) {
		config.command = cmd
	}
}

// Sets the host paths to bind mount in the container
func WithMounts(mounts ...Mount) ConfigOption {
	return func(config *ContainerConfig) {
		config.mounts = mounts
	}
}

// Sets the DNS servers to use.
func WithDNSServers(servers ...net.IP) ConfigOption {
	return func(config *ContainerConfig) {
		config.dnsServers = servers
	}
}

// Sets the DNS search domains to use.
func WithDNSSearchDomains(domains ...string) ConfigOption {
	return func(config *ContainerConfig) {
		config.dnsSearchDomains = domains
	}
}

// WithExtraHosts sets additional hosts that should resolve in the container.
func WithExtraHosts(hosts ...ExtraHost) ConfigOption {
	return func(config *ContainerConfig) {
		config.extraHosts = hosts
	}
}

// Configure the pulling behaviour for this container
func WithPullConfig(shouldPull ShouldPull) ConfigOption {
	return func(config *ContainerConfig) {
		config.pullConfig = shouldPull
	}
}

func PullNever(img string, exists bool) bool {
	return false
}

func PullAlways(img string, exists bool) bool {
	return true
}

func PullIfNotExists(img string, exists bool) bool {
	return !exists
}

var _ ShouldPull = PullNever
var _ ShouldPull = PullAlways
var _ ShouldPull = PullIfNotExists

// A ContainerProvider offers basic control over a container lifecycle
//
// For a standard container lifecycle, use the following pattern:
// 1. call Create()
// 2. defer Remove()
// 3. call Wait()
// 4. call Start()
// 5. defer Stop()
// 6. call Logs(), copy until EOF
// 7. check the Wait() channels for the actual exit code
// 8. call CopyFrom() if required
type ContainerProvider interface {
	// Create a new container with the given ContainerConfig, returns the ID of the container for later use.
	//
	// Note: Currently all containers are started with
	// * Host networking ("--net=host")
	Create(ctx context.Context, cfg *ContainerConfig) (string, error)
	// Remove a container that is not running.
	Remove(ctx context.Context, containerId string) error
	// Start a container that was created previously.
	Start(ctx context.Context, containerID string) error
	// Stop a container. Calling this on a stopped container will return nil.
	Stop(ctx context.Context, containerID string, timeout *time.Duration) error
	// Wait can be used to receive the exit code once the container stops.
	Wait(ctx context.Context, containerID string) (<-chan int64, <-chan error)
	// Logs returns reads for stdout and stderr of a running container
	Logs(ctx context.Context, containerID string) (io.ReadCloser, io.ReadCloser, error)
	// CopyFrom copies files/folders from the container to the local filesystem
	CopyFrom(ctx context.Context, container, sourcePath, destPath string) error
	// Close should be called once this provider is no longer needed.
	Close() error
	// Command returns the command line utility used to control this container provider.
	Command() string
}

var providers = map[string]func(context.Context) (ContainerProvider, error){
	"podman": NewPodmanProvider,
	"docker": NewDockerProvider,
}

// Returns the list of available providers
func Providers() []string {
	keys := make([]string, 0, len(providers))
	for k := range providers {
		keys = append(keys, k)
	}
	return keys
}

// Returns a new, initialized ContainerProvider of the given provider type.
func NewProvider(ctx context.Context, provider string) (ContainerProvider, error) {
	providerFun, ok := providers[provider]
	if !ok {
		return nil, fmt.Errorf("unknown container provider '%s'", provider)
	}
	return providerFun(ctx)
}

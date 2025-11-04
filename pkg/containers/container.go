package containers

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	nat "github.com/docker/go-connections/nat"
)

// ContainerStats represents container resource statistics
type ContainerStats struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryUsage float64 `json:"memory_usage"` // in MB
	MemoryLimit float64 `json:"memory_limit"` // in MB
}

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
	labels           map[string]string
	exposedPorts     nat.PortSet
}

// AdditionalPort represents a single additional port configuration
type AdditionalPort struct {
	Name          string `json:"name"`           // User-friendly name for the port
	ContainerPort int    `json:"container_port"` // Port inside the container
	HostPort      int    `json:"host_port"`      // Port on the host machine
	Protocol      string `json:"protocol"`       // Protocol: "tcp" or "udp" (defaults to "tcp" if empty)
}

// VolumeMount represents a volume mount configuration
type VolumeMount struct {
	Source   string `json:"source"`              // Host path or volume name
	Target   string `json:"target"`              // Container path
	ReadOnly bool   `json:"read_only,omitempty"` // Mount as read-only
	Type     string `json:"type,omitempty"`      // Mount type: "bind" or "volume" (defaults to "bind")
}

// ContainerOverrides represents user-defined container overrides
type ContainerOverrides struct {
	Environment    map[string]string `json:"environment,omitempty"`     // Additional environment variables
	Volumes        []VolumeMount     `json:"volumes,omitempty"`         // Additional volume mounts
	NetworkMode    string            `json:"network_mode,omitempty"`    // Override network mode
	RestartPolicy  string            `json:"restart_policy,omitempty"`  // Override restart policy
	CPULimit       float64           `json:"cpu_limit,omitempty"`       // CPU limit (e.g., 1.5 for 1.5 cores)
	MemoryOverride int64             `json:"memory_override,omitempty"` // Override memory limit in MB
	Labels         map[string]string `json:"labels,omitempty"`          // Additional labels
	CapAdd         []string          `json:"cap_add,omitempty"`         // Linux capabilities to add
	CapDrop        []string          `json:"cap_drop,omitempty"`        // Linux capabilities to drop
	Devices        []string          `json:"devices,omitempty"`         // Device mappings
	ExtraHosts     []string          `json:"extra_hosts,omitempty"`     // Extra entries for /etc/hosts
	Privileged     bool              `json:"privileged,omitempty"`      // Run container in privileged mode
	ReadOnly       bool              `json:"read_only,omitempty"`       // Mount root filesystem as read-only
	SecurityOpt    []string          `json:"security_opt,omitempty"`    // Security options
	ShmSize        int64             `json:"shm_size,omitempty"`        // Size of /dev/shm in bytes
	User           string            `json:"user,omitempty"`            // User to run commands as
	WorkingDir     string            `json:"working_dir,omitempty"`     // Working directory inside container
	Entrypoint     []string          `json:"entrypoint,omitempty"`      // Override default entrypoint
	Command        []string          `json:"command,omitempty"`         // Override default command
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
	// Logs returns reads for stdout and stderr of a running container (combined)
	Logs(ctx context.Context, containerID string) (io.ReadCloser, error)
	// CopyFrom copies files/folders from the container to the local filesystem
	CopyFrom(ctx context.Context, container, sourcePath, destPath string) error
	// EnsureNetwork creates a network if it doesn't exist
	EnsureNetwork(ctx context.Context, networkName string) error
	// GetContainerStatus returns the status of a container
	GetContainerStatus(ctx context.Context, containerID string) (string, error)
	// GetContainerStats returns statistics for a container
	GetContainerStats(ctx context.Context, containerID string) (*ContainerStats, error)
	// CleanupOrphanedContainers removes containers not tracked in the provided map
	CleanupOrphanedContainers(ctx context.Context, trackedIDs map[string]bool) error
	// Exec executes a command inside the container
	Exec(ctx context.Context, containerID string, cmd []string) (string, error)
	// Close should be called once this provider is no longer needed.
	Close() error
	// Command returns the command line utility used to control this container provider.
	Command() string
	// Get IP associated with container
	GetIP(ctx context.Context, containerID string, networkName string) (string, error)
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

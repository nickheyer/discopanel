// Package runtimespec defines the contract between the DiscoPanel provisioner
// (which prepares a server's data directory) and the discopanel-runtime container
// entrypoint (which launches the prepared server). It must remain stdlib-only:
// cmd/runtime is compiled into a minimal container image that imports this package.
package runtimespec

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// StateDir is the directory inside the server data dir that holds
	// provisioner-managed state.
	StateDir = ".discopanel"

	// LaunchFileName is the launch spec consumed by the runtime entrypoint.
	LaunchFileName = "launch.json"

	// ManifestFileName records what the provisioner installed, for idempotency.
	ManifestFileName = "manifest.json"

	// AgentFileName is the agent connection spec consumed by the runtime
	// supervisor: where to reach the panel and how to authenticate.
	AgentFileName = "agent.json"
)

// Launch kinds understood by the runtime entrypoint.
const (
	LaunchKindJar      = "jar"       // java <flags> -jar <Jar> <args> nogui
	LaunchKindArgsFile = "args-file" // java <flags> @<ArgsFile> nogui (modern Forge/NeoForge)
	LaunchKindCustom   = "custom"    // java <flags> <Exec tokens verbatim>
)

// LaunchSpec tells the runtime entrypoint how to start the server process.
// All paths are relative to the server data directory (/data in the container).
type LaunchSpec struct {
	Version   int    `json:"version"`
	Kind      string `json:"kind"`
	Jar       string `json:"jar,omitempty"`
	ArgsFile  string `json:"args_file,omitempty"`
	Exec      string `json:"exec,omitempty"` // whitespace-tokenized for LaunchKindCustom
	NoGui     bool   `json:"no_gui"`
	Loader    string `json:"loader"`
	MCVersion string `json:"mc_version"`
	JavaMajor int    `json:"java_major"`
}

// AgentSpec tells the runtime supervisor how to reach the panel's agent
// endpoint. Written by the panel lifecycle manager before each start; the
// token authenticates exactly one server's telemetry session.
type AgentSpec struct {
	Version  int    `json:"version"`
	Enabled  bool   `json:"enabled"`
	PanelURL string `json:"panel_url"`
	Token    string `json:"token"`
	ServerID string `json:"server_id"`
}

// ModpackRef identifies the modpack a server was provisioned from.
type ModpackRef struct {
	Source    string `json:"source"` // "curseforge" | "modrinth" | "zip"
	ID        string `json:"id"`
	VersionID string `json:"version_id,omitempty"`
}

// Manifest records the provisioned state of a server data directory.
type Manifest struct {
	Version       int         `json:"version"`
	Loader        string      `json:"loader"`
	LoaderVersion string      `json:"loader_version,omitempty"`
	MCVersion     string      `json:"mc_version"`
	JavaMajor     int         `json:"java_major"`
	Modpack       *ModpackRef `json:"modpack,omitempty"`
	ProvisionedAt string      `json:"provisioned_at"`
}

func LaunchPath(dataDir string) string {
	return filepath.Join(dataDir, StateDir, LaunchFileName)
}

func AgentPath(dataDir string) string {
	return filepath.Join(dataDir, StateDir, AgentFileName)
}

// ReadAgentSpec loads the agent spec; returns (nil, nil) when absent.
func ReadAgentSpec(dataDir string) (*AgentSpec, error) {
	data, err := os.ReadFile(AgentPath(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var spec AgentSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("invalid agent spec: %w", err)
	}
	return &spec, nil
}

// WriteAgentSpec persists the agent spec into a server data directory. The
// file carries the agent token, so it is not group/world readable.
func WriteAgentSpec(dataDir string, spec *AgentSpec) error {
	if err := os.MkdirAll(filepath.Join(dataDir, StateDir), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(AgentPath(dataDir), data, 0600)
}

func ManifestPath(dataDir string) string {
	return filepath.Join(dataDir, StateDir, ManifestFileName)
}

// ReadLaunchSpec loads the launch spec from a server data directory.
func ReadLaunchSpec(dataDir string) (*LaunchSpec, error) {
	data, err := os.ReadFile(LaunchPath(dataDir))
	if err != nil {
		return nil, err
	}
	var spec LaunchSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("invalid launch spec: %w", err)
	}
	return &spec, nil
}

// WriteLaunchSpec persists the launch spec into a server data directory.
func WriteLaunchSpec(dataDir string, spec *LaunchSpec) error {
	if err := os.MkdirAll(filepath.Join(dataDir, StateDir), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(LaunchPath(dataDir), data, 0644)
}

// ReadManifest loads the provision manifest; returns (nil, nil) when absent.
func ReadManifest(dataDir string) (*Manifest, error) {
	data, err := os.ReadFile(ManifestPath(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid provision manifest: %w", err)
	}
	return &m, nil
}

// WriteManifest persists the provision manifest into a server data directory.
func WriteManifest(dataDir string, m *Manifest) error {
	if err := os.MkdirAll(filepath.Join(dataDir, StateDir), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ManifestPath(dataDir), data, 0644)
}

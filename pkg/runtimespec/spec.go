// Defines contract between provisioner and runtime entrypoint, stdlib only
package runtimespec

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// Directory inside server data dir holding provisioner state
	StateDir = ".discopanel"

	// Launch spec file consumed by runtime entrypoint
	LaunchFileName = "launch.json"

	// Records what the provisioner installed, for idempotency
	ManifestFileName = "manifest.json"

	// Agent connection spec telling runtime how to reach panel
	AgentFileName = "agent.json"

	// Newest launch spec format this code writes and understands
	LaunchSpecVersion = 1

	// Newest agent spec format this code writes and understands
	AgentSpecVersion = 1
)

// Launch kinds understood by the runtime entrypoint
const (
	LaunchKindJar      = "jar"       // Java <flags> -jar <Jar> <args> nogui
	LaunchKindArgsFile = "args-file" // Java <flags> @<ArgsFile> nogui (modern Forge/NeoForge)
	LaunchKindCustom   = "custom"    // Java <flags> <Exec tokens verbatim>
)

// Tells runtime entrypoint how to start the server process
type LaunchSpec struct {
	Version   int    `json:"version"`
	Kind      string `json:"kind"`
	Jar       string `json:"jar,omitempty"`
	ArgsFile  string `json:"args_file,omitempty"`
	Exec      string `json:"exec,omitempty"` // Whitespace-tokenized for LaunchKindCustom
	Loader    string `json:"loader"`
	MCVersion string `json:"mc_version"`
	JavaMajor int    `json:"java_major"`
}

// Tells runtime supervisor how to reach the panel's agent endpoint
type AgentSpec struct {
	Version  int    `json:"version"`
	Enabled  bool   `json:"enabled"`
	PanelURL string `json:"panel_url"`
	Token    string `json:"token"`
	ServerID string `json:"server_id"`
}

// Identifies the modpack a server was provisioned from
type ModpackRef struct {
	Source    string `json:"source"` // "curseforge" | "modrinth" | "zip"
	ID        string `json:"id"`
	VersionID string `json:"version_id,omitempty"`
}

// Records the provisioned state of a server data directory
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

// Loads agent spec, nil when absent
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

// Persists agent spec, file kept unreadable by group or world
func WriteAgentSpec(dataDir string, spec *AgentSpec) error {
	if err := os.MkdirAll(filepath.Join(dataDir, StateDir), 0755); err != nil {
		return err
	}
	if spec.Version == 0 {
		spec.Version = AgentSpecVersion
	}
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	// Rename replaces the root-owned file container protection leaves
	tmp, err := os.CreateTemp(filepath.Join(dataDir, StateDir), "agent-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), AgentPath(dataDir)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

func ManifestPath(dataDir string) string {
	return filepath.Join(dataDir, StateDir, ManifestFileName)
}

// Loads the launch spec from a server data directory
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

// Persists the launch spec into a server data directory
func WriteLaunchSpec(dataDir string, spec *LaunchSpec) error {
	if err := os.MkdirAll(filepath.Join(dataDir, StateDir), 0755); err != nil {
		return err
	}
	if spec.Version == 0 {
		spec.Version = LaunchSpecVersion
	}
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(LaunchPath(dataDir), data, 0644)
}

// Loads provision manifest, nil when absent
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

// Persists the provision manifest into a server data directory
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

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nickheyer/discopanel/pkg/runtimespec"
)

// agentJarDir is where the runtime image ships the disco-agent jars, one per
// loader family (built from agent/ and baked in by docker/Dockerfile.runtime).
const agentJarDir = "/opt/discopanel/agent"

// installedAgentJarName is the stable filename the agent is installed under
// inside the server's mods/plugins directory, so upgrades replace in place
// and disabling removes exactly one file.
const installedAgentJarName = "disco-agent.jar"

// agentJarFor maps a loader to the shipped jar and the directory (relative to
// the data dir) it must be installed into. Loaders without a supported shim
// return ok=false. Folia is excluded: its regionized scheduler is incompatible
// with the Bukkit scheduler the paper shim uses.
func agentJarFor(spec *runtimespec.LaunchSpec) (jar string, dir string, ok bool) {
	// The mod shims target modern loaders running Java 17+.
	if spec.JavaMajor < 17 {
		return "", "", false
	}
	switch spec.Loader {
	case "fabric", "quilt":
		return "disco-agent-fabric.jar", "mods", true
	case "neoforge":
		return "disco-agent-neoforge.jar", "mods", true
	case "forge":
		return "disco-agent-forge.jar", "mods", true
	case "paper", "purpur", "pufferfish", "spigot", "bukkit":
		return "disco-agent-paper.jar", "plugins", true
	default:
		return "", "", false
	}
}

// installAgentMod copies the loader-matched disco-agent jar into the server's
// mods/plugins directory. Missing jars (e.g. images built without the agent)
// just skip: the supervisor's own telemetry still works.
func installAgentMod(spec *runtimespec.LaunchSpec) {
	jar, dir, ok := agentJarFor(spec)
	if !ok {
		removeAgentMod(spec)
		return
	}
	src := filepath.Join(agentJarDir, jar)
	srcInfo, err := os.Stat(src)
	if err != nil {
		return
	}
	targetDir := filepath.Join(dataDir, dir)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		fmt.Printf("[discopanel-runtime] WARN: cannot create %s for disco-agent: %v\n", dir, err)
		return
	}
	dst := filepath.Join(targetDir, installedAgentJarName)
	if dstInfo, err := os.Stat(dst); err == nil && dstInfo.Size() == srcInfo.Size() && !dstInfo.ModTime().Before(srcInfo.ModTime()) {
		return // already current
	}
	if err := copyFile(src, dst); err != nil {
		fmt.Printf("[discopanel-runtime] WARN: failed to install disco-agent mod: %v\n", err)
		return
	}
	fmt.Printf("[discopanel-runtime] installed disco-agent (%s -> %s/%s)\n", jar, dir, installedAgentJarName)
}

// removeAgentMod removes a previously installed agent jar when the agent is
// disabled or the loader stopped being supported.
func removeAgentMod(spec *runtimespec.LaunchSpec) {
	_ = spec
	for _, dir := range []string{"mods", "plugins"} {
		path := filepath.Join(dataDir, dir, installedAgentJarName)
		if _, err := os.Stat(path); err == nil {
			if err := os.Remove(path); err == nil {
				fmt.Printf("[discopanel-runtime] removed disco-agent from %s/\n", dir)
			}
		}
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

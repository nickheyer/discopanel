package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

const (
	geyserJar     = "/opt/geyser/Geyser.jar"
	geyserURL     = "https://download.geysermc.org/v2/projects/geyser/versions/latest/builds/latest/downloads/standalone"
	dataDir       = "/data"
	configFile    = "/data/config.yml"
)

func main() {
	puid := getEnvInt("PUID", 1000)
	pgid := getEnvInt("PGID", 1000)

	// Ensure data directory exists and is owned correctly
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		fatal("failed to create data dir: %v", err)
	}
	if err := os.Chown(dataDir, puid, pgid); err != nil {
		fatal("failed to chown data dir: %v", err)
	}

	// Download Geyser if not present
	if _, err := os.Stat(geyserJar); os.IsNotExist(err) {
		fmt.Println("Downloading latest Geyser standalone...")
		if err := downloadGeyser(); err != nil {
			fatal("failed to download geyser: %v", err)
		}
	}

	// Generate config if needed
	if getEnv("OVERWRITE_CONFIG", "false") == "true" || !fileExists(configFile) {
		if err := generateConfig(); err != nil {
			fatal("failed to generate config: %v", err)
		}
		if err := os.Chown(configFile, puid, pgid); err != nil {
			fatal("failed to chown config: %v", err)
		}
	}

	// Chown data directory recursively
	if err := chownRecursive(dataDir, puid, pgid); err != nil {
		fatal("failed to chown data dir: %v", err)
	}

	// Build java command
	javaOpts := getEnv("JAVA_OPTS", "-Dlog4j2.disableJmx=true")
	initMem := getEnv("INIT_MEMORY", "1024M")
	maxMem := getEnv("MAX_MEMORY", "1024M")

	args := []string{
		"java",
		javaOpts,
		"-Xms" + initMem,
		"-Xmx" + maxMem,
		"-jar", geyserJar,
	}

	// Find java binary
	javaPath, err := exec.LookPath("java")
	if err != nil {
		fatal("java not found: %v", err)
	}

	// Change to data directory
	if err := os.Chdir(dataDir); err != nil {
		fatal("failed to chdir: %v", err)
	}

	// Drop privileges and exec java
	if err := syscall.Setgid(pgid); err != nil {
		fatal("failed to setgid: %v", err)
	}
	if err := syscall.Setuid(puid); err != nil {
		fatal("failed to setuid: %v", err)
	}

	if err := syscall.Exec(javaPath, args, os.Environ()); err != nil {
		fatal("failed to exec java: %v", err)
	}
}

func downloadGeyser() error {
	if err := os.MkdirAll(filepath.Dir(geyserJar), 0755); err != nil {
		return err
	}

	resp, err := http.Get(geyserURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	f, err := os.Create(geyserJar)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func generateConfig() error {
	config := fmt.Sprintf(`bedrock:
  address: %s
  port: %s
  motd1: "%s"
  motd2: "%s"
  server-name: "%s"
  enable-proxy-protocol: %s

remote:
  address: %s
  port: %s
  auth-type: %s
  allow-password-authentication: %s
  use-proxy-protocol: %s
  forward-hostname: %s

floodgate-key-file: %s
command-suggestions: %s
passthrough-motd: %s
passthrough-protocol-name: %s
passthrough-player-counts: %s
legacy-ping-passthrough: %s
ping-passthrough-interval: %s
max-players: %s
debug-mode: %s
general-thread-pool: %s
allow-third-party-capes: %s
allow-third-party-ears: %s
show-cooldown: %s
show-coordinates: %s
emote-offhand-workaround: %s
default-locale: %s
cache-images: %s
allow-custom-skulls: %s
add-non-bedrock-items: %s
above-bedrock-nether-building: %s
force-resource-packs: %s
xbox-achievements-enabled: %s

metrics:
  enabled: %s
  uuid: %s
`,
		getEnv("BEDROCK_ADDRESS", "0.0.0.0"),
		getEnv("BEDROCK_PORT", "19132"),
		getEnv("BEDROCK_MOTD1", "GeyserMC"),
		getEnv("BEDROCK_MOTD2", "Minecraft Server"),
		getEnv("BEDROCK_SERVERNAME", "Geyser"),
		getEnv("BEDROCK_ENABLE_PROXY_PROTOCOL", "false"),
		getEnv("REMOTE_ADDRESS", "auto"),
		getEnv("REMOTE_PORT", "25565"),
		getEnv("REMOTE_AUTH_TYPE", "online"),
		getEnv("REMOTE_ALLOW_PASS_AUTH", "true"),
		getEnv("REMOTE_USE_PROXY_PROTOCOL", "false"),
		getEnv("REMOTE_FORWARD_HOSTNAME", "false"),
		getEnv("GEYSER_FLOODGATE_KEY_FILE", "key.pem"),
		getEnv("GEYSER_COMMAND_SUGGESTIONS", "true"),
		getEnv("GEYSER_PASSTHROUGH_MOTD", "false"),
		getEnv("GEYSER_PASSTHROUGH_PROTOCOL_NAME", "false"),
		getEnv("GEYSER_PASSTHROUGH_PLAYER_COUNTS", "false"),
		getEnv("GEYSER_PASSTHROUGH_LEGACY_PING", "false"),
		getEnv("GEYSER_PASSTHROUGH_INTERVAL", "3"),
		getEnv("GEYSER_MAX_PLAYER", "100"),
		getEnv("GEYSER_DEBUG", "false"),
		getEnv("GEYSER_GENERAL_THREAD_POOL", "32"),
		getEnv("GEYSER_ALLOW_THIRD_PARTY_CAPES", "true"),
		getEnv("GEYSER_ALLOW_THIRD_PARTY_EARS", "false"),
		getEnv("GEYSER_SHOW_COOLDOWN", "title"),
		getEnv("GEYSER_SHOW_COORDINATES", "true"),
		getEnv("GEYSER_EMOTE_OFFHAND_WORKAROUND", "disabled"),
		getEnv("GEYSER_DEFAULT_LOCALE", "en_us"),
		getEnv("GEYSER_CACHE_IMAGES", "0"),
		getEnv("GEYSER_ALLOW_CUSTOM_SKULLS", "true"),
		getEnv("GEYSER_ADD_NON_BEDROCK_ITEMS", "true"),
		getEnv("GEYSER_ABOVE_BEDROCK_NETHER_BUILDING", "false"),
		getEnv("GEYSER_FORCE_RESOURCE_PACKS", "true"),
		getEnv("GEYSER_XBOX_ACHIEVEMENTS_ENABLED", "false"),
		getEnv("GEYSER_METRICS_ENABLED", "false"),
		getEnv("GEYSER_METRICS_UUID", "generateduuid"),
	)

	return os.WriteFile(configFile, []byte(config), 0644)
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func chownRecursive(path string, uid, gid int) error {
	return filepath.Walk(path, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(name, uid, gid)
	})
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

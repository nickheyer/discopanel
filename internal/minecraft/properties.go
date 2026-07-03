package minecraft

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ServerProperties represents the Minecraft server.properties file
type ServerProperties map[string]string

// SaveServerProperties saves the server.properties file
func SaveServerProperties(serverDataPath string, properties ServerProperties) error {
	propertiesPath := filepath.Join(serverDataPath, "server.properties")

	// Ensure directory exists
	if err := os.MkdirAll(serverDataPath, 0755); err != nil {
		return fmt.Errorf("failed to create server data directory: %w", err)
	}

	// Read the original file to preserve comments and ordering
	originalLines := []string{}
	if file, err := os.Open(propertiesPath); err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			originalLines = append(originalLines, scanner.Text())
		}
		err = scanner.Err()
		if err != nil {
			return fmt.Errorf("failed to scan server properties file: %w", err)
		}
		file.Close()
	}

	// Create new file
	file, err := os.Create(propertiesPath)
	if err != nil {
		return fmt.Errorf("failed to create server.properties: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	updatedKeys := make(map[string]bool)

	// Update existing lines
	for _, line := range originalLines {
		trimmed := strings.TrimSpace(line)

		// Keep comments and empty lines
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			fmt.Fprintf(writer, "%s\n", line)
			continue
		}

		// Check if this is a property line
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			if newValue, exists := properties[key]; exists {
				fmt.Fprintf(writer, "%s=%s\n", key, newValue)
				updatedKeys[key] = true
			} else {
				fmt.Fprintf(writer, "%s\n", line)
			}
		} else {
			fmt.Fprintf(writer, "%s\n", line)
		}
	}

	// Add any new properties that weren't in the original file
	for key, value := range properties {
		if !updatedKeys[key] {
			fmt.Fprintf(writer, "%s=%s\n", key, value)
		}
	}

	return writer.Flush()
}

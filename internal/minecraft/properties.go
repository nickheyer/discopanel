package minecraft

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ServerProperties represents the Minecraft server.properties file
type ServerProperties map[string]string

// LoadServerProperties loads the server.properties file from a server's data directory
func LoadServerProperties(serverDataPath string) (ServerProperties, error) {
	propertiesPath := filepath.Join(serverDataPath, "server.properties")
	
	file, err := os.Open(propertiesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open server.properties: %w", err)
	}
	defer file.Close()

	properties := make(ServerProperties)
	scanner := bufio.NewScanner(file)
	
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		
		// Parse key=value pairs
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			properties[key] = value
		}
	}
	
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading server.properties: %w", err)
	}
	
	return properties, nil
}

// SaveServerProperties saves the server.properties file
func SaveServerProperties(serverDataPath string, properties ServerProperties) error {
	propertiesPath := filepath.Join(serverDataPath, "server.properties")
	
	// Read the original file to preserve comments and ordering
	originalLines := []string{}
	if file, err := os.Open(propertiesPath); err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			originalLines = append(originalLines, scanner.Text())
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
			writer.WriteString(line + "\n")
			continue
		}
		
		// Check if this is a property line
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			if newValue, exists := properties[key]; exists {
				writer.WriteString(key + "=" + newValue + "\n")
				updatedKeys[key] = true
			} else {
				writer.WriteString(line + "\n")
			}
		} else {
			writer.WriteString(line + "\n")
		}
	}
	
	// Add any new properties that weren't in the original file
	for key, value := range properties {
		if !updatedKeys[key] {
			writer.WriteString(key + "=" + value + "\n")
		}
	}
	
	return writer.Flush()
}

// Common property getters and setters

func (p ServerProperties) GetString(key string, defaultValue string) string {
	if value, exists := p[key]; exists {
		return value
	}
	return defaultValue
}

func (p ServerProperties) GetInt(key string, defaultValue int) int {
	if value, exists := p[key]; exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func (p ServerProperties) GetBool(key string, defaultValue bool) bool {
	if value, exists := p[key]; exists {
		return value == "true"
	}
	return defaultValue
}

func (p ServerProperties) SetString(key, value string) {
	p[key] = value
}

func (p ServerProperties) SetInt(key string, value int) {
	p[key] = strconv.Itoa(value)
}

func (p ServerProperties) SetBool(key string, value bool) {
	if value {
		p[key] = "true"
	} else {
		p[key] = "false"
	}
}


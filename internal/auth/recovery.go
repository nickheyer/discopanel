package auth

import (
	"fmt"
	"os"
	"path/filepath"
)

// GetRecoveryKeyPath returns the path where the recovery key should be stored
func GetRecoveryKeyPath() (string, error) {
	// Get the data directory from environment or use default
	dataDir := os.Getenv("DISCOPANEL_DATA_DIR")
	if dataDir == "" {
		// Use current working directory
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dataDir = filepath.Join(cwd, "data")
	}

	// Ensure directory exists
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return "", err
	}

	return filepath.Join(dataDir, ".recovery_key"), nil
}

// SaveRecoveryKeyToFile saves the recovery key to a secure file
func SaveRecoveryKeyToFile(key string) error {
	path, err := GetRecoveryKeyPath()
	if err != nil {
		return err
	}

	// Write key to file with restricted permissions (owner read only)
	content := "DiscoPanel Recovery Key\n========================\n\n"
	content += fmt.Sprintf("Key: %s\n\n", key)
	content += "IMPORTANT: Keep this key secure! It can be used to reset any user password.\n"
	content += "Store it in a safe place outside of this server.\n\n"
	content += "To use this key for password recovery:\n"
	content += "1. Access the login page\n"
	content += "2. Click 'Forgot Password'\n"
	content += "3. Enter your username and this recovery key\n"
	content += "4. Set a new password\n"

	return os.WriteFile(path, []byte(content), 0400)
}

// ReadRecoveryKeyFromFile reads the recovery key from file (for display purposes only)
func ReadRecoveryKeyFromFile() (string, error) {
	path, err := GetRecoveryKeyPath()
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	// Parse the key from the file content
	// This is a simple implementation - in production, you might want more robust parsing
	lines := string(data)
	keyPrefix := "Key: "
	start := 0
	for i := 0; i < len(lines); i++ {
		if i+len(keyPrefix) <= len(lines) && lines[i:i+len(keyPrefix)] == keyPrefix {
			start = i + len(keyPrefix)
			break
		}
	}

	if start > 0 {
		end := start
		for end < len(lines) && lines[end] != '\n' {
			end++
		}
		return lines[start:end], nil
	}

	return "", fmt.Errorf("recovery key not found in file")
}

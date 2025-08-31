package console

import (
	"testing"
)

// TestGlobalHistoryIntegration tests the global history manager functions
func TestGlobalHistoryIntegration(t *testing.T) {
	serverID := "test-server-123"
	
	// Clear any existing history for this server
	ClearCommands(serverID)
	
	// Add a command
	entry := AddCommand(serverID, "list", "Player1\nPlayer2", true, nil)
	
	if entry == nil {
		t.Fatal("Expected command entry, got nil")
	}
	
	if entry.ServerID != serverID {
		t.Errorf("Expected ServerID %s, got %s", serverID, entry.ServerID)
	}
	
	if entry.Command != "list" {
		t.Errorf("Expected Command 'list', got %s", entry.Command)
	}
	
	if entry.Output != "Player1\nPlayer2" {
		t.Errorf("Expected Output 'Player1\\nPlayer2', got %s", entry.Output)
	}
	
	// Retrieve commands
	commands := GetCommands(serverID, 0)
	
	if len(commands) != 1 {
		t.Errorf("Expected 1 command, got %d", len(commands))
	}
	
	if commands[0].ID != entry.ID {
		t.Errorf("Expected same command ID, got different IDs")
	}
	
	// Add another command with error
	errorEntry := AddCommand(serverID, "invalid", "", false, 
		&commandError{message: "Unknown command"})
	
	if errorEntry.Success {
		t.Errorf("Expected Success false for error command")
	}
	
	if errorEntry.Error != "Unknown command" {
		t.Errorf("Expected Error 'Unknown command', got %s", errorEntry.Error)
	}
	
	// Should now have 2 commands
	commands = GetCommands(serverID, 0)
	if len(commands) != 2 {
		t.Errorf("Expected 2 commands, got %d", len(commands))
	}
	
	// Clear and verify
	ClearCommands(serverID)
	commands = GetCommands(serverID, 0)
	if len(commands) != 0 {
		t.Errorf("Expected 0 commands after clear, got %d", len(commands))
	}
}

// commandError is a simple error type for testing
type commandError struct {
	message string
}

func (e *commandError) Error() string {
	return e.message
}
package console

import (
	"errors"
	"testing"
	"time"
)

func TestHistoryStorage_AddCommand(t *testing.T) {
	storage := NewHistoryStorage(5)
	serverID := "test-server"
	
	// Add a successful command
	entry := storage.AddCommand(serverID, "list", "Player1, Player2", true, nil)
	
	if entry.ServerID != serverID {
		t.Errorf("Expected ServerID %s, got %s", serverID, entry.ServerID)
	}
	if entry.Command != "list" {
		t.Errorf("Expected Command 'list', got %s", entry.Command)
	}
	if entry.Output != "Player1, Player2" {
		t.Errorf("Expected Output 'Player1, Player2', got %s", entry.Output)
	}
	if !entry.Success {
		t.Errorf("Expected Success true, got %v", entry.Success)
	}
	if entry.Error != "" {
		t.Errorf("Expected no error, got %s", entry.Error)
	}
}

func TestHistoryStorage_AddCommandWithError(t *testing.T) {
	storage := NewHistoryStorage(5)
	serverID := "test-server"
	
	// Add a failed command
	entry := storage.AddCommand(serverID, "invalid", "", false, errors.New("command failed"))
	
	if entry.Success {
		t.Errorf("Expected Success false, got %v", entry.Success)
	}
	if entry.Error != "command failed" {
		t.Errorf("Expected Error 'command failed', got %s", entry.Error)
	}
}

func TestHistoryStorage_GetCommands(t *testing.T) {
	storage := NewHistoryStorage(5)
	serverID := "test-server"
	
	// Add multiple commands
	storage.AddCommand(serverID, "cmd1", "output1", true, nil)
	storage.AddCommand(serverID, "cmd2", "output2", true, nil)
	storage.AddCommand(serverID, "cmd3", "output3", true, nil)
	
	commands := storage.GetCommands(serverID, 0)
	
	if len(commands) != 3 {
		t.Errorf("Expected 3 commands, got %d", len(commands))
	}
	
	// Check order (should be chronological)
	if commands[0].Command != "cmd1" {
		t.Errorf("Expected first command 'cmd1', got %s", commands[0].Command)
	}
	if commands[2].Command != "cmd3" {
		t.Errorf("Expected last command 'cmd3', got %s", commands[2].Command)
	}
}

func TestHistoryStorage_GetCommandsWithLimit(t *testing.T) {
	storage := NewHistoryStorage(5)
	serverID := "test-server"
	
	// Add multiple commands
	for i := 1; i <= 5; i++ {
		storage.AddCommand(serverID, "cmd", "output", true, nil)
	}
	
	commands := storage.GetCommands(serverID, 3)
	
	if len(commands) != 3 {
		t.Errorf("Expected 3 commands with limit, got %d", len(commands))
	}
}

func TestHistoryStorage_MaxSize(t *testing.T) {
	storage := NewHistoryStorage(3) // Small max size
	serverID := "test-server"
	
	// Add more commands than max size
	for i := 1; i <= 5; i++ {
		storage.AddCommand(serverID, "cmd", "output", true, nil)
	}
	
	commands := storage.GetCommands(serverID, 0)
	
	if len(commands) != 3 {
		t.Errorf("Expected 3 commands (max size), got %d", len(commands))
	}
}

func TestHistoryStorage_ClearCommands(t *testing.T) {
	storage := NewHistoryStorage(5)
	serverID := "test-server"
	
	// Add commands
	storage.AddCommand(serverID, "cmd1", "output1", true, nil)
	storage.AddCommand(serverID, "cmd2", "output2", true, nil)
	
	// Clear commands
	storage.ClearCommands(serverID)
	
	commands := storage.GetCommands(serverID, 0)
	
	if len(commands) != 0 {
		t.Errorf("Expected 0 commands after clear, got %d", len(commands))
	}
}

func TestHistoryStorage_ThreadSafety(t *testing.T) {
	storage := NewHistoryStorage(100)
	serverID := "test-server"
	
	// Run concurrent operations
	done := make(chan bool, 2)
	
	// Writer goroutine
	go func() {
		for i := 0; i < 50; i++ {
			storage.AddCommand(serverID, "cmd", "output", true, nil)
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()
	
	// Reader goroutine
	go func() {
		for i := 0; i < 50; i++ {
			storage.GetCommands(serverID, 10)
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()
	
	// Wait for both goroutines
	<-done
	<-done
	
	// Should not panic and should have some commands
	commands := storage.GetCommands(serverID, 0)
	if len(commands) == 0 {
		t.Errorf("Expected some commands after concurrent operations")
	}
}
# Console Command History

This package provides thread-safe storage for Minecraft server console command history.

## Usage

### Basic Operations

```go
import "github.com/nickheyer/discopanel/internal/console"

// Add a successful command
entry := console.AddCommand("server-123", "list", "Player1, Player2", true, nil)

// Add a failed command
entry := console.AddCommand("server-123", "invalid", "", false, errors.New("Unknown command"))

// Get recent commands (0 = all commands)
commands := console.GetCommands("server-123", 10) // Get last 10 commands

// Clear command history for a server
console.ClearCommands("server-123")
```

### Command Entry Structure

```go
type CommandEntry struct {
    ID        string    `json:"id"`        // Unique identifier
    ServerID  string    `json:"server_id"` // Server identifier
    Command   string    `json:"command"`   // The executed command
    Output    string    `json:"output"`    // Command output
    Success   bool      `json:"success"`   // Whether command succeeded
    Error     string    `json:"error"`     // Error message if failed
    Timestamp time.Time `json:"timestamp"` // When command was executed
}
```

### Integration with API Handlers

```go
// In your command handler
output, err := s.docker.ExecCommand(ctx, containerID, command)

// Store the command result
console.AddCommand(serverID, command, output, err == nil, err)
```

## Features

- **Thread-safe**: Safe for concurrent access from multiple goroutines
- **Memory efficient**: Automatically limits history size per server (default: 100 commands)
- **Simple API**: Easy to integrate with existing code
- **Global instance**: Singleton pattern for easy access across the application

## Configuration

The default maximum history size is 100 commands per server. This can be adjusted by modifying the `NewHistoryStorage()` call in `manager.go`.
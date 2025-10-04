package console

import (
	"sync"
	"time"
)

// CommandEntry represents a single command execution and its result
type CommandEntry struct {
	ID        string    `json:"id"`
	ServerID  string    `json:"server_id"`
	Command   string    `json:"command"`
	Output    string    `json:"output"`
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// HistoryStorage provides thread-safe storage for command history
type HistoryStorage struct {
	commands map[string][]*CommandEntry // serverID -> commands
	mutex    sync.RWMutex
	maxSize  int
}

// NewHistoryStorage creates a new command history storage
func NewHistoryStorage(maxSize int) *HistoryStorage {
	if maxSize <= 0 {
		maxSize = 100 // default max size
	}
	
	return &HistoryStorage{
		commands: make(map[string][]*CommandEntry),
		maxSize:  maxSize,
	}
}

// AddCommand stores a new command entry for the specified server
func (h *HistoryStorage) AddCommand(serverID, command, output string, success bool, err error) *CommandEntry {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	
	entry := &CommandEntry{
		ID:        generateID(),
		ServerID:  serverID,
		Command:   command,
		Output:    output,
		Success:   success,
		Timestamp: time.Now(),
	}
	
	if err != nil {
		entry.Error = err.Error()
	}
	
	// Get existing commands for this server
	commands := h.commands[serverID]
	
	// Add new command
	commands = append(commands, entry)
	
	// Keep only the most recent commands (simple circular buffer)
	if len(commands) > h.maxSize {
		commands = commands[len(commands)-h.maxSize:]
	}
	
	h.commands[serverID] = commands
	
	return entry
}

// GetCommands retrieves command history for a server
func (h *HistoryStorage) GetCommands(serverID string, limit int) []*CommandEntry {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	
	commands := h.commands[serverID]
	if len(commands) == 0 {
		return []*CommandEntry{}
	}
	
	// Apply limit if specified
	if limit > 0 && len(commands) > limit {
		start := len(commands) - limit
		return append([]*CommandEntry{}, commands[start:]...)
	}
	
	// Return copy of the slice to prevent external modification
	return append([]*CommandEntry{}, commands...)
}

// ClearCommands removes all command history for a server
func (h *HistoryStorage) ClearCommands(serverID string) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	
	delete(h.commands, serverID)
}

// generateID creates a simple unique ID for command entries
func generateID() string {
	return time.Now().Format("20060102150405.000000")
}
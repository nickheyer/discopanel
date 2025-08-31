package console

import "sync"

var (
	globalHistory *HistoryStorage
	once          sync.Once
)

// GetGlobalHistory returns the global command history storage instance
func GetGlobalHistory() *HistoryStorage {
	once.Do(func() {
		globalHistory = NewHistoryStorage(100) // Store up to 100 commands per server
	})
	return globalHistory
}

// AddCommand is a convenience function to add a command to the global history
func AddCommand(serverID, command, output string, success bool, err error) *CommandEntry {
	return GetGlobalHistory().AddCommand(serverID, command, output, success, err)
}

// GetCommands is a convenience function to get commands from the global history
func GetCommands(serverID string, limit int) []*CommandEntry {
	return GetGlobalHistory().GetCommands(serverID, limit)
}

// ClearCommands is a convenience function to clear commands from the global history
func ClearCommands(serverID string) {
	GetGlobalHistory().ClearCommands(serverID)
}
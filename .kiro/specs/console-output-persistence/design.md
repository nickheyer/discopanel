# Design Document

## Overview

The console output persistence feature addresses the critical issue where rconn (remote console) command outputs disappear immediately after execution. Currently, commands are executed via Docker's `rcon-cli` tool, which returns output synchronously but this output is not properly captured and persisted in the web console interface.

The solution involves implementing a command output capture and persistence system that:
1. Captures rconn command outputs from the Docker container execution
2. Stores command history and outputs in memory/cache
3. Enhances the frontend console to display persistent command results
4. Provides visual distinction between regular server logs and command outputs

## Architecture

### Current Flow
```
Frontend Console → API `/servers/{id}/command` → Docker ExecCommand → rcon-cli → Minecraft Server
                                                      ↓
                                                   Output returned but not persisted
```

### Enhanced Flow
```
Frontend Console → API `/servers/{id}/command` → Docker ExecCommand → rcon-cli → Minecraft Server
                                                      ↓
                                               Command Output Capture
                                                      ↓
                                               Command History Storage
                                                      ↓
                                               WebSocket/Polling Update → Frontend Console Display
```

## Components and Interfaces

### 1. Backend Components

#### Command History Manager
**Location**: `internal/console/history.go`

```go
type CommandEntry struct {
    ID        string    `json:"id"`
    ServerID  string    `json:"server_id"`
    Command   string    `json:"command"`
    Output    string    `json:"output"`
    Success   bool      `json:"success"`
    Error     string    `json:"error,omitempty"`
    Timestamp time.Time `json:"timestamp"`
}

type HistoryManager interface {
    AddCommand(serverID, command, output string, success bool, err error) *CommandEntry
    GetHistory(serverID string, limit int) []*CommandEntry
    ClearHistory(serverID string)
    GetEntry(entryID string) *CommandEntry
}
```

#### Enhanced Command Handler
**Location**: `internal/api/command_handlers.go` (modified)

- Capture command output from Docker ExecCommand
- Store command and output in HistoryManager
- Return enhanced response with command ID
- Provide endpoint for retrieving command history

#### Console WebSocket Handler (Optional Enhancement)
**Location**: `internal/api/console_handlers.go`

- Real-time command output streaming
- Live console updates without polling

### 2. Frontend Components

#### Enhanced Console Component
**Location**: `web/discopanel/src/lib/components/server-console.svelte` (modified)

- Display command history alongside server logs
- Visual distinction for command entries vs server logs
- Command output persistence across page refreshes
- Enhanced command input with history navigation

#### Command History Store
**Location**: `web/discopanel/src/lib/stores/console.ts`

```typescript
interface CommandEntry {
  id: string;
  server_id: string;
  command: string;
  output: string;
  success: boolean;
  error?: string;
  timestamp: string;
}

interface ConsoleStore {
  commandHistory: CommandEntry[];
  addCommand: (entry: CommandEntry) => void;
  loadHistory: (serverId: string) => Promise<void>;
  clearHistory: (serverId: string) => void;
}
```

## Data Models

### Command Entry Storage
Commands will be stored in memory using a circular buffer to prevent memory leaks:

```go
type ServerCommandHistory struct {
    ServerID string
    Commands []*CommandEntry
    MaxSize  int
    mutex    sync.RWMutex
}
```

### Frontend Command Display Model
```typescript
interface DisplayCommand {
  id: string;
  command: string;
  output: string;
  success: boolean;
  timestamp: Date;
  type: 'command' | 'log';
}
```

## Error Handling

### Backend Error Scenarios
1. **Docker ExecCommand Failure**: Return error response with failure details
2. **RCON Connection Issues**: Capture connection errors and display to user
3. **Command Timeout**: Implement timeout handling for long-running commands
4. **Memory Limits**: Implement circular buffer with configurable size limits

### Frontend Error Scenarios
1. **API Request Failures**: Display error toast and maintain command in input
2. **History Loading Failures**: Graceful degradation with local storage fallback
3. **WebSocket Connection Issues**: Fall back to polling mechanism

## Testing Strategy

### Backend Testing
1. **Unit Tests**:
   - Command history manager operations
   - Command execution and output capture
   - Error handling scenarios
   - Memory management and circular buffer behavior

2. **Integration Tests**:
   - End-to-end command execution flow
   - Docker container command execution
   - API endpoint functionality

### Frontend Testing
1. **Component Tests**:
   - Console component rendering with command history
   - Command input and submission
   - Visual distinction between logs and commands
   - Auto-scroll behavior with mixed content

2. **Store Tests**:
   - Command history management
   - API integration
   - Error state handling

### Manual Testing Scenarios
1. **Command Execution**: Verify commands execute and output persists
2. **Mixed Content**: Test console with both server logs and command outputs
3. **Memory Management**: Test with high command volume
4. **Error Handling**: Test with invalid commands and connection failures
5. **Page Refresh**: Verify command history persists across refreshes

## Implementation Phases

### Phase 1: Backend Command Capture
- Implement CommandEntry model and HistoryManager
- Enhance existing command handler to capture and store output
- Add command history API endpoint

### Phase 2: Frontend Integration
- Modify console component to display command history
- Implement visual distinction between logs and commands
- Add command history loading and display

### Phase 3: Enhanced UX
- Add command history navigation (up/down arrows)
- Implement command output filtering/search
- Add command execution status indicators

### Phase 4: Performance Optimization
- Implement WebSocket for real-time updates (optional)
- Add configurable history limits
- Optimize memory usage and cleanup

## Security Considerations

1. **Command Validation**: Ensure commands are properly validated before execution
2. **Output Sanitization**: Sanitize command outputs to prevent XSS
3. **Access Control**: Verify user permissions for command execution
4. **Rate Limiting**: Implement rate limiting for command execution
5. **Audit Logging**: Log command executions for security auditing

## Performance Considerations

1. **Memory Management**: Use circular buffers with configurable limits (default: 100 commands per server)
2. **API Response Size**: Limit command history responses to prevent large payloads
3. **Frontend Rendering**: Virtualize command history display for large histories
4. **Cleanup**: Implement automatic cleanup of old command entries
5. **Caching**: Cache recent command history in browser storage
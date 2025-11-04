package containers

import (
	"bufio"
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/nickheyer/discopanel/pkg/logger"
)

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp time.Time
	Content   string
	Type      string // "stdout", "stderr", "command", "command_output"
}

// ContainerLogStream handles log streaming for a single container
type ContainerLogStream struct {
	containerID string
	logs        []LogEntry
	mu          sync.RWMutex
	maxEntries  int
	cancelFunc  context.CancelFunc
	active      bool
}

// LogStreamer handles log streaming for all containers
type LogStreamer struct {
	provider   ContainerProvider
	streams    map[string]*ContainerLogStream
	mu         sync.RWMutex
	log        *logger.Logger
	maxEntries int
}

// NewLogStreamer creates a new log streamer
func NewLogStreamer(provider ContainerProvider, log *logger.Logger, maxEntriesPerContainer int) *LogStreamer {
	if maxEntriesPerContainer <= 0 {
		maxEntriesPerContainer = 10000
	}
	return &LogStreamer{
		provider:   provider,
		streams:    make(map[string]*ContainerLogStream),
		log:        log,
		maxEntries: maxEntriesPerContainer,
	}
}

// Start streaming logs for a container
func (ls *LogStreamer) StartStreaming(containerID string) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	// Stop existing stream if any
	if stream, exists := ls.streams[containerID]; exists && stream.active {
		return nil // Already streaming
	}

	// Create new stream
	ctx, cancel := context.WithCancel(context.Background())
	stream := &ContainerLogStream{
		containerID: containerID,
		logs:        []LogEntry{},
		maxEntries:  ls.maxEntries,
		cancelFunc:  cancel,
		active:      true,
	}

	ls.streams[containerID] = stream

	// Start streaming in background
	go ls.streamLogs(ctx, stream)

	return nil
}

// StopStreaming stops streaming logs for a container
func (ls *LogStreamer) StopStreaming(containerID string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if stream, exists := ls.streams[containerID]; exists {
		stream.cancelFunc()
		stream.active = false
	}
}

// StopAllStreaming stops all log streaming
func (ls *LogStreamer) StopAllStreaming() {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	for _, stream := range ls.streams {
		if stream.active {
			stream.cancelFunc()
			stream.active = false
		}
	}
}

// GetLogs returns the logs for a container
func (ls *LogStreamer) GetLogs(containerID string, limit int) []LogEntry {
	ls.mu.RLock()
	stream, exists := ls.streams[containerID]
	ls.mu.RUnlock()

	if !exists {
		return []LogEntry{}
	}

	stream.mu.RLock()
	defer stream.mu.RUnlock()

	logs := stream.logs
	if limit > 0 && len(logs) > limit {
		logs = logs[len(logs)-limit:]
	}

	result := make([]LogEntry, len(logs))
	copy(result, logs)
	return result
}

// AddLogEntry adds a log entry for a container
func (ls *LogStreamer) AddLogEntry(containerID string, logType string, content string) {
	ls.mu.RLock()
	stream, exists := ls.streams[containerID]
	ls.mu.RUnlock()

	if !exists {
		return
	}

	entry := LogEntry{
		Timestamp: time.Now(),
		Type:      logType,
		Content:   content,
	}

	stream.mu.Lock()
	defer stream.mu.Unlock()

	stream.logs = append(stream.logs, entry)

	// Trim if exceeded max entries
	if len(stream.logs) > stream.maxEntries {
		stream.logs = stream.logs[len(stream.logs)-stream.maxEntries:]
	}
}

// Prepend cmd execution w/ cmd input log
func (ls *LogStreamer) AddCommandEntry(containerID, command string, timestamp time.Time) {
	ls.mu.RLock()
	stream, exists := ls.streams[containerID]
	ls.mu.RUnlock()

	if !exists {
		return
	}

	stream.mu.Lock()
	defer stream.mu.Unlock()

	// Add command entry with the provided timestamp + ANSI to prevent color bleed
	stream.logs = append(stream.logs, LogEntry{
		Timestamp: timestamp,
		Content:   "\u001b[0m" + command,
		Type:      "command",
	})

	// Trim if exceeding max entries
	if len(stream.logs) > stream.maxEntries {
		stream.logs = stream.logs[len(stream.logs)-stream.maxEntries:]
	}
}

// Add command output to the log stream (after execution)
func (ls *LogStreamer) AddCommandOutput(containerID, output string, success bool, timestamp time.Time) {
	ls.mu.RLock()
	stream, exists := ls.streams[containerID]
	ls.mu.RUnlock()

	if !exists {
		return // No stream to add to
	}

	stream.mu.Lock()
	defer stream.mu.Unlock()

	// Add output entry if present + ANSI to prevent color bleed
	if output != "" {
		output = "\u001b[0m" + output + "\u001b[0m"
		lines := strings.SplitSeq(strings.TrimSpace(output), "\n")
		for line := range lines {
			if line != "" {
				stream.logs = append(stream.logs, LogEntry{
					Timestamp: timestamp,
					Content:   line,
					Type:      "command_output",
				})
			}
		}
	} else if !success {
		// Add error message if command failed with no output
		stream.logs = append(stream.logs, LogEntry{
			Timestamp: timestamp,
			Content:   "Command failed to execute",
			Type:      "command_output",
		})
	}

	// Trim if exceeding max entries
	if len(stream.logs) > stream.maxEntries {
		stream.logs = stream.logs[len(stream.logs)-stream.maxEntries:]
	}
}

// IsStreaming checks if a container is currently being streamed
func (ls *LogStreamer) IsStreaming(containerID string) bool {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	stream, exists := ls.streams[containerID]
	return exists && stream.active
}

// streamLogs streams logs from a container
func (ls *LogStreamer) streamLogs(ctx context.Context, stream *ContainerLogStream) {
	defer func() {
		stream.mu.Lock()
		stream.active = false
		stream.mu.Unlock()
	}()

	reader, err := ls.provider.Logs(ctx, stream.containerID)
	if err != nil {
		ls.log.Error("Failed to get logs for container %s: %v", stream.containerID, err)
		return
	}
	defer reader.Close()

	// We assume TTY is true
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for long lines

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default: // To get rid of whatever the hell runtime is doing when TTY is enabled
			line := scanner.Text()
			// Split on \r carriage and take last chunk
			if strings.Contains(line, "\r") {
				parts := strings.Split(line, "\r")
				line = parts[len(parts)-1]
			}

			// Filter out RCON spam
			if shouldFilterLine(line) {
				continue
			}

			if line != "" {
				entry := LogEntry{
					Timestamp: time.Now(),
					Content:   line,
					Type:      "stdout",
				}

				stream.mu.Lock()
				stream.logs = append(stream.logs, entry)

				// Trim if exceeding max entries, keep recent
				if len(stream.logs) > stream.maxEntries {
					stream.logs = stream.logs[len(stream.logs)-stream.maxEntries:]
				}
				stream.mu.Unlock()
			}
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		ls.log.Error("Error reading logs for container %s: %v", stream.containerID, err)
	}
}

func shouldFilterLine(line string) bool {
	// Filter out RCON connection spam
	filters := []string{
		"Thread RCON Client",
		"[RCON Listener",
		"Rcon connection from",
	}

	for _, filter := range filters {
		if strings.Contains(line, filter) {
			return true
		}
	}

	return false
}

// Clears logs for a container
func (ls *LogStreamer) ClearLogs(containerID string) {
	ls.mu.RLock()
	stream, exists := ls.streams[containerID]
	ls.mu.RUnlock()

	if exists {
		stream.mu.Lock()
		stream.logs = make([]LogEntry, 0, stream.maxEntries)
		stream.mu.Unlock()
	}
}

package api

import (
	"bufio"
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/nickheyer/discopanel/pkg/logger"
)

// A single log entry
type LogEntry struct {
	Timestamp time.Time
	Content   string
	Type      string // "stdout", "stderr", "command", "command_output"
}

// Log streaming for a single container
type ContainerLogStream struct {
	containerID string
	logs        []LogEntry
	mu          sync.RWMutex
	maxEntries  int
	cancelFunc  context.CancelFunc
	active      bool
}

// Log streaming for all containers
type LogStreamer struct {
	docker     *client.Client
	streams    map[string]*ContainerLogStream // containerID -> stream
	mu         sync.RWMutex
	log        *logger.Logger
	maxEntries int
}

// Creates a new log streamer
func NewLogStreamer(dockerClient *client.Client, log *logger.Logger, maxEntriesPerContainer int) *LogStreamer {
	if maxEntriesPerContainer <= 0 {
		maxEntriesPerContainer = 10000 // Default to 10k entries per container
	}
	return &LogStreamer{
		docker:     dockerClient,
		streams:    make(map[string]*ContainerLogStream),
		log:        log,
		maxEntries: maxEntriesPerContainer,
	}
}

// Start streaming logs for a container
func (ls *LogStreamer) StartStreaming(containerID string) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	// Check if already streaming
	if stream, exists := ls.streams[containerID]; exists && stream.active {
		return nil // Already streaming
	}

	// Create new stream
	ctx, cancel := context.WithCancel(context.Background())
	stream := &ContainerLogStream{
		containerID: containerID,
		logs:        make([]LogEntry, 0, ls.maxEntries),
		maxEntries:  ls.maxEntries,
		cancelFunc:  cancel,
		active:      true,
	}

	ls.streams[containerID] = stream

	// Start streaming in background
	go ls.streamLogs(ctx, stream)

	return nil
}

// Stop streaming logs for a container
func (ls *LogStreamer) StopStreaming(containerID string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if stream, exists := ls.streams[containerID]; exists && stream.active {
		stream.cancelFunc()
		stream.active = false
	}
}

// Setup and start streaming of logs from Docker in the background
func (ls *LogStreamer) streamLogs(ctx context.Context, stream *ContainerLogStream) {
	defer func() {
		stream.mu.Lock()
		stream.active = false
		stream.mu.Unlock()
	}()

	// Check if container has TTY enabled
	inspect, err := ls.docker.ContainerInspect(ctx, stream.containerID)
	if err != nil {
		ls.log.Error("Failed to inspect container %s: %v", stream.containerID, err)
		return
	}

	// Log streaming config
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
		Tail:       "100", // Start with last 100 lines
	}

	// Start streaming
	reader, err := ls.docker.ContainerLogs(ctx, stream.containerID, options)
	if err != nil {
		ls.log.Error("Failed to start log streaming for container %s: %v", stream.containerID, err)
		return
	}
	defer reader.Close()

	// If TTY is disabled, Docker sends multiplexed stream demultiplexed
	var logReader io.Reader
	if !inspect.Config.Tty {
		pr, pw := io.Pipe()
		go func() {
			defer pw.Close()
			_, err := stdcopy.StdCopy(pw, pw, reader)
			if err != nil && err != io.EOF {
				ls.log.Error("Error demultiplexing logs for container %s: %v", stream.containerID, err)
			}
		}()
		logReader = pr
	} else {
		// TTY enabled: raw stream, no headers
		logReader = reader
	}

	scanner := bufio.NewScanner(logReader)
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
			if ls.shouldFilterLine(line) {
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

// Check if a log line should be filtered
func (ls *LogStreamer) shouldFilterLine(line string) bool {
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

// Prepend cmd execution w/ cmd input log
func (ls *LogStreamer) AddCommandEntry(containerID, command string, timestamp time.Time) {
	ls.mu.RLock()
	stream, exists := ls.streams[containerID]
	ls.mu.RUnlock()

	if !exists {
		// Create a new stream if it doesn't exist
		ls.mu.Lock()
		stream = &ContainerLogStream{
			containerID: containerID,
			logs:        make([]LogEntry, 0, ls.maxEntries),
			maxEntries:  ls.maxEntries,
			active:      false,
		}
		ls.streams[containerID] = stream
		ls.mu.Unlock()
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

// Log entry and output together for one cmd (NOTE: Should probably add before/after execution seperately and not use this)
func (ls *LogStreamer) AddCommand(containerID, command, output string, success bool) {
	timestamp := time.Now()
	ls.AddCommandEntry(containerID, command, timestamp)
	if output != "" || !success {
		ls.AddCommandOutput(containerID, output, success, timestamp)
	}
}

// Get logs for a container
func (ls *LogStreamer) GetLogs(containerID string, tail int) []LogEntry {
	ls.mu.RLock()
	stream, exists := ls.streams[containerID]
	ls.mu.RUnlock()

	if !exists {
		return []LogEntry{}
	}

	stream.mu.RLock()
	defer stream.mu.RUnlock()

	// Return the requested tail of logs
	if tail <= 0 || tail > len(stream.logs) {
		// Return all logs
		result := make([]LogEntry, len(stream.logs))
		copy(result, stream.logs)
		return result
	}

	// Return last 'tail' entries
	start := len(stream.logs) - tail
	result := make([]LogEntry, tail)
	copy(result, stream.logs[start:])
	return result
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

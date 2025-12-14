package logger

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
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ContainerLogStream manages log streaming for a single container
type ContainerLogStream struct {
	containerID string
	logs        []*v1.LogEntry
	mu          sync.RWMutex
	maxEntries  int
	cancelFunc  context.CancelFunc
	active      bool
}

// LogStreamer manages log streaming for all containers
type LogStreamer struct {
	docker     *client.Client
	streams    map[string]*ContainerLogStream // containerID -> stream
	mu         sync.RWMutex
	log        *Logger
	maxEntries int
}

// NewLogStreamer creates a new log streamer
func NewLogStreamer(dockerClient *client.Client, log *Logger, maxEntriesPerContainer int) *LogStreamer {
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

// StartStreaming starts streaming logs for a container
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
		logs:        make([]*v1.LogEntry, 0, ls.maxEntries),
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

	if stream, exists := ls.streams[containerID]; exists && stream.active {
		stream.cancelFunc()
		stream.active = false
	}
}

// streamLogs sets up and starts streaming of logs from Docker in the background
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

	// If TTY is disabled, Docker sends multiplexed stream that needs demultiplexing
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
		default:
			line := scanner.Text()
			// Split on \r carriage return and take last chunk
			if strings.Contains(line, "\r") {
				parts := strings.Split(line, "\r")
				line = parts[len(parts)-1]
			}

			// Filter out RCON spam
			if ls.shouldFilterLine(line) {
				continue
			}

			if line != "" {
				entry := &v1.LogEntry{
					Timestamp: timestamppb.New(time.Now()),
					Message:   line,
					Level:     "info",
					Source:    "stdout",
					IsCommand: false,
					IsError:   false,
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

// shouldFilterLine checks if a log line should be filtered
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

// AddCommandEntry prepends command execution with command input log
func (ls *LogStreamer) AddCommandEntry(containerID, command string, timestamp time.Time) {
	ls.mu.RLock()
	stream, exists := ls.streams[containerID]
	ls.mu.RUnlock()

	if !exists {
		// Create a new stream if it doesn't exist
		ls.mu.Lock()
		stream = &ContainerLogStream{
			containerID: containerID,
			logs:        make([]*v1.LogEntry, 0, ls.maxEntries),
			maxEntries:  ls.maxEntries,
			active:      false,
		}
		ls.streams[containerID] = stream
		ls.mu.Unlock()
	}

	stream.mu.Lock()
	defer stream.mu.Unlock()

	// Add command entry with the provided timestamp + ANSI to prevent color bleed
	entry := &v1.LogEntry{
		Timestamp: timestamppb.New(timestamp),
		Message:   "\u001b[0m" + command,
		Level:     "debug",
		Source:    "command",
		IsCommand: true,
		IsError:   false,
	}

	stream.logs = append(stream.logs, entry)

	// Trim if exceeding max entries
	if len(stream.logs) > stream.maxEntries {
		stream.logs = stream.logs[len(stream.logs)-stream.maxEntries:]
	}
}

// AddCommandOutput adds command output to the log stream (after execution)
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
		lines := strings.Split(strings.TrimSpace(output), "\n")
		for _, line := range lines {
			if line != "" {
				entry := &v1.LogEntry{
					Timestamp: timestamppb.New(timestamp),
					Message:   line,
					Level:     "debug",
					Source:    "command_output",
					IsCommand: false,
					IsError:   !success,
				}
				stream.logs = append(stream.logs, entry)
			}
		}
	} else if !success {
		// Add error message if command failed with no output
		entry := &v1.LogEntry{
			Timestamp: timestamppb.New(timestamp),
			Message:   "Command failed to execute",
			Level:     "error",
			Source:    "command_output",
			IsCommand: false,
			IsError:   true,
		}
		stream.logs = append(stream.logs, entry)
	}

	// Trim if exceeding max entries
	if len(stream.logs) > stream.maxEntries {
		stream.logs = stream.logs[len(stream.logs)-stream.maxEntries:]
	}
}

// GetLogs gets logs for a container
func (ls *LogStreamer) GetLogs(containerID string, tail int) []*v1.LogEntry {
	ls.mu.RLock()
	stream, exists := ls.streams[containerID]
	ls.mu.RUnlock()

	if !exists {
		return []*v1.LogEntry{}
	}

	stream.mu.RLock()
	defer stream.mu.RUnlock()

	// Return the requested tail of logs
	if tail <= 0 || tail > len(stream.logs) {
		// Return all logs
		result := make([]*v1.LogEntry, len(stream.logs))
		copy(result, stream.logs)
		return result
	}

	// Return last 'tail' entries
	start := len(stream.logs) - tail
	result := make([]*v1.LogEntry, tail)
	copy(result, stream.logs[start:])
	return result
}

// ClearLogs clears logs for a container
func (ls *LogStreamer) ClearLogs(containerID string) {
	ls.mu.RLock()
	stream, exists := ls.streams[containerID]
	ls.mu.RUnlock()

	if exists {
		stream.mu.Lock()
		stream.logs = make([]*v1.LogEntry, 0, stream.maxEntries)
		stream.mu.Unlock()
	}
}
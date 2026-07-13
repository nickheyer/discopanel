package main

import (
	"bufio"
	"io"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// Matches JVM GC pause lines, captures duration
var gcPausePattern = regexp.MustCompile(`\bPause\b.*\s([0-9]+(?:\.[0-9]+)?)ms\s*$`)

// Follows the JVM GC log, aggregates pause durations
type gcLogTail struct {
	path    string
	enabled bool

	mu      sync.Mutex
	count   int64
	totalMs float64
	maxMs   float64
}

func newGCLogTail(path string, javaMajor int) *gcLogTail {
	return &gcLogTail{path: path, enabled: javaMajor >= 11}
}

// Returns and resets the current pause window
func (t *gcLogTail) drain() (int64, float64, float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	count, total, max := t.count, t.totalMs, t.maxMs
	t.count, t.totalMs, t.maxMs = 0, 0, 0
	return count, total, max
}

func (t *gcLogTail) record(ms float64) {
	t.mu.Lock()
	t.count++
	t.totalMs += ms
	if ms > t.maxMs {
		t.maxMs = ms
	}
	t.mu.Unlock()
}

// Polls the GC log for appended lines, reopens on rotation
func (t *gcLogTail) run(done chan struct{}) {
	if !t.enabled {
		return
	}

	var file *os.File
	var reader *bufio.Reader
	var offset int64

	reopen := func() bool {
		if file != nil {
			_ = file.Close()
			file = nil
		}
		f, err := os.Open(t.path)
		if err != nil {
			return false
		}
		file = f
		reader = bufio.NewReader(f)
		offset = 0
		return true
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			if file != nil {
				_ = file.Close()
			}
			return
		case <-ticker.C:
		}

		if file == nil {
			if !reopen() {
				continue
			}
			// First open reads the log from the top
		}

		// A shrunken file size means it was recreated
		if info, err := os.Stat(t.path); err == nil && info.Size() < offset {
			if !reopen() {
				continue
			}
		}

		for {
			line, err := reader.ReadString('\n')
			offset += int64(len(line))
			if line != "" {
				if m := gcPausePattern.FindStringSubmatch(trimEOL(line)); m != nil {
					if ms, perr := strconv.ParseFloat(m[1], 64); perr == nil {
						t.record(ms)
					}
				}
			}
			if err != nil {
				if err != io.EOF {
					_ = file.Close()
					file = nil
				}
				break
			}
		}
	}
}

func trimEOL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

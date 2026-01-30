package upload

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nickheyer/discopanel/pkg/logger"
)

var (
	ErrSessionNotFound  = errors.New("upload session not found")
	ErrSessionExpired   = errors.New("upload session expired")
	ErrSessionCompleted = errors.New("upload session already completed")
	ErrInvalidChunk     = errors.New("invalid chunk index")
	ErrChunkExists      = errors.New("chunk already received")
	ErrFileTooLarge     = errors.New("file exceeds maximum allowed size")
)

// Session represents an active upload session
type Session struct {
	ID             string
	Filename       string
	TotalSize      int64
	ChunkSize      int32
	TotalChunks    int32
	ReceivedChunks map[int32]bool
	BytesReceived  int64
	TempPath       string
	ExpiresAt      time.Time
	Completed      bool
	mu             sync.Mutex
	file           *os.File
}

// Manager handles upload sessions
type Manager struct {
	sessions      map[string]*Session
	mu            sync.RWMutex
	tempDir       string
	sessionTTL    time.Duration
	maxUploadSize int64
	log           *logger.Logger
	stopCh        chan struct{}
}

// NewManager creates a new upload manager
func NewManager(tempDir string, sessionTTL time.Duration, maxUploadSize int64, log *logger.Logger) *Manager {
	m := &Manager{
		sessions:      make(map[string]*Session),
		tempDir:       tempDir,
		sessionTTL:    sessionTTL,
		maxUploadSize: maxUploadSize,
		log:           log,
		stopCh:        make(chan struct{}),
	}

	// Ensure temp directory exists
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		log.Error("Failed to create upload temp directory: %v", err)
	}

	// Start cleanup goroutine
	go m.cleanupLoop()

	return m
}

// InitSession creates a new upload session
func (m *Manager) InitSession(filename string, totalSize int64, chunkSize int32) (*Session, error) {
	// Validate max upload size
	if m.maxUploadSize > 0 && totalSize > m.maxUploadSize {
		return nil, ErrFileTooLarge
	}

	// Calculate total chunks
	totalChunks := int32((totalSize + int64(chunkSize) - 1) / int64(chunkSize))
	if totalChunks == 0 {
		totalChunks = 1
	}

	// Generate session ID
	sessionID := uuid.New().String()

	// Create temp file path
	tempPath := filepath.Join(m.tempDir, fmt.Sprintf("upload-%s-%s", sessionID, sanitizeFilename(filename)))

	// Create and open temp file
	file, err := os.Create(tempPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	// Pre-allocate file size for efficient random writes
	if err := file.Truncate(totalSize); err != nil {
		file.Close()
		os.Remove(tempPath)
		return nil, fmt.Errorf("failed to allocate file space: %w", err)
	}

	session := &Session{
		ID:             sessionID,
		Filename:       filename,
		TotalSize:      totalSize,
		ChunkSize:      chunkSize,
		TotalChunks:    totalChunks,
		ReceivedChunks: make(map[int32]bool),
		BytesReceived:  0,
		TempPath:       tempPath,
		ExpiresAt:      time.Now().Add(m.sessionTTL),
		Completed:      false,
		file:           file,
	}

	m.mu.Lock()
	m.sessions[sessionID] = session
	m.mu.Unlock()

	m.log.Info("Upload session created: %s (file: %s, size: %d, chunks: %d)", sessionID, filename, totalSize, totalChunks)

	return session, nil
}

// GetSession retrieves a session by ID
func (m *Manager) GetSession(id string) (*Session, error) {
	m.mu.RLock()
	session, exists := m.sessions[id]
	m.mu.RUnlock()

	if !exists {
		return nil, ErrSessionNotFound
	}

	if time.Now().After(session.ExpiresAt) {
		m.cleanupSession(id)
		return nil, ErrSessionExpired
	}

	return session, nil
}

// WriteChunk writes a chunk to the session's temp file
func (m *Manager) WriteChunk(sessionID string, chunkIndex int32, data []byte) (completed bool, err error) {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return false, err
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.Completed {
		return true, ErrSessionCompleted
	}

	// Validate chunk index
	if chunkIndex < 0 || chunkIndex >= session.TotalChunks {
		return false, ErrInvalidChunk
	}

	// Check if chunk already received
	if session.ReceivedChunks[chunkIndex] {
		// Allow re-upload of same chunk (idempotent)
		return session.Completed, nil
	}

	// Calculate offset for this chunk
	offset := int64(chunkIndex) * int64(session.ChunkSize)

	// Write data at offset
	_, err = session.file.WriteAt(data, offset)
	if err != nil {
		return false, fmt.Errorf("failed to write chunk: %w", err)
	}

	// Mark chunk as received
	session.ReceivedChunks[chunkIndex] = true
	session.BytesReceived += int64(len(data))

	// Check if all chunks received
	if len(session.ReceivedChunks) >= int(session.TotalChunks) {
		session.Completed = true
		// Sync and close the file
		if err := session.file.Sync(); err != nil {
			m.log.Error("Failed to sync file for session %s: %v", sessionID, err)
		}
		if err := session.file.Close(); err != nil {
			m.log.Error("Failed to close file for session %s: %v", sessionID, err)
		}
		session.file = nil
		m.log.Info("Upload session completed: %s", sessionID)
	}

	return session.Completed, nil
}

// GetMissingChunks returns the list of chunk indices that haven't been received
func (m *Manager) GetMissingChunks(sessionID string) ([]int32, error) {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return nil, err
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	var missing []int32
	for i := int32(0); i < session.TotalChunks; i++ {
		if !session.ReceivedChunks[i] {
			missing = append(missing, i)
		}
	}

	return missing, nil
}

// GetTempPath returns the temp file path for a completed session
func (m *Manager) GetTempPath(sessionID string) (string, string, error) {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return "", "", err
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if !session.Completed {
		return "", "", errors.New("upload not completed")
	}

	return session.TempPath, session.Filename, nil
}

// GetSessionStatus returns the status of a session
func (m *Manager) GetSessionStatus(sessionID string) (bytesReceived int64, totalBytes int64, chunksReceived int32, totalChunks int32, completed bool, tempPath string, err error) {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return 0, 0, 0, 0, false, "", err
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	tempPathResult := ""
	if session.Completed {
		tempPathResult = session.TempPath
	}

	return session.BytesReceived, session.TotalSize, int32(len(session.ReceivedChunks)), session.TotalChunks, session.Completed, tempPathResult, nil
}

// Cancel cancels an upload session and cleans up
func (m *Manager) Cancel(sessionID string) error {
	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	if exists {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()

	if !exists {
		return ErrSessionNotFound
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	// Close file if open
	if session.file != nil {
		session.file.Close()
		session.file = nil
	}

	// Remove temp file
	if session.TempPath != "" {
		os.Remove(session.TempPath)
	}

	m.log.Info("Upload session cancelled: %s", sessionID)
	return nil
}

// CleanupSession removes a session after the file has been moved to its destination
func (m *Manager) CleanupSession(sessionID string) error {
	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	if exists {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()

	if !exists {
		return nil // Already cleaned up
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	// Close file if open
	if session.file != nil {
		session.file.Close()
		session.file = nil
	}

	// Note: Don't remove temp file here - it should be moved by the caller
	m.log.Info("Upload session cleaned up: %s", sessionID)
	return nil
}

// Stop stops the manager and cleans up all sessions
func (m *Manager) Stop() {
	close(m.stopCh)

	m.mu.Lock()
	defer m.mu.Unlock()

	for id, session := range m.sessions {
		session.mu.Lock()
		if session.file != nil {
			session.file.Close()
		}
		if session.TempPath != "" {
			os.Remove(session.TempPath)
		}
		session.mu.Unlock()
		delete(m.sessions, id)
	}
}

// cleanupLoop periodically cleans up expired sessions
func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.cleanupExpired()
		case <-m.stopCh:
			return
		}
	}
}

// cleanupExpired removes all expired sessions
func (m *Manager) cleanupExpired() {
	m.mu.Lock()
	var expired []string
	now := time.Now()
	for id, session := range m.sessions {
		if now.After(session.ExpiresAt) {
			expired = append(expired, id)
		}
	}
	m.mu.Unlock()

	for _, id := range expired {
		m.cleanupSession(id)
	}

	if len(expired) > 0 {
		m.log.Info("Cleaned up %d expired upload sessions", len(expired))
	}
}

// cleanupSession removes a single session
func (m *Manager) cleanupSession(id string) {
	m.mu.Lock()
	session, exists := m.sessions[id]
	if exists {
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	if !exists {
		return
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.file != nil {
		session.file.Close()
		session.file = nil
	}

	if session.TempPath != "" {
		os.Remove(session.TempPath)
	}
}

// sanitizeFilename removes potentially dangerous characters from filename
func sanitizeFilename(filename string) string {
	// Replace path separators and other potentially dangerous characters
	result := make([]byte, 0, len(filename))
	for i := 0; i < len(filename); i++ {
		c := filename[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '_' {
			result = append(result, c)
		}
	}
	if len(result) == 0 {
		return "upload"
	}
	return string(result)
}

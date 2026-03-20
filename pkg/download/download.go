package download

import (
	"errors"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nickheyer/discopanel/pkg/logger"
)

var (
	ErrSessionNotFound = errors.New("download session not found")
	ErrSessionExpired  = errors.New("download session expired")
)

// A prepared download
type Session struct {
	ID          string
	FilePath    string // file to serve
	Filename    string // suggested download filename
	TotalSize   int64
	DeleteAfter bool // if true, file is deleted on session cleanup
	ExpiresAt   time.Time
}

// Handles download sessions and their temp-file lifecycle
type Manager struct {
	sessions   map[string]*Session
	mu         sync.RWMutex
	tempDir    string
	sessionTTL time.Duration
	log        *logger.Logger
	stopCh     chan struct{}
}

// New download manager and starts the cleanup loop
func NewManager(tempDir string, sessionTTL time.Duration, log *logger.Logger) *Manager {
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		log.Error("Failed to create download temp directory: %v", err)
	}

	m := &Manager{
		sessions:   make(map[string]*Session),
		tempDir:    tempDir,
		sessionTTL: sessionTTL,
		log:        log,
		stopCh:     make(chan struct{}),
	}

	go m.cleanupLoop()
	return m
}

func (m *Manager) TempDir() string {
	return m.tempDir
}

// Registers a file that is ready to be downloaded. Deletes file on cleanup if deleteAfter is true
func (m *Manager) InitSession(filePath, filename string, totalSize int64, deleteAfter bool) *Session {
	session := &Session{
		ID:          uuid.New().String(),
		FilePath:    filePath,
		Filename:    filename,
		TotalSize:   totalSize,
		DeleteAfter: deleteAfter,
		ExpiresAt:   time.Now().Add(m.sessionTTL),
	}

	m.mu.Lock()
	m.sessions[session.ID] = session
	m.mu.Unlock()

	m.log.Info("Download session created: %s (file: %s, size: %d)", session.ID, filename, totalSize)
	return session
}

// Get session by ID
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

// Cleanup session and its temp file
func (m *Manager) CleanupSession(id string) {
	m.cleanupSession(id)
}

// Stop manager and clean up sessions
func (m *Manager) Stop() {
	close(m.stopCh)

	m.mu.Lock()
	defer m.mu.Unlock()

	for id, session := range m.sessions {
		if session.DeleteAfter && session.FilePath != "" {
			os.Remove(session.FilePath)
		}
		delete(m.sessions, id)
	}
}

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

func (m *Manager) cleanupExpired() {
	m.mu.RLock()
	var expired []string
	now := time.Now()
	for id, session := range m.sessions {
		if now.After(session.ExpiresAt) {
			expired = append(expired, id)
		}
	}
	m.mu.RUnlock()

	for _, id := range expired {
		m.cleanupSession(id)
	}

	if len(expired) > 0 {
		m.log.Info("Cleaned up %d expired download sessions", len(expired))
	}
}

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

	if session.DeleteAfter && session.FilePath != "" {
		os.Remove(session.FilePath)
	}
}

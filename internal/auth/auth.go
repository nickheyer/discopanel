package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/nickheyer/discopanel/internal/db"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserNotActive      = errors.New("user is not active")
	ErrAuthDisabled       = errors.New("authentication is disabled")
	ErrInvalidToken       = errors.New("invalid token")
	ErrSessionExpired     = errors.New("session expired")
)

type Manager struct {
	store *db.Store
}

func NewManager(store *db.Store) *Manager {
	return &Manager{
		store: store,
	}
}

// HashPassword hashes a plain text password
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// CheckPassword compares a hashed password with plain text
func CheckPassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// GenerateSecretKey generates a random secret key
func GenerateSecretKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// GenerateJWT generates a JWT token for a user
func (m *Manager) GenerateJWT(user *db.User, authConfig *db.AuthConfig) (string, error) {
	claims := jwt.MapClaims{
		"user_id":  user.ID,
		"username": user.Username,
		"role":     user.Role,
		"exp":      time.Now().Add(time.Duration(authConfig.SessionTimeout) * time.Second).Unix(),
		"iat":      time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(authConfig.JWTSecret))
}

// ValidateJWT validates a JWT token and returns the claims
func (m *Manager) ValidateJWT(tokenString string, authConfig *db.AuthConfig) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(authConfig.JWTSecret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		// Check expiration
		if exp, ok := claims["exp"].(float64); ok {
			if time.Now().Unix() > int64(exp) {
				return nil, ErrSessionExpired
			}
		}
		return claims, nil
	}

	return nil, ErrInvalidToken
}

// Login authenticates a user and creates a session
func (m *Manager) Login(ctx context.Context, username, password string) (*db.User, string, error) {
	// Check if auth is enabled
	authConfig, _, err := m.store.GetAuthConfig(ctx)
	if err != nil {
		return nil, "", err
	}

	if !authConfig.Enabled {
		// If auth is disabled and no users exist, allow access
		userCount, err := m.store.CountUsers(ctx)
		if err != nil {
			return nil, "", err
		}
		if userCount == 0 {
			// No users exist, auth is disabled - allow unrestricted access
			return nil, "", nil
		}
		return nil, "", ErrAuthDisabled
	}

	// Get user by username
	user, err := m.store.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, "", ErrInvalidCredentials
	}

	// Check password
	if !CheckPassword(user.PasswordHash, password) {
		return nil, "", ErrInvalidCredentials
	}

	// Check if user is active
	if !user.IsActive {
		return nil, "", ErrUserNotActive
	}

	// Generate JWT token
	token, err := m.GenerateJWT(user, authConfig)
	if err != nil {
		return nil, "", err
	}

	// Create session
	session := &db.Session{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: time.Now().Add(time.Duration(authConfig.SessionTimeout) * time.Second),
	}
	if err := m.store.CreateSession(ctx, session); err != nil {
		return nil, "", err
	}

	// Update last login
	now := time.Now()
	user.LastLogin = &now
	if err := m.store.UpdateUser(ctx, user); err != nil {
		// Non-critical error, log but don't fail
	}

	return user, token, nil
}

// Logout deletes a user session
func (m *Manager) Logout(ctx context.Context, token string) error {
	return m.store.DeleteSession(ctx, token)
}

// ValidateSession validates a session token
func (m *Manager) ValidateSession(ctx context.Context, token string) (*db.User, error) {
	// Check if auth is enabled
	authConfig, _, err := m.store.GetAuthConfig(ctx)
	if err != nil {
		return nil, err
	}

	if !authConfig.Enabled {
		// If auth is disabled and no users exist, allow access
		userCount, err := m.store.CountUsers(ctx)
		if err != nil {
			return nil, err
		}
		if userCount == 0 {
			// No users exist, auth is disabled - allow unrestricted access
			return nil, nil
		}
		return nil, ErrAuthDisabled
	}

	// Validate JWT
	claims, err := m.ValidateJWT(token, authConfig)
	if err != nil {
		return nil, err
	}

	// Get session from database
	session, err := m.store.GetSession(ctx, token)
	if err != nil {
		return nil, ErrSessionExpired
	}

	// Verify user ID matches
	if userID, ok := claims["user_id"].(string); ok {
		if session.UserID != userID {
			return nil, ErrInvalidToken
		}
	}

	return session.User, nil
}

// CreateUser creates a new user
func (m *Manager) CreateUser(ctx context.Context, username, email, password string, role db.UserRole) (*db.User, error) {
	// Hash password
	hashedPassword, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	// Handle optional email
	var emailPtr *string
	if email != "" {
		emailPtr = &email
	}

	// Create user
	user := &db.User{
		ID:           uuid.New().String(),
		Username:     username,
		Email:        emailPtr,
		PasswordHash: hashedPassword,
		Role:         role,
		IsActive:     true,
	}

	if err := m.store.CreateUser(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

// ChangePassword changes a user's password
func (m *Manager) ChangePassword(ctx context.Context, userID, oldPassword, newPassword string) error {
	// Get user
	user, err := m.store.GetUser(ctx, userID)
	if err != nil {
		return err
	}

	// Verify old password
	if !CheckPassword(user.PasswordHash, oldPassword) {
		return ErrInvalidCredentials
	}

	// Hash new password
	hashedPassword, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	// Update password
	user.PasswordHash = hashedPassword
	return m.store.UpdateUser(ctx, user)
}

// ResetPassword resets a user's password using recovery key
func (m *Manager) ResetPassword(ctx context.Context, username, recoveryKey, newPassword string) error {
	// Get auth config
	authConfig, _, err := m.store.GetAuthConfig(ctx)
	if err != nil {
		return err
	}

	// Verify recovery key
	if !CheckPassword(authConfig.RecoveryKeyHash, recoveryKey) {
		return ErrInvalidCredentials
	}

	// Get user
	user, err := m.store.GetUserByUsername(ctx, username)
	if err != nil {
		return err
	}

	// Hash new password
	hashedPassword, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	// Update password
	user.PasswordHash = hashedPassword
	return m.store.UpdateUser(ctx, user)
}

// InitializeAuth initializes authentication configuration
func (m *Manager) InitializeAuth(ctx context.Context) error {
	authConfig, isNew, err := m.store.GetAuthConfig(ctx)
	if err != nil {
		return err
	}

	if isNew || authConfig.JWTSecret == "" {
		// Generate JWT secret
		jwtSecret, err := GenerateSecretKey()
		if err != nil {
			return err
		}
		authConfig.JWTSecret = jwtSecret

		// Generate recovery key
		recoveryKey, err := GenerateSecretKey()
		if err != nil {
			return err
		}
		authConfig.RecoveryKey = recoveryKey
		
		// Hash recovery key for storage
		hashedRecovery, err := HashPassword(recoveryKey)
		if err != nil {
			return err
		}
		authConfig.RecoveryKeyHash = hashedRecovery

		// Save config
		if err := m.store.SaveAuthConfig(ctx, authConfig); err != nil {
			return err
		}

		// Write recovery key to file (only on first initialization)
		if err := m.saveRecoveryKey(recoveryKey); err != nil {
			// Log error but don't fail
			fmt.Printf("Warning: Could not save recovery key to file: %v\n", err)
		}
	}

	return nil
}

// saveRecoveryKey saves the recovery key to a file
func (m *Manager) saveRecoveryKey(key string) error {
	// Save to file
	if err := SaveRecoveryKeyToFile(key); err != nil {
		// If file save fails, at least print it
		fmt.Printf("\n===========================================\n")
		fmt.Printf("IMPORTANT: Save this recovery key securely!\n")
		fmt.Printf("Recovery Key: %s\n", key)
		fmt.Printf("===========================================\n\n")
		return err
	}
	
	// Also print to console for immediate visibility
	path, _ := GetRecoveryKeyPath()
	fmt.Printf("\n===========================================\n")
	fmt.Printf("Recovery key has been saved to: %s\n", path)
	fmt.Printf("Recovery Key: %s\n", key)
	fmt.Printf("IMPORTANT: Keep this key secure!\n")
	fmt.Printf("===========================================\n\n")
	
	return nil
}

// CheckPermission checks if a user has permission for an action
func CheckPermission(user *db.User, requiredRole db.UserRole) bool {
	if user == nil {
		return false
	}

	// Admin can do everything
	if user.Role == db.RoleAdmin {
		return true
	}

	// Editor can do editor and viewer actions
	if user.Role == db.RoleEditor && (requiredRole == db.RoleEditor || requiredRole == db.RoleViewer) {
		return true
	}

	// Viewer can only do viewer actions
	if user.Role == db.RoleViewer && requiredRole == db.RoleViewer {
		return true
	}

	return false
}
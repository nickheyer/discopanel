package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/nickheyer/discopanel/internal/config"
	"github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/rbac"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrUserNotActive        = errors.New("user is not active")
	ErrInvalidToken         = errors.New("invalid token")
	ErrSessionExpired       = errors.New("session expired")
	ErrLocalAuthDisabled    = errors.New("local authentication is disabled")
	ErrRegistrationDisabled = errors.New("registration is disabled")
	ErrSessionTimeoutMin    = errors.New("session timeout must be at least 300 seconds (5 minutes)")
	ErrAPITokenExpired      = errors.New("api token has expired")
	ErrAPITokenNotFound     = errors.New("api token not found")
	ErrInvalidRecoveryKey   = errors.New("invalid recovery key")
)

// Auth override keys
const (
	settingLocalEnabled      = "auth.local.enabled"
	settingAllowRegistration = "auth.local.allow_registration"
	settingAnonymousAccess   = "auth.anonymous_access"
	settingSessionTimeout    = "auth.session_timeout"
)

type Manager struct {
	store       *db.Store
	enforcer    *rbac.Enforcer
	config      *config.AuthConfig
	jwtSecret   []byte
	recoveryKey string
}

const jwtSecretSettingKey = "jwt_secret"

func NewManager(store *db.Store, enforcer *rbac.Enforcer, cfg *config.AuthConfig) (*Manager, error) {
	ctx := context.Background()
	var secret []byte

	// Priority: config value → DB-stored value → generate + persist to DB
	if cfg.JWTSecret != "" {
		secret = []byte(cfg.JWTSecret)
	} else {
		stored, err := store.GetSystemSetting(ctx, jwtSecretSettingKey)
		if err == nil && stored != "" {
			secret, err = hex.DecodeString(stored)
			if err != nil {
				return nil, fmt.Errorf("failed to decode stored JWT secret: %w", err)
			}
		} else {
			// Generate new secret and persist it
			secret = make([]byte, 32)
			if _, err := rand.Read(secret); err != nil {
				return nil, fmt.Errorf("failed to generate JWT secret: %w", err)
			}
			if err := store.SetSystemSetting(ctx, jwtSecretSettingKey, hex.EncodeToString(secret)); err != nil {
				return nil, fmt.Errorf("failed to persist JWT secret: %w", err)
			}
			// Clean all sessions since old tokens are now invalid
			_ = store.CleanAllSessions(ctx)
		}
	}

	// Generate recovery key
	recoveryBytes := make([]byte, 32)
	if _, err := rand.Read(recoveryBytes); err != nil {
		return nil, fmt.Errorf("failed to generate recovery key: %w", err)
	}

	m := &Manager{
		store:       store,
		enforcer:    enforcer,
		config:      cfg,
		jwtSecret:   secret,
		recoveryKey: hex.EncodeToString(recoveryBytes),
	}

	m.loadSettingOverrides(ctx)

	return m, nil
}

func (m *Manager) Login(ctx context.Context, username, password string) (*db.User, []string, string, time.Time, error) {
	if !m.config.Local.Enabled {
		return nil, nil, "", time.Time{}, ErrLocalAuthDisabled
	}

	user, err := m.store.GetUserByUsernameAndProvider(ctx, username, "local")
	if err != nil {
		return nil, nil, "", time.Time{}, ErrInvalidCredentials
	}

	if !checkPassword(user.PasswordHash, password) {
		return nil, nil, "", time.Time{}, ErrInvalidCredentials
	}

	if !user.IsActive {
		return nil, nil, "", time.Time{}, ErrUserNotActive
	}

	// Get user roles
	roleNames, err := m.store.GetUserRoleNames(ctx, user.ID)
	if err != nil {
		return nil, nil, "", time.Time{}, fmt.Errorf("failed to get user roles: %w", err)
	}

	// Generate token
	expiresAt := time.Now().Add(time.Duration(m.config.SessionTimeout) * time.Second)
	token, err := m.generateJWT(user.ID, user.Username, roleNames, expiresAt)
	if err != nil {
		return nil, nil, "", time.Time{}, err
	}

	// Create session
	session := &db.Session{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: expiresAt,
	}
	if err := m.store.CreateSession(ctx, session); err != nil {
		return nil, nil, "", time.Time{}, err
	}

	// Update last login
	now := time.Now()
	user.LastLogin = &now
	_ = m.store.UpdateUser(ctx, user)

	return user, roleNames, token, expiresAt, nil
}

func (m *Manager) ValidateSession(ctx context.Context, token string) (*AuthenticatedUser, error) {
	if token == "" {
		return nil, ErrInvalidToken
	}

	// Validate JWT
	claims, err := m.validateJWT(token)
	if err != nil {
		return nil, err
	}

	// Get session from database
	session, err := m.store.GetSession(ctx, token)
	if err != nil {
		return nil, ErrSessionExpired
	}

	userID, _ := claims["user_id"].(string)
	if session.UserID != userID {
		return nil, ErrInvalidToken
	}

	// Get user
	user, err := m.store.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	if !user.IsActive {
		return nil, ErrUserNotActive
	}

	// Get roles
	roleNames, err := m.store.GetUserRoleNames(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	authUser := &AuthenticatedUser{
		ID:       user.ID,
		Username: user.Username,
		Roles:    roleNames,
		Provider: user.AuthProvider,
	}
	if user.Email != nil {
		authUser.Email = *user.Email
	}

	return authUser, nil
}

func (m *Manager) Logout(ctx context.Context, token string) error {
	return m.store.DeleteSession(ctx, token)
}

func (m *Manager) CreateLocalUser(ctx context.Context, username, email, password string) (*db.User, error) {
	hashedPassword, err := hashPassword(password)
	if err != nil {
		return nil, err
	}

	var emailPtr *string
	if email != "" {
		emailPtr = &email
	}

	user := &db.User{
		ID:           uuid.New().String(),
		Username:     username,
		Email:        emailPtr,
		PasswordHash: hashedPassword,
		AuthProvider: "local",
		IsActive:     true,
	}

	if err := m.store.CreateUser(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

func (m *Manager) ChangePassword(ctx context.Context, userID, oldPassword, newPassword string) error {
	user, err := m.store.GetUser(ctx, userID)
	if err != nil {
		return err
	}

	if user.AuthProvider != "local" {
		return errors.New("password change only available for local auth users")
	}

	if !checkPassword(user.PasswordHash, oldPassword) {
		return ErrInvalidCredentials
	}

	hashedPassword, err := hashPassword(newPassword)
	if err != nil {
		return err
	}

	user.PasswordHash = hashedPassword
	return m.store.UpdateUser(ctx, user)
}

func (m *Manager) AnonymousUser() *AuthenticatedUser {
	return &AuthenticatedUser{
		ID:       "anonymous",
		Username: "anonymous",
		Roles:    []string{"anonymous"},
		Provider: "anonymous",
	}
}

func (m *Manager) IsAnonymousAccessEnabled() bool {
	return m.config.AnonymousAccess
}

func (m *Manager) IsAnyAuthEnabled() bool {
	return m.config.Local.Enabled || m.config.OIDC.Enabled
}

func (m *Manager) IsLocalAuthEnabled() bool {
	return m.config.Local.Enabled
}

func (m *Manager) IsRegistrationAllowed() bool {
	return m.config.Local.Enabled && m.config.Local.AllowRegistration
}

func (m *Manager) GetConfig() *config.AuthConfig {
	return m.config
}

// loadSettingOverrides reads SystemSetting overrides from the DB and applies
// them to the in-memory config, so DB values take precedence over config.yaml.
func (m *Manager) loadSettingOverrides(ctx context.Context) {
	if v, err := m.store.GetSystemSetting(ctx, settingLocalEnabled); err == nil {
		if b, err := strconv.ParseBool(v); err == nil {
			m.config.Local.Enabled = b
		}
	}
	if v, err := m.store.GetSystemSetting(ctx, settingAllowRegistration); err == nil {
		if b, err := strconv.ParseBool(v); err == nil {
			m.config.Local.AllowRegistration = b
		}
	}
	if v, err := m.store.GetSystemSetting(ctx, settingAnonymousAccess); err == nil {
		if b, err := strconv.ParseBool(v); err == nil {
			m.config.AnonymousAccess = b
		}
	}
	if v, err := m.store.GetSystemSetting(ctx, settingSessionTimeout); err == nil {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			m.config.SessionTimeout = i
		}
	}
}

// UpdateSettings updates mutable auth settings. Only non-nil parameters are applied.
func (m *Manager) UpdateSettings(ctx context.Context, localEnabled, allowReg, anonAccess *bool, sessionTimeout *int32) error {
	// Validate session timeout
	if sessionTimeout != nil && *sessionTimeout < 300 {
		return ErrSessionTimeoutMin
	}

	// Persist and apply each provided field
	if localEnabled != nil {
		if err := m.store.SetSystemSetting(ctx, settingLocalEnabled, strconv.FormatBool(*localEnabled)); err != nil {
			return fmt.Errorf("failed to save local auth setting: %w", err)
		}
		m.config.Local.Enabled = *localEnabled
	}

	if allowReg != nil {
		if err := m.store.SetSystemSetting(ctx, settingAllowRegistration, strconv.FormatBool(*allowReg)); err != nil {
			return fmt.Errorf("failed to save registration setting: %w", err)
		}
		m.config.Local.AllowRegistration = *allowReg
	}

	if anonAccess != nil {
		if err := m.store.SetSystemSetting(ctx, settingAnonymousAccess, strconv.FormatBool(*anonAccess)); err != nil {
			return fmt.Errorf("failed to save anonymous access setting: %w", err)
		}
		m.config.AnonymousAccess = *anonAccess
	}

	if sessionTimeout != nil {
		if err := m.store.SetSystemSetting(ctx, settingSessionTimeout, strconv.Itoa(int(*sessionTimeout))); err != nil {
			return fmt.Errorf("failed to save session timeout setting: %w", err)
		}
		m.config.SessionTimeout = int(*sessionTimeout)
	}

	return nil
}

// Creates a new API token for a user. Plaintext is returned, SHA-256 hash is stored
func (m *Manager) GenerateAPIToken(ctx context.Context, userID, name string, expiresInDays *int32) (string, *db.APIToken, error) {
	// Generate 32 random bytes
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("failed to generate token: %w", err)
	}

	plaintext := "dp_" + base64.RawURLEncoding.EncodeToString(raw)

	// SHA-256 hash for storage
	hash := sha256.Sum256([]byte(plaintext))
	hashHex := hex.EncodeToString(hash[:])

	var expiresAt *time.Time
	if expiresInDays != nil && *expiresInDays > 0 {
		t := time.Now().Add(time.Duration(*expiresInDays) * 24 * time.Hour)
		expiresAt = &t
	}

	token := &db.APIToken{
		ID:        uuid.New().String(),
		UserID:    userID,
		Name:      name,
		TokenHash: hashHex,
		ExpiresAt: expiresAt,
	}

	if err := m.store.CreateAPIToken(ctx, token); err != nil {
		return "", nil, fmt.Errorf("failed to store api token: %w", err)
	}

	return plaintext, token, nil
}

// Validates a raw API token (dp_...) and returns the authenticated user.
func (m *Manager) ValidateAPIToken(ctx context.Context, rawToken string) (*AuthenticatedUser, error) {
	if !strings.HasPrefix(rawToken, "dp_") {
		return nil, ErrInvalidToken
	}

	// Hash the incoming token
	hash := sha256.Sum256([]byte(rawToken))
	hashHex := hex.EncodeToString(hash[:])

	// Look up by hash
	apiToken, err := m.store.GetAPITokenByHash(ctx, hashHex)
	if err != nil {
		return nil, ErrAPITokenNotFound
	}

	// Check expiry
	if apiToken.ExpiresAt != nil && apiToken.ExpiresAt.Before(time.Now()) {
		return nil, ErrAPITokenExpired
	}

	// Resolve user
	user, err := m.store.GetUser(ctx, apiToken.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get token user: %w", err)
	}

	if !user.IsActive {
		return nil, ErrUserNotActive
	}

	// Get roles
	roleNames, err := m.store.GetUserRoleNames(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user roles: %w", err)
	}

	// Background-update last_used_at
	go func() {
		_ = m.store.UpdateAPITokenLastUsed(context.Background(), apiToken.ID)
	}()

	authUser := &AuthenticatedUser{
		ID:       user.ID,
		Username: user.Username,
		Roles:    roleNames,
		Provider: user.AuthProvider,
	}
	if user.Email != nil {
		authUser.Email = *user.Email
	}

	return authUser, nil
}

func (m *Manager) GetRecoveryKey() string {
	return m.recoveryKey
}

func (m *Manager) UseRecoveryKey(ctx context.Context, key string) error {
	if m.recoveryKey == "" || key != m.recoveryKey {
		return ErrInvalidRecoveryKey
	}
	if err := m.store.ResetAllUsers(ctx); err != nil {
		return fmt.Errorf("failed to reset users: %w", err)
	}
	m.recoveryKey = ""
	return nil
}

func (m *Manager) generateJWT(userID, username string, roles []string, expiresAt time.Time) (string, error) {
	claims := jwt.MapClaims{
		"user_id":  userID,
		"username": username,
		"roles":    roles,
		"exp":      expiresAt.Unix(),
		"iat":      time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.jwtSecret)
}

func (m *Manager) validateJWT(tokenString string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return m.jwtSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if exp, ok := claims["exp"].(float64); ok {
			if time.Now().Unix() > int64(exp) {
				return nil, ErrSessionExpired
			}
		}
		return claims, nil
	}

	return nil, ErrInvalidToken
}

func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func checkPassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

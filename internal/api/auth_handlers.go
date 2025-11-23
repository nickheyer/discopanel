package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/nickheyer/discopanel/internal/auth"
	"github.com/nickheyer/discopanel/internal/db"
	"golang.org/x/oauth2"
)

// Auth request/response structures
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token     string    `json:"token"`
	User      *db.User  `json:"user"`
	ExpiresAt time.Time `json:"expires_at"`
}

type RegisterRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

type ResetPasswordRequest struct {
	Username    string `json:"username"`
	RecoveryKey string `json:"recovery_key"`
	NewPassword string `json:"new_password"`
}

type OIDCVerifyPasswordRequest struct {
	Password string `json:"password"`
}

type AuthStatusResponse struct {
	Enabled           bool `json:"enabled"`
	FirstUserSetup    bool `json:"first_user_setup"`
	AllowRegistration bool `json:"allow_registration"`
	OIDCEnabled       bool `json:"oidc_enabled"`
}

type CreateUserRequest struct {
	Username string      `json:"username"`
	Email    string      `json:"email"`
	Password string      `json:"password"`
	Role     db.UserRole `json:"role"`
}

type UpdateUserRequest struct {
	Email    *string      `json:"email,omitempty"`
	Role     *db.UserRole `json:"role,omitempty"`
	IsActive *bool        `json:"is_active,omitempty"`
}

// LoginError represents all possible login error types
type LoginError string

const (
	// General login errors
	LoginErrorInvalidCredentials LoginError = "invalid_credentials"
	LoginErrorUserNotActive      LoginError = "user_not_active"
	LoginErrorAuthDisabled       LoginError = "auth_disabled"
	LoginErrorLoginFailed        LoginError = "login_failed"

	// OIDC callback errors
	LoginErrorOIDCError                        LoginError = "oidc_error"
	LoginErrorMissingCode                      LoginError = "missing_code"
	LoginErrorInvalidState                     LoginError = "invalid_state"
	LoginErrorConfigurationError               LoginError = "configuration_error"
	LoginErrorProviderError                    LoginError = "provider_error"
	LoginErrorTokenExchangeFailed              LoginError = "token_exchange_failed"
	LoginErrorMissingIDToken                   LoginError = "missing_id_token"
	LoginErrorTokenVerificationFailed          LoginError = "token_verification_failed"
	LoginErrorClaimsExtractionFailed           LoginError = "claims_extraction_failed"
	LoginErrorDatabaseError                    LoginError = "database_error"
	LoginErrorAccountLinkedToDifferentProvider LoginError = "account_linked_to_different_provider"
	LoginErrorPasswordGenerationFailed         LoginError = "password_generation_failed"
	LoginErrorPasswordHashingFailed            LoginError = "password_hashing_failed"
	LoginErrorEmailAlreadyExists               LoginError = "email_already_exists"
	LoginErrorUserCreationFailed               LoginError = "user_creation_failed"
	LoginErrorAccountDisabled                  LoginError = "account_disabled"
	LoginErrorTokenGenerationFailed            LoginError = "token_generation_failed"
	LoginErrorSessionCreationFailed            LoginError = "session_creation_failed"
	LoginErrorPasswordVerificationRequired     LoginError = "password_verification_required"
)

// OIDC callback helper types
type oidcCallbackParams struct {
	code  string
	state string
}

type oidcClaims struct {
	Subject           string `json:"sub"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	Username          string `json:"username"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
}

// OIDCUserMatchMethod indicates how a user was matched during OIDC login
type OIDCUserMatchMethod string

const (
	OIDCMatchBySub      OIDCUserMatchMethod = "sub"
	OIDCMatchByEmail    OIDCUserMatchMethod = "email"
	OIDCMatchByUsername OIDCUserMatchMethod = "username"
	OIDCMatchNone       OIDCUserMatchMethod = "none"
)

// generateRandomPassword generates a random 32-character password
func generateRandomPassword() (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	const length = 32
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	for i := range bytes {
		bytes[i] = charset[bytes[i]%byte(len(charset))]
	}
	return string(bytes), nil
}

// handleLogin handles user login
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate input
	if req.Username == "" || req.Password == "" {
		s.respondError(w, http.StatusBadRequest, "Username and password are required")
		return
	}

	// Attempt login
	user, token, err := s.authManager.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		switch err {
		case auth.ErrInvalidCredentials:
			s.respondError(w, http.StatusUnauthorized, "Invalid credentials")
		case auth.ErrUserNotActive:
			s.respondError(w, http.StatusForbidden, "User account is not active")
		case auth.ErrAuthDisabled:
			s.respondError(w, http.StatusForbidden, "Authentication is disabled")
		default:
			s.log.Error("Login error: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Login failed")
		}
		return
	}

	// Get auth config for session timeout
	authConfig, _, _ := s.store.GetAuthConfig(r.Context())
	expiresAt := time.Now().Add(time.Duration(authConfig.SessionTimeout) * time.Second)

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieAuthToken,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	s.respondJSON(w, http.StatusOK, LoginResponse{
		Token:     token,
		User:      user,
		ExpiresAt: expiresAt,
	})
}

// handleLogout handles user logout
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Extract token
	token := ""
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		// Parse Bearer token
		if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			token = authHeader[7:]
		}
	}

	if token == "" {
		// Try cookie
		cookie, err := r.Cookie(auth.CookieAuthToken)
		if err == nil && cookie != nil {
			token = cookie.Value
		}
	}

	if token != "" {
		// Delete session
		if err := s.authManager.Logout(r.Context(), token); err != nil {
			s.log.Error("Logout error: %v", err)
		}
	}

	// Clear all auth/oidc cookies by expiring them
	cookieNames := []string{
		auth.CookieAuthToken,
		auth.CookieOIDCAccessToken,
		auth.CookieOIDCIdToken,
		auth.CookieRefreshToken,
	}
	for _, name := range cookieNames {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			Expires:  time.Now().Add(-time.Hour),
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
	}

	s.respondJSON(w, http.StatusOK, map[string]string{"message": "Logged out successfully"})
}

// handleRegister handles user registration (first user or when registration is enabled)
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate input
	if req.Username == "" || req.Password == "" {
		s.respondError(w, http.StatusBadRequest, "Username and password are required")
		return
	}

	// Check if this is first user setup
	userCount, err := s.store.CountUsers(r.Context())
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, "Failed to check user count")
		return
	}

	// Determine role
	var role db.UserRole
	if userCount == 0 {
		// First user is always admin
		role = db.RoleAdmin
	} else {
		// Check if registration is allowed
		authConfig, _, err := s.store.GetAuthConfig(r.Context())
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, "Failed to get auth config")
			return
		}

		if !authConfig.AllowRegistration {
			s.respondError(w, http.StatusForbidden, "Registration is disabled")
			return
		}

		// New users default to viewer role
		role = db.RoleViewer
	}

	// Create user
	user, err := s.authManager.CreateUser(r.Context(), req.Username, req.Email, req.Password, role)
	if err != nil {
		s.log.Error("Failed to create user: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	// If this was the first user, enable authentication
	if userCount == 0 {
		authConfig, _, _ := s.store.GetAuthConfig(r.Context())
		authConfig.Enabled = true
		if err := s.store.SaveAuthConfig(r.Context(), authConfig); err != nil {
			s.log.Error("Failed to enable authentication: %v", err)
		}
	}

	s.respondJSON(w, http.StatusCreated, user)
}

// handleGetAuthStatus returns the current authentication status
func (s *Server) handleGetAuthStatus(w http.ResponseWriter, r *http.Request) {
	authConfig, _, err := s.store.GetAuthConfig(r.Context())
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, "Failed to get auth config")
		return
	}

	s.respondJSON(w, http.StatusOK, AuthStatusResponse{
		Enabled:           authConfig.Enabled,
		FirstUserSetup:    false,
		AllowRegistration: authConfig.AllowRegistration,
		OIDCEnabled:       authConfig.OIDCEnabled,
	})
}

// handleGetCurrentUser returns the current authenticated user
func (s *Server) handleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUserFromContext(r.Context())
	if user == nil {
		s.respondError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	s.respondJSON(w, http.StatusOK, user)
}

// handleChangePassword handles password change for the current user
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	user := auth.GetUserFromContext(r.Context())
	if user == nil {
		s.respondError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	if err := s.authManager.ChangePassword(r.Context(), user.ID, req.OldPassword, req.NewPassword); err != nil {
		if err == auth.ErrInvalidCredentials {
			s.respondError(w, http.StatusBadRequest, "Invalid old password")
		} else {
			s.log.Error("Failed to change password: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Failed to change password")
		}
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]string{"message": "Password changed successfully"})
}

// handleResetPassword handles password reset using recovery key
func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := s.authManager.ResetPassword(r.Context(), req.Username, req.RecoveryKey, req.NewPassword); err != nil {
		if err == auth.ErrInvalidCredentials {
			s.respondError(w, http.StatusBadRequest, "Invalid recovery key or username")
		} else {
			s.log.Error("Failed to reset password: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Failed to reset password")
		}
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]string{"message": "Password reset successfully"})
}

// User management endpoints (admin only)

// handleListUsers returns all users (admin only)
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, "Failed to list users")
		return
	}

	s.respondJSON(w, http.StatusOK, users)
}

// handleCreateUser creates a new user (admin only)
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate role
	if req.Role != db.RoleAdmin && req.Role != db.RoleEditor && req.Role != db.RoleViewer {
		s.respondError(w, http.StatusBadRequest, "Invalid role")
		return
	}

	user, err := s.authManager.CreateUser(r.Context(), req.Username, req.Email, req.Password, req.Role)
	if err != nil {
		s.log.Error("Failed to create user: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	s.respondJSON(w, http.StatusCreated, user)
}

// handleUpdateUser updates a user (admin only)
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["id"]

	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	user, err := s.store.GetUser(r.Context(), userID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "User not found")
		return
	}

	// Update fields if provided
	if req.Email != nil {
		if *req.Email == "" {
			user.Email = nil // Allow clearing email
		} else {
			user.Email = req.Email
		}
	}
	if req.Role != nil {
		user.Role = *req.Role
	}
	if req.IsActive != nil {
		user.IsActive = *req.IsActive
	}

	if err := s.store.UpdateUser(r.Context(), user); err != nil {
		s.respondError(w, http.StatusInternalServerError, "Failed to update user")
		return
	}

	s.respondJSON(w, http.StatusOK, user)
}

// handleDeleteUser deletes a user (admin only)
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["id"]

	// Prevent self-deletion
	currentUser := auth.GetUserFromContext(r.Context())
	if currentUser != nil && currentUser.ID == userID {
		s.respondError(w, http.StatusBadRequest, "Cannot delete your own account")
		return
	}

	if err := s.store.DeleteUser(r.Context(), userID); err != nil {
		s.respondError(w, http.StatusInternalServerError, "Failed to delete user")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]string{"message": "User deleted successfully"})
}

// handleGetAuthConfig returns auth configuration
func (s *Server) handleGetAuthConfig(w http.ResponseWriter, r *http.Request) {
	config, _, err := s.store.GetAuthConfig(r.Context())
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, "Failed to get auth config")
		return
	}

	// If auth is enabled, check for admin permission
	if config.Enabled {
		user := auth.GetUserFromContext(r.Context())
		if user == nil || user.Role != db.RoleAdmin {
			s.respondError(w, http.StatusForbidden, "Admin access required")
			return
		}
	}

	// Don't send sensitive fields
	response := map[string]interface{}{
		"enabled":              config.Enabled,
		"session_timeout":      config.SessionTimeout,
		"require_email_verify": config.RequireEmailVerify,
		"allow_registration":   config.AllowRegistration,
		"oidc_enabled":         config.OIDCEnabled,
	}

	s.respondJSON(w, http.StatusOK, response)
}

// handleUpdateAuthConfig updates auth configuration
func (s *Server) handleUpdateAuthConfig(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	config, _, err := s.store.GetAuthConfig(r.Context())
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, "Failed to get auth config")
		return
	}

	// If auth is currently enabled, require admin permission
	if config.Enabled {
		user := auth.GetUserFromContext(r.Context())
		if user == nil || user.Role != db.RoleAdmin {
			s.respondError(w, http.StatusForbidden, "Admin access required")
			return
		}
	}

	// Check if trying to enable auth for the first time
	if val, ok := req["enabled"].(bool); ok && val && !config.Enabled {
		// Check if any users exist
		userCount, err := s.store.CountUsers(r.Context())
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, "Failed to check user count")
			return
		}

		if userCount == 0 {
			// Need to create first admin user
			s.respondJSON(w, http.StatusOK, map[string]interface{}{
				"requires_first_user": true,
				"message":             "Create an admin account to enable authentication",
			})
			return
		}
	}

	// Update allowed fields
	if val, ok := req["enabled"].(bool); ok {
		config.Enabled = val
	}
	if val, ok := req["session_timeout"].(float64); ok {
		config.SessionTimeout = int(val)
	}
	if val, ok := req["require_email_verify"].(bool); ok {
		config.RequireEmailVerify = val
	}
	if val, ok := req["allow_registration"].(bool); ok {
		config.AllowRegistration = val
	}
	if val, ok := req["oidc_enabled"].(bool); ok {
		config.OIDCEnabled = val
	}

	if err := s.store.SaveAuthConfig(r.Context(), config); err != nil {
		s.respondError(w, http.StatusInternalServerError, "Failed to update auth config")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]string{"message": "Auth config updated successfully"})
}

// handleOIDCLogin initiates the OIDC login flow
func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	// Check if OIDC is enabled
	authConfig, _, err := s.store.GetAuthConfig(r.Context())
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, "Failed to get auth config")
		return
	}

	if !authConfig.OIDCEnabled {
		s.respondError(w, http.StatusBadRequest, "OIDC is not enabled")
		return
	}

	// Check if OIDC discovery service is available
	if s.oidcDiscovery == nil {
		s.respondError(w, http.StatusInternalServerError, "OIDC discovery service not configured")
		return
	}

	// Get OIDC provider
	provider, err := s.oidcDiscovery.GetProvider(r.Context())
	if err != nil {
		s.log.Error("Failed to get OIDC provider: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to initialize OIDC provider")
		return
	}

	// Generate random state for CSRF protection
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		s.log.Error("Failed to generate state: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to generate state")
		return
	}
	state := base64.URLEncoding.EncodeToString(stateBytes)

	// Store state in cookie (expires in 10 minutes)
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieOIDCState,
		Value:    state,
		Path:     "/",
		MaxAge:   600, // 10 minutes
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil, // Only set Secure if using HTTPS
	})

	// Build OAuth2 config
	oauth2Config := oauth2.Config{
		ClientID:     s.config.OIDC.ClientID,
		ClientSecret: s.config.OIDC.ClientSecret,
		RedirectURL:  s.config.OIDC.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       s.config.OIDC.Scopes,
	}

	// Generate authorization URL with state parameter
	// The state will be automatically included as a query parameter in the URL
	authURL := oauth2Config.AuthCodeURL(state, oauth2.AccessTypeOffline)

	s.log.Debug("Redirecting to OIDC provider with state: %s", state[:8]+"...") // Log first 8 chars for debugging

	// Redirect to OIDC provider (state is included in the URL)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// extractOIDCCallbackParams extracts and validates OIDC callback query parameters
func (s *Server) extractOIDCCallbackParams(r *http.Request) (*oidcCallbackParams, error) {
	errorParam := r.URL.Query().Get("error")
	if errorParam != "" {
		errorDescription := r.URL.Query().Get("error_description")
		s.log.Error("OIDC callback error: %s - %s", errorParam, errorDescription)
		return nil, newLoginError(LoginErrorOIDCError)
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" {
		s.log.Error("Missing authorization code in OIDC callback")
		return nil, newLoginError(LoginErrorMissingCode)
	}

	return &oidcCallbackParams{code: code, state: state}, nil
}

// loginError represents a login error that should redirect to login page
type loginError struct {
	err LoginError
}

func (e *loginError) Error() string {
	return string(e.err)
}

// newLoginError creates a new login error
func newLoginError(err LoginError) *loginError {
	return &loginError{err: err}
}

// validateOIDCState validates the state parameter and clears the state cookie
func (s *Server) validateOIDCState(w http.ResponseWriter, r *http.Request, state string) error {
	stateCookie, err := r.Cookie(auth.CookieOIDCState)
	if err != nil || stateCookie == nil || stateCookie.Value != state {
		s.log.Error("Invalid OIDC state parameter")
		return newLoginError(LoginErrorInvalidState)
	}

	// Clear state cookie
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieOIDCState,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	return nil
}

// getOIDCProvider retrieves the OIDC provider from the discovery service
func (s *Server) getOIDCProvider(ctx context.Context) (*oidc.Provider, error) {
	if s.oidcDiscovery == nil {
		s.log.Error("OIDC discovery service not configured")
		return nil, newLoginError(LoginErrorConfigurationError)
	}

	provider, err := s.oidcDiscovery.GetProvider(ctx)
	if err != nil {
		s.log.Error("Failed to get OIDC provider: %v", err)
		return nil, newLoginError(LoginErrorProviderError)
	}

	return provider, nil
}

// buildOAuth2Config builds an OAuth2 configuration from the OIDC provider
func (s *Server) buildOAuth2Config(provider *oidc.Provider) oauth2.Config {
	return oauth2.Config{
		ClientID:     s.config.OIDC.ClientID,
		ClientSecret: s.config.OIDC.ClientSecret,
		RedirectURL:  s.config.OIDC.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       s.config.OIDC.Scopes,
	}
}

// exchangeOIDCCodeForTokens exchanges the authorization code for OAuth2 tokens
func (s *Server) exchangeOIDCCodeForTokens(ctx context.Context, oauth2Config oauth2.Config, code string) (*oauth2.Token, string, error) {
	token, err := oauth2Config.Exchange(ctx, code)
	if err != nil {
		s.log.Error("Failed to exchange authorization code for tokens: %v", err)
		return nil, "", newLoginError(LoginErrorTokenExchangeFailed)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		s.log.Error("Missing id_token in token response")
		return nil, "", newLoginError(LoginErrorMissingIDToken)
	}

	return token, rawIDToken, nil
}

// verifyAndExtractIDTokenClaims verifies the ID token and extracts user claims
func (s *Server) verifyAndExtractIDTokenClaims(ctx context.Context, provider *oidc.Provider, rawIDToken string) (*oidcClaims, error) {
	verifier := provider.Verifier(&oidc.Config{ClientID: s.config.OIDC.ClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		s.log.Error("Failed to verify ID token: %v", err)
		return nil, newLoginError(LoginErrorTokenVerificationFailed)
	}

	var claims oidcClaims
	if err := idToken.Claims(&claims); err != nil {
		s.log.Error("Failed to extract claims from ID token: %v", err)
		return nil, newLoginError(LoginErrorClaimsExtractionFailed)
	}

	return &claims, nil
}

// determineUsernameFromClaims determines the username from OIDC claims
func determineUsernameFromClaims(claims *oidcClaims) string {
	if claims.PreferredUsername != "" {
		return claims.PreferredUsername
	}
	if claims.Username != "" {
		return claims.Username
	}
	if claims.Email != "" {
		return claims.Email
	}
	return claims.Subject
}

// findOIDCUser attempts to find an existing user by sub, email, or username
// Returns the user, how they were matched, and any error
func (s *Server) findOIDCUser(ctx context.Context, claims *oidcClaims, username string) (*db.User, OIDCUserMatchMethod, error) {
	// Step 1: Check if user exists based on sub claim
	if claims.Subject != "" {
		userBySub, err := s.store.GetUserByOpenIDSub(ctx, claims.Subject)
		if err == nil {
			return userBySub, OIDCMatchBySub, nil
		} else if err.Error() != "user not found" {
			s.log.Error("Database error while looking up user by OpenID sub: %v", err)
			return nil, OIDCMatchNone, newLoginError(LoginErrorDatabaseError)
		}
	}

	// Step 2: If not found by sub, check by email address
	if claims.Email != "" {
		userByEmail, err := s.store.GetUserByEmail(ctx, claims.Email)
		if err == nil {
			if userByEmail.OpenIDSub != nil && *userByEmail.OpenIDSub != claims.Subject {
				s.log.Warn("OIDC login blocked - user found by email but has different OpenID sub: user_id=%s, username=%s, email=%s, existing_sub=%s, new_sub=%s", userByEmail.ID, userByEmail.Username, claims.Email, *userByEmail.OpenIDSub, claims.Subject)
				return nil, OIDCMatchNone, newLoginError(LoginErrorAccountLinkedToDifferentProvider)
			}

			// If user found by email but not by sub, require password verification before linking
			if userByEmail.OpenIDSub == nil {
				// Return user but indicate password verification is required
				// We'll handle the linking after password verification
				return userByEmail, OIDCMatchByEmail, newLoginError(LoginErrorPasswordVerificationRequired)
			}
			return userByEmail, OIDCMatchByEmail, nil
		} else if err.Error() != "user not found" {
			s.log.Error("Database error while looking up user by email: %v", err)
			return nil, OIDCMatchNone, newLoginError(LoginErrorDatabaseError)
		}
	}

	// Step 3: If still not found, check by username
	userByUsername, err := s.store.GetUserByUsername(ctx, username)
	if err == nil {
		if userByUsername.OpenIDSub != nil && *userByUsername.OpenIDSub != claims.Subject {
			s.log.Warn("OIDC login blocked - user found by username but has different OpenID sub: user_id=%s, username=%s, existing_sub=%s, new_sub=%s", userByUsername.ID, username, *userByUsername.OpenIDSub, claims.Subject)
			return nil, OIDCMatchNone, newLoginError(LoginErrorAccountLinkedToDifferentProvider)
		}

		if userByUsername.OpenIDSub == nil {
			userByUsername.OpenIDSub = &claims.Subject
			if err := s.store.UpdateUser(ctx, userByUsername); err != nil {
				s.log.Error("Failed to update user with OpenID sub: %v", err)
				return nil, OIDCMatchNone, newLoginError(LoginErrorDatabaseError)
			}
		}
		return userByUsername, OIDCMatchByUsername, nil
	} else if err.Error() != "user not found" {
		s.log.Error("Database error while looking up user by username: %v", err)
		return nil, OIDCMatchNone, newLoginError(LoginErrorDatabaseError)
	}

	return nil, OIDCMatchNone, nil // User not found
}

// createOIDCUser creates a new user from OIDC claims
func (s *Server) createOIDCUser(ctx context.Context, claims *oidcClaims, username string) (*db.User, error) {
	authConfig, _, err := s.store.GetAuthConfig(ctx)
	if err != nil {
		s.log.Error("Failed to get auth config: %v", err)
		return nil, newLoginError(LoginErrorConfigurationError)
	}

	userCount, err := s.store.CountUsers(ctx)
	if err != nil {
		s.log.Error("Failed to check user count: %v", err)
		return nil, newLoginError(LoginErrorDatabaseError)
	}

	shouldCreateDisabled := userCount > 0 && !authConfig.AllowRegistration

	var emailPtr *string
	if claims.Email != "" {
		emailPtr = &claims.Email
	}
	var openIDSubPtr *string
	if claims.Subject != "" {
		openIDSubPtr = &claims.Subject
	}

	randomPassword, err := generateRandomPassword()
	if err != nil {
		s.log.Error("Failed to generate random password: %v", err)
		return nil, newLoginError(LoginErrorPasswordGenerationFailed)
	}

	hashedPassword, err := auth.HashPassword(randomPassword)
	if err != nil {
		s.log.Error("Failed to hash password: %v", err)
		return nil, newLoginError(LoginErrorPasswordHashingFailed)
	}

	user := &db.User{
		ID:           uuid.New().String(),
		Username:     username,
		Email:        emailPtr,
		OpenIDSub:    openIDSubPtr,
		PasswordHash: hashedPassword,
		Role:         db.RoleViewer,
		IsActive:     !shouldCreateDisabled,
	}

	if err := s.store.CreateUser(ctx, user); err != nil {
		if err.Error() == "UNIQUE constraint failed: users.email" {
			s.log.Error("OIDC user creation failed - email already in use: %v", err)
			return nil, newLoginError(LoginErrorEmailAlreadyExists)
		}
		s.log.Error("Failed to create OIDC user: %v", err)
		return nil, newLoginError(LoginErrorUserCreationFailed)
	}

	s.log.Info("OIDC new user registered: user_id=%s, username=%s, email=%s, sub=%s, is_active=%v", user.ID, username, claims.Email, claims.Subject, !shouldCreateDisabled)

	if shouldCreateDisabled {
		s.log.Info("OIDC user created but disabled - registration is disabled: %s", username)
		return nil, newLoginError(LoginErrorAccountDisabled)
	}

	return user, nil
}

// createOIDCSessionAndCookies creates a session and sets all necessary cookies
func (s *Server) createOIDCSessionAndCookies(w http.ResponseWriter, r *http.Request, user *db.User, oauthToken *oauth2.Token) error {
	authConfig, _, _ := s.store.GetAuthConfig(r.Context())
	expiresAt := time.Now().Add(time.Duration(authConfig.SessionTimeout) * time.Second)

	tokenString, err := s.authManager.GenerateJWT(user, authConfig)
	if err != nil {
		s.log.Error("Failed to generate JWT token: %v", err)
		return newLoginError(LoginErrorTokenGenerationFailed)
	}

	session := &db.Session{
		UserID:    user.ID,
		Token:     tokenString,
		ExpiresAt: expiresAt,
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
	}

	if err := s.store.CreateSession(r.Context(), session); err != nil {
		s.log.Error("Failed to create session: %v", err)
		return newLoginError(LoginErrorSessionCreationFailed)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieAuthToken,
		Value:    tokenString,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
	})

	if oauthToken.RefreshToken != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     auth.CookieRefreshToken,
			Value:    oauthToken.RefreshToken,
			Path:     "/",
			Expires:  time.Now().Add(7 * 24 * time.Hour), // 7 days
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   r.TLS != nil,
		})
	}

	// Remove OIDC access and ID token from browser cookies by expiring them
	for _, name := range []string{
		auth.CookieOIDCAccessToken,
		auth.CookieOIDCIdToken,
	} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			Expires:  time.Now().Add(-time.Hour), // Expire in the past to delete cookie
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   r.TLS != nil,
		})
	}

	now := time.Now()
	user.LastLogin = &now
	if err := s.store.UpdateUser(r.Context(), user); err != nil {
		s.log.Error("Failed to update last login: %v", err)
	}

	return nil
}

// handleOIDCCallback handles the OIDC callback and exchanges the authorization code for tokens
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract and validate callback parameters
	params, err := s.extractOIDCCallbackParams(r)
	if err != nil {
		http.Redirect(w, r, "/login?error="+err.Error(), http.StatusFound)
		return
	}

	// Validate state
	if err := s.validateOIDCState(w, r, params.state); err != nil {
		http.Redirect(w, r, "/login?error="+err.Error(), http.StatusFound)
		return
	}

	// Get OIDC provider
	provider, err := s.getOIDCProvider(ctx)
	if err != nil {
		http.Redirect(w, r, "/login?error="+err.Error(), http.StatusFound)
		return
	}

	// Build OAuth2 config and exchange code for tokens
	oauth2Config := s.buildOAuth2Config(provider)
	oauthToken, rawIDToken, err := s.exchangeOIDCCodeForTokens(ctx, oauth2Config, params.code)
	if err != nil {
		http.Redirect(w, r, "/login?error="+err.Error(), http.StatusFound)
		return
	}

	// Verify ID token and extract claims
	claims, err := s.verifyAndExtractIDTokenClaims(ctx, provider, rawIDToken)
	if err != nil {
		http.Redirect(w, r, "/login?error="+err.Error(), http.StatusFound)
		return
	}

	// Determine username and find or create user
	username := determineUsernameFromClaims(claims)
	user, matchMethod, err := s.findOIDCUser(ctx, claims, username)

	// Check if password verification is required (user found by email but not by sub)
	if err != nil {
		if loginErr, ok := err.(*loginError); ok && loginErr.err == LoginErrorPasswordVerificationRequired {
			// Store OIDC callback data temporarily in a cookie for password verification
			callbackData := map[string]interface{}{
				"sub":          claims.Subject,
				"email":        claims.Email,
				"username":     username,
				"raw_id_token": rawIDToken,
			}
			callbackDataJSON, _ := json.Marshal(callbackData)
			callbackDataEncoded := base64.URLEncoding.EncodeToString(callbackDataJSON)

			// Store in cookie (expires in 10 minutes)
			http.SetCookie(w, &http.Cookie{
				Name:     auth.CookieOIDCState + "_verify",
				Value:    callbackDataEncoded,
				Path:     "/",
				MaxAge:   600, // 10 minutes
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
				Secure:   r.TLS != nil,
			})

			// Also store the OAuth token data temporarily
			oauthTokenJSON, _ := json.Marshal(oauthToken)
			oauthTokenEncoded := base64.URLEncoding.EncodeToString(oauthTokenJSON)
			http.SetCookie(w, &http.Cookie{
				Name:     auth.CookieOIDCState + "_token",
				Value:    oauthTokenEncoded,
				Path:     "/",
				MaxAge:   600, // 10 minutes
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
				Secure:   r.TLS != nil,
			})

			// Redirect to login page with password verification required flag
			http.Redirect(w, r, "/login?oidc_verify=true&email="+claims.Email, http.StatusFound)
			return
		}
		http.Redirect(w, r, "/login?error="+err.Error(), http.StatusFound)
		return
	}

	// If user not found, create user if registration is enabled
	if user == nil {
		s.log.Debug("OIDC user not found (match_method=%s), attempting to create if registration enabled", matchMethod)
		authConfig, _, err := s.store.GetAuthConfig(ctx)
		if err != nil {
			s.log.Error("Failed to get auth config: %v", err)
			http.Redirect(w, r, "/login?error="+string(LoginErrorConfigurationError), http.StatusFound)
			return
		}

		if authConfig.AllowRegistration {
			var createErr error
			user, createErr = s.createOIDCUser(ctx, claims, username)
			if createErr != nil {
				http.Redirect(w, r, "/login?error="+createErr.Error(), http.StatusFound)
				return
			}
		} else {
			// Registration is disabled and user doesn't exist
			s.log.Info("OIDC login blocked - user not found and registration is disabled: %s", username)
			http.Redirect(w, r, "/login?error="+string(LoginErrorInvalidCredentials), http.StatusFound)
			return
		}
	} else {
		s.log.Debug("OIDC user found via %s: user_id=%s, username=%s", matchMethod, user.ID, user.Username)
	}

	// Check if user is active
	if !user.IsActive {
		s.log.Info("OIDC login blocked - user account is disabled: %s", username)
		http.Redirect(w, r, "/login?error="+string(LoginErrorAccountDisabled), http.StatusFound)
		return
	}

	// Create session and set cookies
	if err := s.createOIDCSessionAndCookies(w, r, user, oauthToken); err != nil {
		http.Redirect(w, r, "/login?error="+err.Error(), http.StatusFound)
		return
	}

	// Redirect to login page
	http.Redirect(w, r, "/login", http.StatusFound)
}

// handleOIDCVerifyPassword verifies the user's password and completes OIDC account linking
func (s *Server) handleOIDCVerifyPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get stored OIDC callback data from cookie
	verifyCookie, err := r.Cookie(auth.CookieOIDCState + "_verify")
	if err != nil || verifyCookie == nil {
		s.respondError(w, http.StatusBadRequest, "OIDC verification session expired or invalid")
		return
	}

	// Decode callback data
	callbackDataJSON, err := base64.URLEncoding.DecodeString(verifyCookie.Value)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid OIDC verification data")
		return
	}

	var callbackData map[string]interface{}
	if err := json.Unmarshal(callbackDataJSON, &callbackData); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid OIDC verification data")
		return
	}

	email, ok := callbackData["email"].(string)
	if !ok || email == "" {
		s.respondError(w, http.StatusBadRequest, "Missing email in verification data")
		return
	}

	// Get user by email
	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "User not found")
		return
	}

	// Parse password from request
	var req OIDCVerifyPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Verify password
	if !auth.CheckPassword(user.PasswordHash, req.Password) {
		s.respondError(w, http.StatusUnauthorized, "Invalid password")
		return
	}

	// Get OAuth token data from cookie
	tokenCookie, err := r.Cookie(auth.CookieOIDCState + "_token")
	if err != nil || tokenCookie == nil {
		s.respondError(w, http.StatusBadRequest, "OIDC token data expired or invalid")
		return
	}

	// Decode OAuth token data
	oauthTokenJSON, err := base64.URLEncoding.DecodeString(tokenCookie.Value)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid OIDC token data")
		return
	}

	var oauthToken oauth2.Token
	if err := json.Unmarshal(oauthTokenJSON, &oauthToken); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid OIDC token data")
		return
	}

	// Link the sub claim to the user
	sub, ok := callbackData["sub"].(string)
	if !ok || sub == "" {
		s.respondError(w, http.StatusBadRequest, "Missing sub claim in verification data")
		return
	}

	user.OpenIDSub = &sub
	if err := s.store.UpdateUser(ctx, user); err != nil {
		s.log.Error("Failed to link OIDC sub to user: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to link account")
		return
	}

	s.log.Info("OIDC account linked after password verification: user_id=%s, username=%s, email=%s, sub=%s", user.ID, user.Username, email, sub)

	// Check if user is active
	if !user.IsActive {
		s.respondError(w, http.StatusForbidden, "User account is disabled")
		return
	}

	// Create session and set cookies
	if err := s.createOIDCSessionAndCookies(w, r, user, &oauthToken); err != nil {
		s.respondError(w, http.StatusInternalServerError, "Failed to create session")
		return
	}

	// Clear verification cookies
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieOIDCState + "_verify",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieOIDCState + "_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	// Get auth config for session timeout
	authConfig, _, _ := s.store.GetAuthConfig(r.Context())
	expiresAt := time.Now().Add(time.Duration(authConfig.SessionTimeout) * time.Second)

	s.respondJSON(w, http.StatusOK, LoginResponse{
		Token:     "", // Token is in cookie
		User:      user,
		ExpiresAt: expiresAt,
	})
}

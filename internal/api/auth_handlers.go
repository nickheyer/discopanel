package api

import (
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

// handleOIDCCallback handles the OIDC callback and exchanges the authorization code for tokens
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	// Get authorization code and state from query parameters
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")

	// Check for OAuth error
	if errorParam != "" {
		errorDescription := r.URL.Query().Get("error_description")
		s.log.Error("OIDC callback error: %s - %s", errorParam, errorDescription)
		http.Redirect(w, r, "/login?error=oidc_error", http.StatusFound)
		return
	}

	// Validate state
	stateCookie, err := r.Cookie(auth.CookieOIDCState)
	if err != nil || stateCookie == nil || stateCookie.Value != state {
		s.log.Error("Invalid OIDC state parameter")
		http.Redirect(w, r, "/login?error=invalid_state", http.StatusFound)
		return
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

	if code == "" {
		s.log.Error("Missing authorization code in OIDC callback")
		http.Redirect(w, r, "/login?error=missing_code", http.StatusFound)
		return
	}

	// Check if OIDC discovery service is available
	if s.oidcDiscovery == nil {
		s.log.Error("OIDC discovery service not configured")
		http.Redirect(w, r, "/login?error=configuration_error", http.StatusFound)
		return
	}

	// Get OIDC provider
	provider, err := s.oidcDiscovery.GetProvider(r.Context())
	if err != nil {
		s.log.Error("Failed to get OIDC provider: %v", err)
		http.Redirect(w, r, "/login?error=provider_error", http.StatusFound)
		return
	}

	// Build OAuth2 config
	oauth2Config := oauth2.Config{
		ClientID:     s.config.OIDC.ClientID,
		ClientSecret: s.config.OIDC.ClientSecret,
		RedirectURL:  s.config.OIDC.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       s.config.OIDC.Scopes,
	}

	// Exchange authorization code for tokens
	ctx := r.Context()
	token, err := oauth2Config.Exchange(ctx, code)
	if err != nil {
		s.log.Error("Failed to exchange authorization code for tokens: %v", err)
		http.Redirect(w, r, "/login?error=token_exchange_failed", http.StatusFound)
		return
	}

	// Extract ID token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		s.log.Error("Missing id_token in token response")
		http.Redirect(w, r, "/login?error=missing_id_token", http.StatusFound)
		return
	}

	// Verify and parse ID token
	verifier := provider.Verifier(&oidc.Config{ClientID: s.config.OIDC.ClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		s.log.Error("Failed to verify ID token: %v", err)
		http.Redirect(w, r, "/login?error=token_verification_failed", http.StatusFound)
		return
	}

	// Extract user claims
	var claims struct {
		Subject           string `json:"sub"`
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
		Username          string `json:"username"`
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
	}

	if err := idToken.Claims(&claims); err != nil {
		s.log.Error("Failed to extract claims from ID token: %v", err)
		http.Redirect(w, r, "/login?error=claims_extraction_failed", http.StatusFound)
		return
	}

	// Determine username (prefer preferred_username, fallback to email or sub)
	username := claims.PreferredUsername
	if username == "" {
		username = claims.Username
	}
	if username == "" {
		username = claims.Email
	}
	if username == "" {
		username = claims.Subject
	}

	// Determine user role from claims (check both groups and roles)
	userRole := db.RoleViewer // Default role

	var user *db.User

	// OIDC Login Flow: Try to find existing user
	// Step 1: Check if user exists based on sub claim
	if claims.Subject != "" {
		userBySub, err := s.store.GetUserByOpenIDSub(r.Context(), claims.Subject)
		if err == nil {
			user = userBySub
			s.log.Info("OIDC user matched by OpenID sub claim: user_id=%s, username=%s, sub=%s", user.ID, user.Username, claims.Subject)
		} else if err.Error() != "user not found" {
			s.log.Error("Database error while looking up user by OpenID sub: %v", err)
			http.Redirect(w, r, "/login?error=database_error", http.StatusFound)
			return
		}
	}

	// Step 2: If not found by sub, check by email address
	if user == nil && claims.Email != "" {
		userByEmail, err := s.store.GetUserByEmail(r.Context(), claims.Email)
		if err == nil {
			// User found by email - check if sub claim is already set
			if userByEmail.OpenIDSub != nil {
				// User already has a sub claim set - check if it matches
				if *userByEmail.OpenIDSub != claims.Subject {
					// Sub claim doesn't match - this is a security issue
					// Fail login to prevent account takeover
					s.log.Warn("OIDC login blocked - user found by email but has different OpenID sub: user_id=%s, username=%s, email=%s, existing_sub=%s, new_sub=%s", userByEmail.ID, userByEmail.Username, claims.Email, *userByEmail.OpenIDSub, claims.Subject)
					http.Redirect(w, r, "/login?error=account_linked_to_different_provider", http.StatusFound)
					return
				}
				// Sub claim matches - proceed with login
				user = userByEmail
				s.log.Info("OIDC user matched by email address with matching sub: user_id=%s, username=%s, email=%s, sub=%s", user.ID, user.Username, claims.Email, claims.Subject)
			} else {
				// User found by email but no sub claim - set it and proceed
				user = userByEmail
				user.OpenIDSub = &claims.Subject
				if err := s.store.UpdateUser(r.Context(), user); err != nil {
					s.log.Error("Failed to update user with OpenID sub: %v", err)
					http.Redirect(w, r, "/login?error=database_error", http.StatusFound)
					return
				}
				s.log.Info("OIDC user matched by email address and linked with sub claim: user_id=%s, username=%s, email=%s, sub=%s", user.ID, user.Username, claims.Email, claims.Subject)
			}
		} else if err.Error() != "user not found" {
			s.log.Error("Database error while looking up user by email: %v", err)
			http.Redirect(w, r, "/login?error=database_error", http.StatusFound)
			return
		}
	}

	// Step 3: If still not found, check by username
	if user == nil {
		userByUsername, err := s.store.GetUserByUsername(r.Context(), username)
		if err == nil {
			// User found by username - check if sub claim is already set
			if userByUsername.OpenIDSub != nil {
				// User already has a sub claim set - check if it matches
				if *userByUsername.OpenIDSub != claims.Subject {
					// Sub claim doesn't match - this is a security issue
					// Fail login to prevent account takeover
					s.log.Warn("OIDC login blocked - user found by username but has different OpenID sub: user_id=%s, username=%s, existing_sub=%s, new_sub=%s", userByUsername.ID, username, *userByUsername.OpenIDSub, claims.Subject)
					http.Redirect(w, r, "/login?error=account_linked_to_different_provider", http.StatusFound)
					return
				}
				// Sub claim matches - proceed with login
				user = userByUsername
				s.log.Info("OIDC user matched by username with matching sub: user_id=%s, username=%s, sub=%s", user.ID, username, claims.Subject)
			} else {
				// User found by username but no sub claim - set it and proceed
				user = userByUsername
				user.OpenIDSub = &claims.Subject
				if err := s.store.UpdateUser(r.Context(), user); err != nil {
					s.log.Error("Failed to update user with OpenID sub: %v", err)
					http.Redirect(w, r, "/login?error=database_error", http.StatusFound)
					return
				}
				s.log.Info("OIDC user matched by username and linked with sub claim: user_id=%s, username=%s, sub=%s", user.ID, username, claims.Subject)
			}
		} else if err.Error() != "user not found" {
			s.log.Error("Database error while looking up user by username: %v", err)
			http.Redirect(w, r, "/login?error=database_error", http.StatusFound)
			return
		}
	}

	// OIDC Register Flow: Create new user if not found
	if user == nil {
		// Check if registration is enabled
		authConfig, _, err := s.store.GetAuthConfig(r.Context())
		if err != nil {
			s.log.Error("Failed to get auth config: %v", err)
			http.Redirect(w, r, "/login?error=configuration_error", http.StatusFound)
			return
		}

		// Check user count to allow first user even if registration is disabled
		userCount, err := s.store.CountUsers(r.Context())
		if err != nil {
			s.log.Error("Failed to check user count: %v", err)
			http.Redirect(w, r, "/login?error=database_error", http.StatusFound)
			return
		}

		// Determine if user should be created as disabled
		shouldCreateDisabled := userCount > 0 && !authConfig.AllowRegistration

		// Create new user with username, email, and sub claim from IDToken
		var emailPtr *string
		if claims.Email != "" {
			emailPtr = &claims.Email
		}
		var openIDSubPtr *string
		if claims.Subject != "" {
			openIDSubPtr = &claims.Subject
		}

		// Generate a random password for OIDC users (required field but not used for OIDC auth)
		randomPassword, err := generateRandomPassword()
		if err != nil {
			s.log.Error("Failed to generate random password: %v", err)
			http.Redirect(w, r, "/login?error=password_generation_failed", http.StatusFound)
			return
		}
		hashedPassword, err := auth.HashPassword(randomPassword)
		if err != nil {
			s.log.Error("Failed to hash password: %v", err)
			http.Redirect(w, r, "/login?error=password_hashing_failed", http.StatusFound)
			return
		}

		user = &db.User{
			ID:           uuid.New().String(),
			Username:     username,
			Email:        emailPtr,
			OpenIDSub:    openIDSubPtr,
			PasswordHash: hashedPassword,
			Role:         userRole,
			IsActive:     !shouldCreateDisabled, // Disable if registration is disabled
		}

		if err := s.store.CreateUser(r.Context(), user); err != nil {
			// Check if error is UNIQUE constraint on email
			if err.Error() == "UNIQUE constraint failed: users.email" {
				s.log.Error("OIDC user creation failed - email already in use: %v", err)
				http.Redirect(w, r, "/login?error=email_already_exists", http.StatusFound)
				return
			}
			s.log.Error("Failed to create OIDC user: %v", err)
			http.Redirect(w, r, "/login?error=user_creation_failed", http.StatusFound)
			return
		}

		s.log.Info("OIDC new user registered: user_id=%s, username=%s, email=%s, sub=%s, is_active=%v", user.ID, username, claims.Email, claims.Subject, !shouldCreateDisabled)

		// If user was created as disabled, redirect with appropriate message
		if shouldCreateDisabled {
			s.log.Info("OIDC user created but disabled - registration is disabled: %s", username)
			http.Redirect(w, r, "/login?error=account_disabled", http.StatusFound)
			return
		}
	}

	// At this point, user is found or created - check if active
	if !user.IsActive {
		s.log.Info("OIDC login blocked - user account is disabled: %s", username)
		http.Redirect(w, r, "/login?error=account_disabled", http.StatusFound)
		return
	}

	// Get auth config for session timeout
	authConfig, _, _ := s.store.GetAuthConfig(r.Context())
	expiresAt := time.Now().Add(time.Duration(authConfig.SessionTimeout) * time.Second)

	// Generate JWT token for session
	tokenString, err := s.authManager.GenerateJWT(user, authConfig)
	if err != nil {
		s.log.Error("Failed to generate JWT token: %v", err)
		http.Redirect(w, r, "/login?error=token_generation_failed", http.StatusFound)
		return
	}

	// Create session
	session := &db.Session{
		UserID:    user.ID,
		Token:     tokenString,
		ExpiresAt: expiresAt,
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
	}
	if err := s.store.CreateSession(r.Context(), session); err != nil {
		s.log.Error("Failed to create session: %v", err)
		http.Redirect(w, r, "/login?error=session_creation_failed", http.StatusFound)
		return
	}

	// Set auth token cookie
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieAuthToken,
		Value:    tokenString,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
	})

	// Set refresh token if exists
	if token.RefreshToken != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     auth.CookieRefreshToken,
			Value:    token.RefreshToken,
			Path:     "/",
			Expires:  time.Now().Add(30 * 24 * time.Hour), // 30 days
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   r.TLS != nil,
		})
	}

	for _, name := range []string{
		auth.CookieOIDCAccessToken,
		auth.CookieOIDCIdToken,
	} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			Expires:  time.Now().Add(-time.Hour),
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
	}

	// Update last login
	now := time.Now()
	user.LastLogin = &now
	if err := s.store.UpdateUser(r.Context(), user); err != nil {
		s.log.Error("Failed to update last login: %v", err)
	}

	// Redirect to /login (frontend will handle the redirect)
	http.Redirect(w, r, "/login", http.StatusFound)
}

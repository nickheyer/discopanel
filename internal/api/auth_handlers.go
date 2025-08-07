package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/nickheyer/discopanel/internal/auth"
	"github.com/nickheyer/discopanel/internal/db"
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
		Name:     "auth_token",
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
		cookie, err := r.Cookie("auth_token")
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

	// Clear cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		Path:     "/",
		Expires:  time.Now().Add(-time.Hour),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

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

	if err := s.store.SaveAuthConfig(r.Context(), config); err != nil {
		s.respondError(w, http.StatusInternalServerError, "Failed to update auth config")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]string{"message": "Auth config updated successfully"})
}

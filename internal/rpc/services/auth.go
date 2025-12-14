package services

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/nickheyer/discopanel/internal/auth"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Compile-time check that AuthService implements the interface
var _ discopanelv1connect.AuthServiceHandler = (*AuthService)(nil)

// AuthService implements the Auth service
type AuthService struct {
	store       *storage.Store
	authManager *auth.Manager
	log         *logger.Logger
}

// NewAuthService creates a new auth service
func NewAuthService(store *storage.Store, authManager *auth.Manager, log *logger.Logger) *AuthService {
	return &AuthService{
		store:       store,
		authManager: authManager,
		log:         log,
	}
}

// Helper functions for auth service

// dbUserToProto converts a database User to a proto User
func dbUserToProto(user *storage.User) *v1.User {
	if user == nil {
		return nil
	}

	protoUser := &v1.User{
		Id:        user.ID,
		Username:  user.Username,
		IsActive:  user.IsActive,
		CreatedAt: timestamppb.New(user.CreatedAt),
		UpdatedAt: timestamppb.New(user.UpdatedAt),
	}

	// Map role
	switch user.Role {
	case storage.RoleAdmin:
		protoUser.Role = v1.UserRole_USER_ROLE_ADMIN
	case storage.RoleEditor:
		protoUser.Role = v1.UserRole_USER_ROLE_EDITOR
	case storage.RoleViewer:
		protoUser.Role = v1.UserRole_USER_ROLE_VIEWER
	default:
		protoUser.Role = v1.UserRole_USER_ROLE_UNSPECIFIED
	}

	// Handle optional email
	if user.Email != nil && *user.Email != "" {
		protoUser.Email = user.Email
	}

	// Note: recovery_key is intentionally not included in the proto response for security reasons
	// It should only be shown to the user when first created

	return protoUser
}

// protoRoleToDBRole converts a proto UserRole to a DB UserRole
func protoRoleToDBRole(role v1.UserRole) storage.UserRole {
	switch role {
	case v1.UserRole_USER_ROLE_ADMIN:
		return storage.RoleAdmin
	case v1.UserRole_USER_ROLE_EDITOR:
		return storage.RoleEditor
	case v1.UserRole_USER_ROLE_VIEWER:
		return storage.RoleViewer
	default:
		return storage.RoleViewer
	}
}

// extractTokenFromHeaders extracts the auth token from the request headers
func extractTokenFromHeaders(headers http.Header) string {
	// Try Authorization header first
	authHeader := headers.Get("Authorization")
	if authHeader != "" && len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		return authHeader[7:]
	}

	// Try cookie
	cookies := headers.Values("Cookie")
	for _, cookie := range cookies {
		if len(cookie) > 11 && cookie[:11] == "auth_token=" {
			// Simple cookie parsing - find the value up to semicolon or end
			value := cookie[11:]
			if idx := indexByte(value, ';'); idx >= 0 {
				value = value[:idx]
			}
			return value
		}
	}

	return ""
}

// indexByte returns the index of the first instance of c in s, or -1 if not present
func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// GetAuthStatus checks if auth is enabled
func (s *AuthService) GetAuthStatus(ctx context.Context, req *connect.Request[v1.GetAuthStatusRequest]) (*connect.Response[v1.GetAuthStatusResponse], error) {
	authConfig, _, err := s.store.GetAuthConfig(ctx)
	if err != nil {
		s.log.Error("Failed to get auth config: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get auth config"))
	}

	// Check if this is first user setup
	userCount, err := s.store.CountUsers(ctx)
	if err != nil {
		s.log.Error("Failed to count users: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to check user count"))
	}

	return connect.NewResponse(&v1.GetAuthStatusResponse{
		Enabled:           authConfig.Enabled,
		FirstUserSetup:    userCount == 0,
		AllowRegistration: authConfig.AllowRegistration,
	}), nil
}

// Login authenticates user credentials
func (s *AuthService) Login(ctx context.Context, req *connect.Request[v1.LoginRequest]) (*connect.Response[v1.LoginResponse], error) {
	msg := req.Msg

	// Validate input
	if msg.Username == "" || msg.Password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("username and password are required"))
	}

	// Attempt login
	user, token, err := s.authManager.Login(ctx, msg.Username, msg.Password)
	if err != nil {
		switch err {
		case auth.ErrInvalidCredentials:
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid credentials"))
		case auth.ErrUserNotActive:
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("user account is not active"))
		case auth.ErrAuthDisabled:
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("authentication is disabled"))
		default:
			s.log.Error("Login error: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("login failed"))
		}
	}

	// Get auth config for session timeout
	authConfig, _, _ := s.store.GetAuthConfig(ctx)
	expiresAt := time.Now().Add(time.Duration(authConfig.SessionTimeout) * time.Second)

	// Set cookie in response headers
	resp := connect.NewResponse(&v1.LoginResponse{
		Token:     token,
		User:      dbUserToProto(user),
		ExpiresAt: timestamppb.New(expiresAt),
	})

	// Set auth cookie
	cookie := &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
	resp.Header().Set("Set-Cookie", cookie.String())

	return resp, nil
}

// Logout invalidates session token
func (s *AuthService) Logout(ctx context.Context, req *connect.Request[v1.LogoutRequest]) (*connect.Response[v1.LogoutResponse], error) {
	// Extract token from headers
	token := extractTokenFromHeaders(req.Header())

	if token != "" {
		// Delete session
		if err := s.authManager.Logout(ctx, token); err != nil {
			s.log.Error("Logout error: %v", err)
			// Don't fail the logout request even if session deletion fails
		}
	}

	// Clear cookie in response
	resp := connect.NewResponse(&v1.LogoutResponse{
		Message: "Logged out successfully",
	})

	// Clear auth cookie
	cookie := &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		Path:     "/",
		Expires:  time.Now().Add(-time.Hour),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
	resp.Header().Set("Set-Cookie", cookie.String())

	return resp, nil
}

// Register creates a new user account
func (s *AuthService) Register(ctx context.Context, req *connect.Request[v1.RegisterRequest]) (*connect.Response[v1.RegisterResponse], error) {
	msg := req.Msg

	// Validate input
	if msg.Username == "" || msg.Password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("username and password are required"))
	}

	// Check if this is first user setup
	userCount, err := s.store.CountUsers(ctx)
	if err != nil {
		s.log.Error("Failed to check user count: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to check user count"))
	}

	// Determine role
	var role storage.UserRole
	if userCount == 0 {
		// First user is always admin
		role = storage.RoleAdmin
	} else {
		// Check if registration is allowed
		authConfig, _, err := s.store.GetAuthConfig(ctx)
		if err != nil {
			s.log.Error("Failed to get auth config: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get auth config"))
		}

		if !authConfig.AllowRegistration {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("registration is disabled"))
		}

		// New users default to viewer role
		role = storage.RoleViewer
	}

	// Create user
	user, err := s.authManager.CreateUser(ctx, msg.Username, msg.Email, msg.Password, role)
	if err != nil {
		s.log.Error("Failed to create user: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create user"))
	}

	// If this was the first user, enable authentication
	if userCount == 0 {
		authConfig, _, _ := s.store.GetAuthConfig(ctx)
		authConfig.Enabled = true
		if err := s.store.SaveAuthConfig(ctx, authConfig); err != nil {
			s.log.Error("Failed to enable authentication: %v", err)
		}
	}

	return connect.NewResponse(&v1.RegisterResponse{
		User: dbUserToProto(user),
	}), nil
}

// ResetPassword resets password with recovery key
func (s *AuthService) ResetPassword(ctx context.Context, req *connect.Request[v1.ResetPasswordRequest]) (*connect.Response[v1.ResetPasswordResponse], error) {
	msg := req.Msg

	if err := s.authManager.ResetPassword(ctx, msg.Username, msg.RecoveryKey, msg.NewPassword); err != nil {
		if err == auth.ErrInvalidCredentials {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid recovery key or username"))
		}
		s.log.Error("Failed to reset password: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to reset password"))
	}

	return connect.NewResponse(&v1.ResetPasswordResponse{
		Message: "Password reset successfully",
	}), nil
}

// GetAuthConfig gets auth configuration
func (s *AuthService) GetAuthConfig(ctx context.Context, req *connect.Request[v1.GetAuthConfigRequest]) (*connect.Response[v1.GetAuthConfigResponse], error) {
	config, _, err := s.store.GetAuthConfig(ctx)
	if err != nil {
		s.log.Error("Failed to get auth config: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get auth config"))
	}

	// If auth is enabled, check for admin permission
	if config.Enabled {
		user := auth.GetUserFromContext(ctx)
		if user == nil || user.Role != storage.RoleAdmin {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("admin access required"))
		}
	}

	return connect.NewResponse(&v1.GetAuthConfigResponse{
		Enabled:            config.Enabled,
		SessionTimeout:     int32(config.SessionTimeout),
		RequireEmailVerify: config.RequireEmailVerify,
		AllowRegistration:  config.AllowRegistration,
	}), nil
}

// UpdateAuthConfig modifies auth configuration
func (s *AuthService) UpdateAuthConfig(ctx context.Context, req *connect.Request[v1.UpdateAuthConfigRequest]) (*connect.Response[v1.UpdateAuthConfigResponse], error) {
	msg := req.Msg

	config, _, err := s.store.GetAuthConfig(ctx)
	if err != nil {
		s.log.Error("Failed to get auth config: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get auth config"))
	}

	// If auth is currently enabled, require admin permission
	if config.Enabled {
		user := auth.GetUserFromContext(ctx)
		if user == nil || user.Role != storage.RoleAdmin {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("admin access required"))
		}
	}

	// Check if trying to enable auth for the first time
	requiresFirstUser := false
	if msg.Enabled != nil && *msg.Enabled && !config.Enabled {
		// Check if any users exist
		userCount, err := s.store.CountUsers(ctx)
		if err != nil {
			s.log.Error("Failed to check user count: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to check user count"))
		}

		if userCount == 0 {
			// Need to create first admin user
			requiresFirstUser = true
			return connect.NewResponse(&v1.UpdateAuthConfigResponse{
				Message:           "Create an admin account to enable authentication",
				RequiresFirstUser: requiresFirstUser,
			}), nil
		}
	}

	// Update allowed fields
	if msg.Enabled != nil {
		config.Enabled = *msg.Enabled
	}
	if msg.SessionTimeout != nil {
		config.SessionTimeout = int(*msg.SessionTimeout)
	}
	if msg.RequireEmailVerify != nil {
		config.RequireEmailVerify = *msg.RequireEmailVerify
	}
	if msg.AllowRegistration != nil {
		config.AllowRegistration = *msg.AllowRegistration
	}

	if err := s.store.SaveAuthConfig(ctx, config); err != nil {
		s.log.Error("Failed to update auth config: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to update auth config"))
	}

	return connect.NewResponse(&v1.UpdateAuthConfigResponse{
		Message:           "Auth config updated successfully",
		RequiresFirstUser: false,
	}), nil
}

// GetCurrentUser gets authenticated user info
func (s *AuthService) GetCurrentUser(ctx context.Context, req *connect.Request[v1.GetCurrentUserRequest]) (*connect.Response[v1.GetCurrentUserResponse], error) {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("not authenticated"))
	}

	return connect.NewResponse(&v1.GetCurrentUserResponse{
		User: dbUserToProto(user),
	}), nil
}

// ChangePassword changes user's own password
func (s *AuthService) ChangePassword(ctx context.Context, req *connect.Request[v1.ChangePasswordRequest]) (*connect.Response[v1.ChangePasswordResponse], error) {
	msg := req.Msg

	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("not authenticated"))
	}

	if err := s.authManager.ChangePassword(ctx, user.ID, msg.OldPassword, msg.NewPassword); err != nil {
		if err == auth.ErrInvalidCredentials {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid old password"))
		}
		s.log.Error("Failed to change password: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to change password"))
	}

	return connect.NewResponse(&v1.ChangePasswordResponse{
		Message: "Password changed successfully",
	}), nil
}

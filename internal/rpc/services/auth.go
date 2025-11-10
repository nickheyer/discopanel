package services

import (
	"context"

	"connectrpc.com/connect"
	"github.com/nickheyer/discopanel/internal/auth"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
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

// GetAuthStatus checks if auth is enabled
func (s *AuthService) GetAuthStatus(ctx context.Context, req *connect.Request[v1.GetAuthStatusRequest]) (*connect.Response[v1.GetAuthStatusResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// Login authenticates user credentials
func (s *AuthService) Login(ctx context.Context, req *connect.Request[v1.LoginRequest]) (*connect.Response[v1.LoginResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// Logout invalidates session token
func (s *AuthService) Logout(ctx context.Context, req *connect.Request[v1.LogoutRequest]) (*connect.Response[v1.LogoutResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// Register creates a new user account
func (s *AuthService) Register(ctx context.Context, req *connect.Request[v1.RegisterRequest]) (*connect.Response[v1.RegisterResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// ResetPassword resets password with recovery key
func (s *AuthService) ResetPassword(ctx context.Context, req *connect.Request[v1.ResetPasswordRequest]) (*connect.Response[v1.ResetPasswordResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetAuthConfig gets auth configuration
func (s *AuthService) GetAuthConfig(ctx context.Context, req *connect.Request[v1.GetAuthConfigRequest]) (*connect.Response[v1.GetAuthConfigResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// UpdateAuthConfig modifies auth configuration
func (s *AuthService) UpdateAuthConfig(ctx context.Context, req *connect.Request[v1.UpdateAuthConfigRequest]) (*connect.Response[v1.UpdateAuthConfigResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetCurrentUser gets authenticated user info
func (s *AuthService) GetCurrentUser(ctx context.Context, req *connect.Request[v1.GetCurrentUserRequest]) (*connect.Response[v1.GetCurrentUserResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// ChangePassword changes user's own password
func (s *AuthService) ChangePassword(ctx context.Context, req *connect.Request[v1.ChangePasswordRequest]) (*connect.Response[v1.ChangePasswordResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}
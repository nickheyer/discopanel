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

// Compile-time check that UserService implements the interface
var _ discopanelv1connect.UserServiceHandler = (*UserService)(nil)

// UserService implements the User service
type UserService struct {
	store       *storage.Store
	authManager *auth.Manager
	log         *logger.Logger
}

// NewUserService creates a new user service
func NewUserService(store *storage.Store, authManager *auth.Manager, log *logger.Logger) *UserService {
	return &UserService{
		store:       store,
		authManager: authManager,
		log:         log,
	}
}

// ListUsers lists all users
func (s *UserService) ListUsers(ctx context.Context, req *connect.Request[v1.ListUsersRequest]) (*connect.Response[v1.ListUsersResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// CreateUser creates a new user
func (s *UserService) CreateUser(ctx context.Context, req *connect.Request[v1.CreateUserRequest]) (*connect.Response[v1.CreateUserResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// UpdateUser updates a user
func (s *UserService) UpdateUser(ctx context.Context, req *connect.Request[v1.UpdateUserRequest]) (*connect.Response[v1.UpdateUserResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// DeleteUser deletes a user
func (s *UserService) DeleteUser(ctx context.Context, req *connect.Request[v1.DeleteUserRequest]) (*connect.Response[v1.DeleteUserResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}
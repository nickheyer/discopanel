package services

import (
	"context"
	"errors"

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

// ListUsers lists all users (admin only)
func (s *UserService) ListUsers(ctx context.Context, req *connect.Request[v1.ListUsersRequest]) (*connect.Response[v1.ListUsersResponse], error) {
	// Check admin permission
	user := auth.GetUserFromContext(ctx)
	if user == nil || user.Role != storage.RoleAdmin {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("admin access required"))
	}

	users, err := s.store.ListUsers(ctx)
	if err != nil {
		s.log.Error("Failed to list users: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to list users"))
	}

	// Convert DB users to proto users
	protoUsers := make([]*v1.User, len(users))
	for i, u := range users {
		protoUsers[i] = dbUserToProto(u)
	}

	return connect.NewResponse(&v1.ListUsersResponse{
		Users: protoUsers,
	}), nil
}

// CreateUser creates a new user (admin only)
func (s *UserService) CreateUser(ctx context.Context, req *connect.Request[v1.CreateUserRequest]) (*connect.Response[v1.CreateUserResponse], error) {
	// Check admin permission
	user := auth.GetUserFromContext(ctx)
	if user == nil || user.Role != storage.RoleAdmin {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("admin access required"))
	}

	msg := req.Msg

	// Validate role
	role := protoRoleToDBRole(msg.Role)
	if role != storage.RoleAdmin && role != storage.RoleEditor && role != storage.RoleViewer {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid role"))
	}

	// Create user
	newUser, err := s.authManager.CreateUser(ctx, msg.Username, msg.Email, msg.Password, role)
	if err != nil {
		s.log.Error("Failed to create user: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create user"))
	}

	return connect.NewResponse(&v1.CreateUserResponse{
		User: dbUserToProto(newUser),
	}), nil
}

// UpdateUser updates a user (admin only)
func (s *UserService) UpdateUser(ctx context.Context, req *connect.Request[v1.UpdateUserRequest]) (*connect.Response[v1.UpdateUserResponse], error) {
	// Check admin permission
	currentUser := auth.GetUserFromContext(ctx)
	if currentUser == nil || currentUser.Role != storage.RoleAdmin {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("admin access required"))
	}

	msg := req.Msg

	// Get the user to update
	user, err := s.store.GetUser(ctx, msg.Id)
	if err != nil {
		s.log.Error("Failed to get user: %v", err)
		return nil, connect.NewError(connect.CodeNotFound, errors.New("user not found"))
	}

	// Update fields if provided
	if msg.Email != nil {
		if *msg.Email == "" {
			user.Email = nil // Allow clearing email
		} else {
			user.Email = msg.Email
		}
	}
	if msg.Role != nil {
		user.Role = protoRoleToDBRole(*msg.Role)
	}
	if msg.IsActive != nil {
		user.IsActive = *msg.IsActive
	}

	if err := s.store.UpdateUser(ctx, user); err != nil {
		s.log.Error("Failed to update user: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to update user"))
	}

	return connect.NewResponse(&v1.UpdateUserResponse{
		User: dbUserToProto(user),
	}), nil
}

// DeleteUser deletes a user (admin only)
func (s *UserService) DeleteUser(ctx context.Context, req *connect.Request[v1.DeleteUserRequest]) (*connect.Response[v1.DeleteUserResponse], error) {
	// Check admin permission
	currentUser := auth.GetUserFromContext(ctx)
	if currentUser == nil || currentUser.Role != storage.RoleAdmin {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("admin access required"))
	}

	msg := req.Msg

	// Prevent self-deletion
	if currentUser.ID == msg.Id {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cannot delete your own account"))
	}

	if err := s.store.DeleteUser(ctx, msg.Id); err != nil {
		s.log.Error("Failed to delete user: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to delete user"))
	}

	return connect.NewResponse(&v1.DeleteUserResponse{
		Message: "User deleted successfully",
	}), nil
}
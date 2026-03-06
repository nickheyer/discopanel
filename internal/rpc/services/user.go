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
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ discopanelv1connect.UserServiceHandler = (*UserService)(nil)

type UserService struct {
	store       *storage.Store
	authManager *auth.Manager
	log         *logger.Logger
}

func NewUserService(store *storage.Store, authManager *auth.Manager, log *logger.Logger) *UserService {
	return &UserService{
		store:       store,
		authManager: authManager,
		log:         log,
	}
}

func (s *UserService) ListUsers(ctx context.Context, req *connect.Request[v1.ListUsersRequest]) (*connect.Response[v1.ListUsersResponse], error) {
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		s.log.Error("Failed to list users: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to list users"))
	}

	protoUsers := make([]*v1.User, 0, len(users))
	for _, user := range users {
		roles, _ := s.store.GetUserRoleNames(ctx, user.ID)
		protoUsers = append(protoUsers, dbUserToProto(user, roles))
	}

	return connect.NewResponse(&v1.ListUsersResponse{
		Users: protoUsers,
	}), nil
}

func (s *UserService) GetUser(ctx context.Context, req *connect.Request[v1.GetUserRequest]) (*connect.Response[v1.GetUserResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user ID is required"))
	}

	user, err := s.store.GetUser(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("user not found"))
	}

	roles, _ := s.store.GetUserRoleNames(ctx, user.ID)

	return connect.NewResponse(&v1.GetUserResponse{
		User: dbUserToProto(user, roles),
	}), nil
}

func (s *UserService) CreateUser(ctx context.Context, req *connect.Request[v1.CreateUserRequest]) (*connect.Response[v1.CreateUserResponse], error) {
	msg := req.Msg

	if msg.Username == "" || msg.Password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("username and password are required"))
	}

	user, err := s.authManager.CreateLocalUser(ctx, msg.Username, msg.Email, msg.Password)
	if err != nil {
		s.log.Error("Failed to create user: %v", err)
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("failed to create user"))
	}

	// Assign roles
	for _, roleName := range msg.Roles {
		if err := s.store.AssignRole(ctx, user.ID, roleName, "local"); err != nil {
			s.log.Error("Failed to assign role %s to user %s: %v", roleName, user.ID, err)
		}
	}

	// If no roles specified, assign default roles
	if len(msg.Roles) == 0 {
		defaultRoles, _ := s.store.GetDefaultRoles(ctx)
		for _, role := range defaultRoles {
			_ = s.store.AssignRole(ctx, user.ID, role.Name, "local")
		}
	}

	roles, _ := s.store.GetUserRoleNames(ctx, user.ID)

	return connect.NewResponse(&v1.CreateUserResponse{
		User: dbUserToProto(user, roles),
	}), nil
}

func (s *UserService) UpdateUser(ctx context.Context, req *connect.Request[v1.UpdateUserRequest]) (*connect.Response[v1.UpdateUserResponse], error) {
	msg := req.Msg

	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user ID is required"))
	}

	user, err := s.store.GetUser(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("user not found"))
	}

	if msg.Email != nil {
		user.Email = msg.Email
	}
	if msg.IsActive != nil {
		user.IsActive = *msg.IsActive
	}

	if err := s.store.UpdateUser(ctx, user); err != nil {
		s.log.Error("Failed to update user: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to update user"))
	}

	// Update roles if provided
	if len(msg.Roles) > 0 {
		// Get current roles
		currentRoles, _ := s.store.GetUserRoleNames(ctx, user.ID)

		// Build sets for comparison
		currentSet := make(map[string]bool)
		for _, r := range currentRoles {
			currentSet[r] = true
		}
		desiredSet := make(map[string]bool)
		for _, r := range msg.Roles {
			desiredSet[r] = true
		}

		// Remove roles not in desired set
		for _, r := range currentRoles {
			if !desiredSet[r] {
				_ = s.store.UnassignRole(ctx, user.ID, r)
			}
		}

		// Add roles not in current set
		for _, r := range msg.Roles {
			if !currentSet[r] {
				_ = s.store.AssignRole(ctx, user.ID, r, "local")
			}
		}
	}

	roles, _ := s.store.GetUserRoleNames(ctx, user.ID)

	return connect.NewResponse(&v1.UpdateUserResponse{
		User: dbUserToProto(user, roles),
	}), nil
}

func (s *UserService) DeleteUser(ctx context.Context, req *connect.Request[v1.DeleteUserRequest]) (*connect.Response[v1.DeleteUserResponse], error) {
	msg := req.Msg

	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user ID is required"))
	}

	if err := s.store.DeleteUser(ctx, msg.Id); err != nil {
		s.log.Error("Failed to delete user: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to delete user"))
	}

	return connect.NewResponse(&v1.DeleteUserResponse{
		Message: "user deleted",
	}), nil
}

func dbUserToProto(user *storage.User, roles []string) *v1.User {
	protoUser := &v1.User{
		Id:           user.ID,
		Username:     user.Username,
		Email:        user.Email,
		AuthProvider: user.AuthProvider,
		IsActive:     user.IsActive,
		Roles:        roles,
		CreatedAt:    timestamppb.New(user.CreatedAt),
		UpdatedAt:    timestamppb.New(user.UpdatedAt),
	}
	if user.LastLogin != nil {
		protoUser.LastLogin = timestamppb.New(*user.LastLogin)
	}
	return protoUser
}

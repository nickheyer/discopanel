package services

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect"
	"github.com/nickheyer/discopanel/internal/auth"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/rbac"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ discopanelv1connect.AuthServiceHandler = (*AuthService)(nil)

type AuthService struct {
	store       *storage.Store
	authManager *auth.Manager
	enforcer    *rbac.Enforcer
	oidcHandler *auth.OIDCHandler
	log         *logger.Logger
}

func NewAuthService(store *storage.Store, authManager *auth.Manager, enforcer *rbac.Enforcer, oidcHandler *auth.OIDCHandler, log *logger.Logger) *AuthService {
	return &AuthService{
		store:       store,
		authManager: authManager,
		enforcer:    enforcer,
		oidcHandler: oidcHandler,
		log:         log,
	}
}

func (s *AuthService) GetAuthStatus(ctx context.Context, req *connect.Request[v1.GetAuthStatusRequest]) (*connect.Response[v1.GetAuthStatusResponse], error) {
	userCount, err := s.store.CountUsers(ctx)
	if err != nil {
		s.log.Error("Failed to count users: %v", err)
		userCount = 0
	}

	oidcEnabled := s.oidcHandler != nil && s.oidcHandler.IsEnabled()

	return connect.NewResponse(&v1.GetAuthStatusResponse{
		LocalAuthEnabled:       s.authManager.IsLocalAuthEnabled(),
		OidcEnabled:            oidcEnabled,
		AllowRegistration:      s.authManager.IsRegistrationAllowed(),
		FirstUserSetup:         userCount == 0,
		AnonymousAccessEnabled: s.authManager.IsAnonymousAccessEnabled(),
	}), nil
}

func (s *AuthService) Login(ctx context.Context, req *connect.Request[v1.LoginRequest]) (*connect.Response[v1.LoginResponse], error) {
	msg := req.Msg

	if msg.Username == "" || msg.Password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("username and password are required"))
	}

	user, roles, token, expiresAt, err := s.authManager.Login(ctx, msg.Username, msg.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrUserNotActive) {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid credentials"))
		}
		if errors.Is(err, auth.ErrLocalAuthDisabled) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("local authentication is disabled"))
		}
		s.log.Error("Login failed: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("login failed"))
	}

	return connect.NewResponse(&v1.LoginResponse{
		Token:     token,
		User:      dbUserToProto(user, roles),
		ExpiresAt: timestamppb.New(expiresAt),
	}), nil
}

func (s *AuthService) Logout(ctx context.Context, req *connect.Request[v1.LogoutRequest]) (*connect.Response[v1.LogoutResponse], error) {
	// Extract token from Authorization header
	token := ""
	if authHeader := req.Header().Get("Authorization"); authHeader != "" {
		token, _ = strings.CutPrefix(authHeader, "Bearer ")
		token, _ = strings.CutPrefix(token, "bearer ")
	}

	if token != "" {
		if err := s.authManager.Logout(ctx, token); err != nil {
			s.log.Debug("Logout error: %v", err)
		}
	}

	return connect.NewResponse(&v1.LogoutResponse{
		Message: "logged out",
	}), nil
}

func (s *AuthService) Register(ctx context.Context, req *connect.Request[v1.RegisterRequest]) (*connect.Response[v1.RegisterResponse], error) {
	msg := req.Msg

	// Check if registration is allowed
	userCount, _ := s.store.CountUsers(ctx)
	isFirstUser := userCount == 0

	if !isFirstUser && !s.authManager.IsRegistrationAllowed() {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("registration is disabled"))
	}

	if msg.Username == "" || msg.Password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("username and password are required"))
	}

	user, err := s.authManager.CreateLocalUser(ctx, msg.Username, msg.Email, msg.Password)
	if err != nil {
		s.log.Error("Registration failed: %v", err)
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("registration failed"))
	}

	// First user gets admin role, others get default roles
	if isFirstUser {
		_ = s.store.AssignRole(ctx, user.ID, "admin", "local")
	} else {
		defaultRoles, _ := s.store.GetDefaultRoles(ctx)
		for _, role := range defaultRoles {
			_ = s.store.AssignRole(ctx, user.ID, role.Name, "local")
		}
	}

	roles, _ := s.store.GetUserRoleNames(ctx, user.ID)

	return connect.NewResponse(&v1.RegisterResponse{
		User: dbUserToProto(user, roles),
	}), nil
}

func (s *AuthService) GetCurrentUser(ctx context.Context, req *connect.Request[v1.GetCurrentUserRequest]) (*connect.Response[v1.GetCurrentUserResponse], error) {
	authUser := auth.GetUserFromContext(ctx)
	if authUser == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("not authenticated"))
	}

	// When all auth is disabled, the interceptor injects a synthetic admin
	// that doesn't exist in DB. Return it directly.
	if !s.authManager.IsAnyAuthEnabled() {
		roles := authUser.Roles
		protoUser := &v1.User{
			Id:           authUser.ID,
			Username:     authUser.Username,
			AuthProvider: authUser.Provider,
			IsActive:     true,
			Roles:        roles,
		}

		var permissions []*v1.Permission
		if s.enforcer != nil {
			for _, role := range roles {
				for _, p := range s.enforcer.GetPermissionsForRole(role) {
					permissions = append(permissions, &v1.Permission{
						Resource: p.Resource,
						Action:   p.Action,
						ObjectId: p.ObjectID,
					})
				}
			}
		}

		return connect.NewResponse(&v1.GetCurrentUserResponse{
			User:        protoUser,
			Permissions: permissions,
		}), nil
	}

	// Fetch user from db
	dbUser, err := s.store.GetUser(ctx, authUser.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get user"))
	}

	// Fetch roles from db
	roles, _ := s.store.GetUserRoleNames(ctx, authUser.ID)

	// Collect permissions from all user roles via the RBAC enforcer
	var permissions []*v1.Permission
	if s.enforcer != nil {
		seen := make(map[string]bool)
		for _, role := range roles {
			for _, p := range s.enforcer.GetPermissionsForRole(role) {
				key := p.Resource + ":" + p.Action + ":" + p.ObjectID
				if !seen[key] {
					seen[key] = true
					permissions = append(permissions, &v1.Permission{
						Resource: p.Resource,
						Action:   p.Action,
						ObjectId: p.ObjectID,
					})
				}
			}
		}
	}

	return connect.NewResponse(&v1.GetCurrentUserResponse{
		User:        dbUserToProto(dbUser, roles),
		Permissions: permissions,
	}), nil
}

func (s *AuthService) ChangePassword(ctx context.Context, req *connect.Request[v1.ChangePasswordRequest]) (*connect.Response[v1.ChangePasswordResponse], error) {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("not authenticated"))
	}

	msg := req.Msg
	if msg.OldPassword == "" || msg.NewPassword == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("old and new passwords are required"))
	}

	if err := s.authManager.ChangePassword(ctx, user.ID, msg.OldPassword, msg.NewPassword); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("incorrect current password"))
		}
		s.log.Error("Change password failed: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to change password"))
	}

	return connect.NewResponse(&v1.ChangePasswordResponse{
		Message: "password changed",
	}), nil
}

func (s *AuthService) GetOIDCLoginURL(ctx context.Context, req *connect.Request[v1.GetOIDCLoginURLRequest]) (*connect.Response[v1.GetOIDCLoginURLResponse], error) {
	if s.oidcHandler == nil || !s.oidcHandler.IsEnabled() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("OIDC is not enabled"))
	}

	return connect.NewResponse(&v1.GetOIDCLoginURLResponse{
		LoginUrl: "/api/v1/auth/oidc/login",
	}), nil
}

func (s *AuthService) GetAuthConfig(ctx context.Context, req *connect.Request[v1.GetAuthConfigRequest]) (*connect.Response[v1.GetAuthConfigResponse], error) {
	cfg := s.authManager.GetConfig()
	oidcEnabled := s.oidcHandler != nil && s.oidcHandler.IsEnabled()

	userCount, err := s.store.CountUsers(ctx)
	if err != nil {
		s.log.Error("Failed to count users: %v", err)
		userCount = 0
	}

	resp := &v1.GetAuthConfigResponse{
		LocalAuthEnabled:  cfg.Local.Enabled,
		AllowRegistration: cfg.Local.AllowRegistration,
		AnonymousAccess:   cfg.AnonymousAccess,
		SessionTimeout:    int32(cfg.SessionTimeout),
		OidcEnabled:       oidcEnabled,
		FirstUserSetup:    userCount == 0,
	}

	if oidcEnabled {
		resp.OidcIssuerUri = &cfg.OIDC.IssuerURI
		resp.OidcClientId = &cfg.OIDC.ClientID
		resp.OidcRedirectUrl = &cfg.OIDC.RedirectURL
		resp.OidcScopes = cfg.OIDC.Scopes
		resp.OidcRoleClaim = &cfg.OIDC.RoleClaim
	}

	return connect.NewResponse(resp), nil
}

func (s *AuthService) UpdateAuthSettings(ctx context.Context, req *connect.Request[v1.UpdateAuthSettingsRequest]) (*connect.Response[v1.UpdateAuthSettingsResponse], error) {
	msg := req.Msg

	if err := s.authManager.UpdateSettings(ctx, msg.LocalAuthEnabled, msg.AllowRegistration, msg.AnonymousAccess, msg.SessionTimeout); err != nil {
		if errors.Is(err, auth.ErrSessionTimeoutMin) {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		s.log.Error("Failed to update auth settings: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to update auth settings"))
	}

	// Return the updated config
	configResp, err := s.GetAuthConfig(ctx, connect.NewRequest(&v1.GetAuthConfigRequest{}))
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&v1.UpdateAuthSettingsResponse{
		Config: configResp.Msg,
	}), nil
}

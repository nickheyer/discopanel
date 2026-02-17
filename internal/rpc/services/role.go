package services

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/rbac"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ discopanelv1connect.RoleServiceHandler = (*RoleService)(nil)

type RoleService struct {
	store    *storage.Store
	enforcer *rbac.Enforcer
	log      *logger.Logger
}

func NewRoleService(store *storage.Store, enforcer *rbac.Enforcer, log *logger.Logger) *RoleService {
	return &RoleService{
		store:    store,
		enforcer: enforcer,
		log:      log,
	}
}

func (s *RoleService) ListRoles(ctx context.Context, req *connect.Request[v1.ListRolesRequest]) (*connect.Response[v1.ListRolesResponse], error) {
	roles, err := s.store.ListRoles(ctx)
	if err != nil {
		s.log.Error("Failed to list roles: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to list roles"))
	}

	protoRoles := make([]*v1.Role, 0, len(roles))
	for _, role := range roles {
		perms := s.enforcer.GetPermissionsForRole(role.Name)
		protoRoles = append(protoRoles, dbRoleToProto(role, perms))
	}

	return connect.NewResponse(&v1.ListRolesResponse{
		Roles: protoRoles,
	}), nil
}

func (s *RoleService) GetRole(ctx context.Context, req *connect.Request[v1.GetRoleRequest]) (*connect.Response[v1.GetRoleResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("role ID is required"))
	}

	role, err := s.store.GetRole(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("role not found"))
	}

	perms := s.enforcer.GetPermissionsForRole(role.Name)

	return connect.NewResponse(&v1.GetRoleResponse{
		Role: dbRoleToProto(role, perms),
	}), nil
}

func (s *RoleService) CreateRole(ctx context.Context, req *connect.Request[v1.CreateRoleRequest]) (*connect.Response[v1.CreateRoleResponse], error) {
	msg := req.Msg

	if msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("role name is required"))
	}

	role := &storage.Role{
		ID:          uuid.New().String(),
		Name:        msg.Name,
		Description: msg.Description,
		IsSystem:    false,
		IsDefault:   msg.IsDefault,
	}

	if err := s.store.CreateRole(ctx, role); err != nil {
		s.log.Error("Failed to create role: %v", err)
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("failed to create role"))
	}

	// Set initial permissions if provided
	if len(msg.Permissions) > 0 {
		perms := protoPermsToRbac(msg.Permissions)
		if err := s.enforcer.SetPermissionsForRole(role.Name, perms); err != nil {
			s.log.Error("Failed to set permissions for role %s: %v", role.Name, err)
		}
	}

	perms := s.enforcer.GetPermissionsForRole(role.Name)

	return connect.NewResponse(&v1.CreateRoleResponse{
		Role: dbRoleToProto(role, perms),
	}), nil
}

func (s *RoleService) UpdateRole(ctx context.Context, req *connect.Request[v1.UpdateRoleRequest]) (*connect.Response[v1.UpdateRoleResponse], error) {
	msg := req.Msg

	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("role ID is required"))
	}

	role, err := s.store.GetRole(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("role not found"))
	}

	if role.IsSystem {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot modify system role"))
	}

	if msg.Name != nil {
		role.Name = *msg.Name
	}
	if msg.Description != nil {
		role.Description = *msg.Description
	}
	if msg.IsDefault != nil {
		role.IsDefault = *msg.IsDefault
	}

	if err := s.store.UpdateRole(ctx, role); err != nil {
		s.log.Error("Failed to update role: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to update role"))
	}

	perms := s.enforcer.GetPermissionsForRole(role.Name)

	return connect.NewResponse(&v1.UpdateRoleResponse{
		Role: dbRoleToProto(role, perms),
	}), nil
}

func (s *RoleService) DeleteRole(ctx context.Context, req *connect.Request[v1.DeleteRoleRequest]) (*connect.Response[v1.DeleteRoleResponse], error) {
	msg := req.Msg

	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("role ID is required"))
	}

	role, err := s.store.GetRole(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("role not found"))
	}

	if role.IsSystem {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot delete system role"))
	}

	// Remove all permissions for this role
	_ = s.enforcer.SetPermissionsForRole(role.Name, nil)

	if err := s.store.DeleteRole(ctx, msg.Id); err != nil {
		s.log.Error("Failed to delete role: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to delete role"))
	}

	return connect.NewResponse(&v1.DeleteRoleResponse{
		Message: "role deleted",
	}), nil
}

func (s *RoleService) GetPermissionMatrix(ctx context.Context, req *connect.Request[v1.GetPermissionMatrixRequest]) (*connect.Response[v1.GetPermissionMatrixResponse], error) {
	matrix := s.enforcer.GetPermissionMatrix()

	rolePermsMap := make(map[string]*v1.RolePermissions)
	for roleName, perms := range matrix {
		protoPerms := make([]*v1.Permission, 0, len(perms))
		for _, p := range perms {
			protoPerms = append(protoPerms, &v1.Permission{
				Resource: p.Resource,
				Action:   p.Action,
				ObjectId: p.ObjectID,
			})
		}
		rolePermsMap[roleName] = &v1.RolePermissions{
			Permissions: protoPerms,
		}
	}

	// Build resource_actions from procedure mappings
	raEntries := rbac.ResourceActionsFromProcedures()
	protoRA := make([]*v1.ResourceActions, 0, len(raEntries))
	for _, ra := range raEntries {
		protoRA = append(protoRA, &v1.ResourceActions{
			Resource: ra.Resource,
			Actions:  ra.Actions,
		})
	}

	resp := &v1.GetPermissionMatrixResponse{
		ResourceActions: protoRA,
		RolePermissions: rolePermsMap,
	}

	// Populate available objects for scoped permissions when requested.
	// Driven entirely by ProcedurePermissions: any resource with a non-empty
	// ObjectIDField is scopeable, and the field name determines which entity
	// type provides the objects (e.g. "server_id" → servers).
	if req.Msg.IncludeObjects {
		type idName struct{ id, name string }

		// Store fetchers keyed by resource constant.
		fetchers := map[string]func() []idName{
			rbac.ResourceServers: func() []idName {
				items, err := s.store.ListServers(ctx)
				if err != nil {
					return nil
				}
				out := make([]idName, len(items))
				for i, x := range items {
					out[i] = idName{x.ID, x.Name}
				}
				return out
			},
			rbac.ResourceModules: func() []idName {
				items, err := s.store.ListModules(ctx)
				if err != nil {
					return nil
				}
				out := make([]idName, len(items))
				for i, x := range items {
					out[i] = idName{x.ID, x.Name}
				}
				return out
			},
			rbac.ResourceModuleTemplates: func() []idName {
				items, err := s.store.ListModuleTemplates(ctx)
				if err != nil {
					return nil
				}
				out := make([]idName, len(items))
				for i, x := range items {
					out[i] = idName{x.ID, x.Name}
				}
				return out
			},
			rbac.ResourceProxy: func() []idName {
				items, err := s.store.GetProxyListeners(ctx)
				if err != nil {
					return nil
				}
				out := make([]idName, len(items))
				for i, x := range items {
					out[i] = idName{x.ID, x.Name}
				}
				return out
			},
			rbac.ResourceTasks: func() []idName {
				items, err := s.store.ListAllScheduledTasks(ctx)
				if err != nil {
					return nil
				}
				out := make([]idName, len(items))
				for i, x := range items {
					out[i] = idName{x.ID, x.Name}
				}
				return out
			},
			rbac.ResourceModpacks: func() []idName {
				items, _, err := s.store.ListIndexedModpacks(ctx, 0, -1)
				if err != nil {
					return nil
				}
				out := make([]idName, len(items))
				for i, x := range items {
					out[i] = idName{x.ID, x.Name}
				}
				return out
			},
		}

		// Collect needed source resources and fetch each once.
		fetched := make(map[string][]idName)
		needed := make(map[string]bool)
		for _, res := range rbac.AllResources {
			if source, ok := rbac.ResourceScopeSource[res]; ok {
				needed[source] = true
			}
		}
		for src := range needed {
			if fn, ok := fetchers[src]; ok {
				fetched[src] = fn()
			}
		}

		// Emit ScopeableObjects in stable resource order.
		var objects []*v1.ScopeableObject
		for _, resource := range rbac.AllResources {
			source, ok := rbac.ResourceScopeSource[resource]
			if !ok {
				continue
			}
			for _, obj := range fetched[source] {
				objects = append(objects, &v1.ScopeableObject{
					Id:          obj.id,
					Name:        obj.name,
					Resource:    resource,
					ScopeSource: source,
				})
			}
		}
		resp.AvailableObjects = objects
	}

	return connect.NewResponse(resp), nil
}

func (s *RoleService) UpdatePermissions(ctx context.Context, req *connect.Request[v1.UpdatePermissionsRequest]) (*connect.Response[v1.UpdatePermissionsResponse], error) {
	msg := req.Msg

	if msg.RoleName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("role name is required"))
	}

	perms := protoPermsToRbac(msg.Permissions)

	if err := s.enforcer.SetPermissionsForRole(msg.RoleName, perms); err != nil {
		s.log.Error("Failed to update permissions for role %s: %v", msg.RoleName, err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to update permissions"))
	}

	return connect.NewResponse(&v1.UpdatePermissionsResponse{
		Message: "permissions updated",
	}), nil
}

func (s *RoleService) AssignRole(ctx context.Context, req *connect.Request[v1.AssignRoleRequest]) (*connect.Response[v1.AssignRoleResponse], error) {
	msg := req.Msg

	if msg.UserId == "" || msg.RoleName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user ID and role name are required"))
	}

	if err := s.store.AssignRole(ctx, msg.UserId, msg.RoleName, "local"); err != nil {
		s.log.Error("Failed to assign role: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to assign role"))
	}

	return connect.NewResponse(&v1.AssignRoleResponse{
		Message: "role assigned",
	}), nil
}

func (s *RoleService) UnassignRole(ctx context.Context, req *connect.Request[v1.UnassignRoleRequest]) (*connect.Response[v1.UnassignRoleResponse], error) {
	msg := req.Msg

	if msg.UserId == "" || msg.RoleName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user ID and role name are required"))
	}

	if err := s.store.UnassignRole(ctx, msg.UserId, msg.RoleName); err != nil {
		s.log.Error("Failed to unassign role: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to unassign role"))
	}

	return connect.NewResponse(&v1.UnassignRoleResponse{
		Message: "role unassigned",
	}), nil
}

func (s *RoleService) GetUserRoles(ctx context.Context, req *connect.Request[v1.GetUserRolesRequest]) (*connect.Response[v1.GetUserRolesResponse], error) {
	msg := req.Msg

	if msg.UserId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user ID is required"))
	}

	roles, err := s.store.GetUserRoleNames(ctx, msg.UserId)
	if err != nil {
		s.log.Error("Failed to get user roles: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get user roles"))
	}

	return connect.NewResponse(&v1.GetUserRolesResponse{
		Roles: roles,
	}), nil
}

func dbRoleToProto(role *storage.Role, perms []rbac.Permission) *v1.Role {
	protoPerms := make([]*v1.Permission, 0, len(perms))
	for _, p := range perms {
		protoPerms = append(protoPerms, &v1.Permission{
			Resource: p.Resource,
			Action:   p.Action,
			ObjectId: p.ObjectID,
		})
	}

	return &v1.Role{
		Id:          role.ID,
		Name:        role.Name,
		Description: role.Description,
		IsSystem:    role.IsSystem,
		IsDefault:   role.IsDefault,
		Permissions: protoPerms,
		CreatedAt:   timestamppb.New(role.CreatedAt),
		UpdatedAt:   timestamppb.New(role.UpdatedAt),
	}
}

func protoPermsToRbac(protoPerms []*v1.Permission) []rbac.Permission {
	perms := make([]rbac.Permission, 0, len(protoPerms))
	for _, p := range protoPerms {
		objectID := p.ObjectId
		if objectID == "" {
			objectID = "*"
		}
		perms = append(perms, rbac.Permission{
			Resource: p.Resource,
			Action:   p.Action,
			ObjectID: objectID,
		})
	}
	return perms
}

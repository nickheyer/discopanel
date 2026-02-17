package rbac

import (
	"fmt"
	"strings"

	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"gorm.io/gorm"
)

// Permission represents a single resource/action/object permission tuple.
type Permission struct {
	Resource string
	Action   string
	ObjectID string
}

// Enforcer wraps a Casbin enforcer with convenience methods for RBAC.
type Enforcer struct {
	enforcer *casbin.Enforcer
}

// NewEnforcer creates a new Casbin RBAC enforcer backed by the given GORM database.
func NewEnforcer(db *gorm.DB) (*Enforcer, error) {
	adapter, err := gormadapter.NewAdapterByDB(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create casbin adapter: %w", err)
	}

	// RBAC model with resource/action/object_id
	m, err := model.NewModelFromString(`
[request_definition]
r = sub, res, act, obj

[policy_definition]
p = sub, res, act, obj

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && (p.res == "*" || r.res == p.res) && (p.act == "*" || r.act == p.act) && (p.obj == "*" || r.obj == p.obj)
`)
	if err != nil {
		return nil, fmt.Errorf("failed to create casbin model: %w", err)
	}

	e, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		return nil, fmt.Errorf("failed to create casbin enforcer: %w", err)
	}

	if err := e.LoadPolicy(); err != nil {
		return nil, fmt.Errorf("failed to load casbin policy: %w", err)
	}

	return &Enforcer{enforcer: e}, nil
}

// SeedDefaultPolicies ensures default roles have their base permissions.
// When anonymousEnabled is true, the anonymous role receives read-only
// access to common resources. When false, all anonymous policies are removed.
func (e *Enforcer) SeedDefaultPolicies(anonymousEnabled bool) error {
	policies := [][]string{
		{"admin", "*", "*", "*"},
		{"user", ResourceServers, ActionRead, "*"},
		{"user", ResourceServers, ActionStart, "*"},
		{"user", ResourceServers, ActionStop, "*"},
		{"user", ResourceServers, ActionRestart, "*"},
		{"user", ResourceServers, ActionCommand, "*"},
		{"user", ResourceServerConfig, ActionRead, "*"},
		{"user", ResourceMods, ActionRead, "*"},
		{"user", ResourceModpacks, ActionRead, "*"},
		{"user", ResourceModules, ActionRead, "*"},
		{"user", ResourceModuleTemplates, ActionRead, "*"},
		{"user", ResourceFiles, ActionRead, "*"},
		{"user", ResourceTasks, ActionRead, "*"},
		{"user", ResourceProxy, ActionRead, "*"},
		{"anonymous", ResourceServers, ActionRead, "*"},
		{"anonymous", ResourceServerConfig, ActionRead, "*"},
		{"anonymous", ResourceMods, ActionRead, "*"},
		{"anonymous", ResourceModpacks, ActionRead, "*"},
		{"anonymous", ResourceModules, ActionRead, "*"},
		{"anonymous", ResourceModuleTemplates, ActionRead, "*"},
		{"anonymous", ResourceFiles, ActionRead, "*"},
		{"anonymous", ResourceTasks, ActionRead, "*"},
		{"anonymous", ResourceProxy, ActionRead, "*"},
	}
	for _, p := range policies {
		if p[0] == "anonymous" && !anonymousEnabled {
			continue
		}
		has, err := e.enforcer.HasPolicy(p[0], p[1], p[2], p[3])
		if err != nil {
			return err
		}
		if !has {
			if _, err = e.enforcer.AddPolicy(p[0], p[1], p[2], p[3]); err != nil {
				return err
			}
		}
	}

	if !anonymousEnabled {
		e.enforcer.RemoveFilteredPolicy(0, "anonymous")
	}

	return e.enforcer.SavePolicy()
}

// Enforce checks if any of the given roles allows the specified action on a
// resource with the given object ID. Returns true on the first matching role.
func (e *Enforcer) Enforce(roles []string, resource, action, objectID string) (bool, error) {
	for _, role := range roles {
		allowed, err := e.enforcer.Enforce(role, resource, action, objectID)
		if err != nil {
			return false, err
		}
		if allowed {
			return true, nil
		}
	}
	return false, nil
}

// GetPermissionsForRole returns all permissions currently assigned to the role.
func (e *Enforcer) GetPermissionsForRole(role string) []Permission {
	policies, err := e.enforcer.GetFilteredPolicy(0, role)
	if err != nil {
		return nil
	}
	perms := make([]Permission, 0, len(policies))
	for _, p := range policies {
		if len(p) >= 4 {
			perms = append(perms, Permission{
				Resource: p[1],
				Action:   p[2],
				ObjectID: p[3],
			})
		}
	}
	return perms
}

// SetPermissionsForRole replaces all permissions for a role atomically.
// The admin role cannot be modified.
func (e *Enforcer) SetPermissionsForRole(role string, perms []Permission) error {
	// Don't modify admin role
	if strings.ToLower(role) == "admin" {
		return fmt.Errorf("cannot modify admin role permissions")
	}

	// Remove existing permissions
	e.enforcer.RemoveFilteredPolicy(0, role)

	// Add new permissions
	for _, p := range perms {
		objectID := p.ObjectID
		if objectID == "" {
			objectID = "*"
		}
		_, err := e.enforcer.AddPolicy(role, p.Resource, p.Action, objectID)
		if err != nil {
			return err
		}
	}

	return e.enforcer.SavePolicy()
}

// GetPermissionMatrix returns a map of role names to their permission slices,
// covering all roles that have any policy defined.
func (e *Enforcer) GetPermissionMatrix() map[string][]Permission {
	policies, err := e.enforcer.GetPolicy()
	if err != nil {
		return nil
	}
	matrix := make(map[string][]Permission)
	for _, p := range policies {
		if len(p) >= 4 {
			role := p[0]
			matrix[role] = append(matrix[role], Permission{
				Resource: p[1],
				Action:   p[2],
				ObjectID: p[3],
			})
		}
	}
	return matrix
}

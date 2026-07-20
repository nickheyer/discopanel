package rbac

import (
	"fmt"
	"strings"

	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"gorm.io/gorm"
)

// Wraps Casbin enforcer with convenience methods for RBAC
type Enforcer struct {
	enforcer *casbin.Enforcer
}

// Creates Casbin RBAC enforcer backed by GORM database
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

// Ensures default roles have their base permissions
func (e *Enforcer) SeedDefaultPolicies(anonymousEnabled bool) error {
	policies := map[string][][]string{
		"admin": {
			{"admin", "*", "*", "*"},
		},
		"user": {
			{"user", ResourceServers, ActionRead, "*"},
			{"user", ResourceServers, ActionStart, "*"},
			{"user", ResourceServers, ActionStop, "*"},
			{"user", ResourceServers, ActionRestart, "*"},
			{"user", ResourceServers, ActionCommand, "*"},
			{"user", ResourceServerProperties, ActionRead, "*"},
			{"user", ResourceMods, ActionRead, "*"},
			{"user", ResourceModpacks, ActionRead, "*"},
			{"user", ResourceModules, ActionRead, "*"},
			{"user", ResourceModuleTemplates, ActionRead, "*"},
			{"user", ResourceFiles, ActionRead, "*"},
			{"user", ResourceTasks, ActionRead, "*"},
			{"user", ResourceProxy, ActionRead, "*"},
		},
		"module": {
			{"module", ResourceServers, ActionRead, "*"},
			{"module", ResourceServerProperties, ActionRead, "*"},
			{"module", ResourceModpacks, ActionRead, "*"},
		},
		"doctor": {
			{"doctor", ResourceServers, ActionRead, "*"},
			{"doctor", ResourceServers, ActionStart, "*"},
			{"doctor", ResourceServers, ActionStop, "*"},
			{"doctor", ResourceServers, ActionRestart, "*"},
			{"doctor", ResourceServerProperties, ActionRead, "*"},
			{"doctor", ResourceSettings, ActionRead, "*"},
			{"doctor", ResourceModpacks, ActionRead, "*"},
		},
		"anonymous": {
			{"anonymous", ResourceServers, ActionRead, "*"},
			{"anonymous", ResourceServerProperties, ActionRead, "*"},
			{"anonymous", ResourceMods, ActionRead, "*"},
			{"anonymous", ResourceModpacks, ActionRead, "*"},
			{"anonymous", ResourceModules, ActionRead, "*"},
			{"anonymous", ResourceModuleTemplates, ActionRead, "*"},
			{"anonymous", ResourceFiles, ActionRead, "*"},
			{"anonymous", ResourceTasks, ActionRead, "*"},
			{"anonymous", ResourceProxy, ActionRead, "*"},
		},
	}

	for role, rolePolicies := range policies {
		existing, err := e.enforcer.GetFilteredPolicy(0, role)
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			continue
		}

		for _, p := range rolePolicies {
			if _, err := e.enforcer.AddPolicy(p[0], p[1], p[2], p[3]); err != nil {
				return err
			}
		}
	}

	return e.enforcer.SavePolicy()
}

// True if any role allows action on resource/object
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

// Returns all permissions currently assigned to role
func (e *Enforcer) GetPermissionsForRole(role string) []*v1.Permission {
	policies, err := e.enforcer.GetFilteredPolicy(0, role)
	if err != nil {
		return nil
	}
	perms := make([]*v1.Permission, 0, len(policies))
	for _, p := range policies {
		if len(p) >= 4 {
			perms = append(perms, &v1.Permission{
				Resource: p[1],
				Action:   p[2],
				ObjectId: p[3],
			})
		}
	}
	return perms
}

// Replaces all permissions for a role, admin role blocked
func (e *Enforcer) SetPermissionsForRole(role string, perms []*v1.Permission) error {
	// Don't modify admin role
	if strings.ToLower(role) == "admin" {
		return fmt.Errorf("cannot modify admin role permissions")
	}

	// Remove existing permissions
	e.enforcer.RemoveFilteredPolicy(0, role)

	// Add new permissions
	for _, p := range perms {
		objectID := p.ObjectId
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

// Maps each role to its permission slices
func (e *Enforcer) GetPermissionMatrix() map[string][]*v1.Permission {
	policies, err := e.enforcer.GetPolicy()
	if err != nil {
		return nil
	}
	matrix := make(map[string][]*v1.Permission)
	for _, p := range policies {
		if len(p) >= 4 {
			role := p[0]
			matrix[role] = append(matrix[role], &v1.Permission{
				Resource: p[1],
				Action:   p[2],
				ObjectId: p[3],
			})
		}
	}
	return matrix
}

package rbac

// Resource constants
const (
	ResourceServers         = "servers"
	ResourceServerConfig    = "server_config"
	ResourceMods            = "mods"
	ResourceModpacks        = "modpacks"
	ResourceModules         = "modules"
	ResourceModuleTemplates = "module_templates"
	ResourceFiles           = "files"
	ResourceTasks           = "tasks"
	ResourceProxy           = "proxy"
	ResourceUsers           = "users"
	ResourceRoles           = "roles"
	ResourceSettings        = "settings"
	ResourceSupport         = "support"
	ResourceUploads         = "uploads"
)

// Action constants
const (
	ActionRead    = "read"
	ActionCreate  = "create"
	ActionUpdate  = "update"
	ActionDelete  = "delete"
	ActionStart   = "start"
	ActionStop    = "stop"
	ActionRestart = "restart"
	ActionCommand = "command"
)

// ResourceActionEntry pairs a resource with its valid actions.
type ResourceActionEntry struct {
	Resource string
	Actions  []string
}

// AllActions in display order.
var AllActions = []string{
	ActionRead, ActionCreate, ActionUpdate, ActionDelete,
	ActionStart, ActionStop, ActionRestart, ActionCommand,
}

// AllResources in display order.
var AllResources = []string{
	ResourceServers, ResourceServerConfig, ResourceMods,
	ResourceModpacks, ResourceModules, ResourceModuleTemplates,
	ResourceFiles, ResourceTasks, ResourceProxy,
	ResourceUsers, ResourceRoles, ResourceSettings,
	ResourceSupport, ResourceUploads,
}

// ResourceScopeSource maps each scopeable resource to the resource that
// provides its scope objects. For example, files are scoped by server_id,
// so ResourceFiles → ResourceServers. Resources absent from this map
// (users, roles, settings, support, uploads) have no per-object scoping.
var ResourceScopeSource = map[string]string{
	ResourceServers:         ResourceServers,
	ResourceServerConfig:    ResourceServers,
	ResourceFiles:           ResourceServers,
	ResourceMods:            ResourceServers,
	ResourceModules:         ResourceModules,
	ResourceModuleTemplates: ResourceModuleTemplates,
	ResourceModpacks:        ResourceModpacks,
	ResourceProxy:           ResourceProxy,
	ResourceTasks:           ResourceTasks,
}

// ResourceActionsFromProcedures derives which actions are valid for each
// resource by inspecting the ProcedurePermissions mapping. Maintains stable
// resource ordering via AllResources and stable action ordering via AllActions.
func ResourceActionsFromProcedures() []ResourceActionEntry {
	actionSet := make(map[string]map[string]bool)
	for _, pp := range ProcedurePermissions {
		if actionSet[pp.Resource] == nil {
			actionSet[pp.Resource] = make(map[string]bool)
		}
		actionSet[pp.Resource][pp.Action] = true
	}

	entries := make([]ResourceActionEntry, 0, len(AllResources))
	for _, res := range AllResources {
		acts, ok := actionSet[res]
		if !ok {
			continue
		}
		var ordered []string
		for _, a := range AllActions {
			if acts[a] {
				ordered = append(ordered, a)
			}
		}
		entries = append(entries, ResourceActionEntry{Resource: res, Actions: ordered})
	}
	return entries
}

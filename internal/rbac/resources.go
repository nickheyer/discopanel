package rbac

// Resource constants
const (
	ResourceServers          = "servers"
	ResourceServerProperties = "server_properties"
	ResourceMods             = "mods"
	ResourceModpacks         = "modpacks"
	ResourceModules          = "modules"
	ResourceModuleTemplates  = "module_templates"
	ResourceFiles            = "files"
	ResourceTasks            = "tasks"
	ResourceProxy            = "proxy"
	ResourceUsers            = "users"
	ResourceRoles            = "roles"
	ResourceSettings         = "settings"
	ResourceSupport          = "support"
	ResourceUploads          = "uploads"
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

// Pairs a resource with its valid actions
type ResourceActionEntry struct {
	Resource string
	Actions  []string
}

// Actions in display order
var AllActions = []string{
	ActionRead, ActionCreate, ActionUpdate, ActionDelete,
	ActionStart, ActionStop, ActionRestart, ActionCommand,
}

// Resources in display order
var AllResources = []string{
	ResourceServers, ResourceServerProperties, ResourceMods,
	ResourceModpacks, ResourceModules, ResourceModuleTemplates,
	ResourceFiles, ResourceTasks, ResourceProxy,
	ResourceUsers, ResourceRoles, ResourceSettings,
	ResourceSupport, ResourceUploads,
}

// Maps scopeable resource to resource providing its scope objects
var ResourceScopeSource = map[string]string{
	ResourceServers:          ResourceServers,
	ResourceServerProperties: ResourceServers,
	ResourceFiles:            ResourceServers,
	ResourceMods:             ResourceServers,
	ResourceModules:          ResourceModules,
	ResourceModuleTemplates:  ResourceModuleTemplates,
	ResourceModpacks:         ResourceModpacks,
	ResourceProxy:            ResourceProxy,
	ResourceTasks:            ResourceTasks,
}

// Derives valid actions per resource from ProcedurePermissions mapping
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

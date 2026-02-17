package rbac

// ProcedurePermission maps an RPC procedure to a resource and action.
type ProcedurePermission struct {
	Resource      string
	Action        string
	ObjectIDField string // Protobuf field name to extract for per-object RBAC (empty = "*")
}

// PublicProcedures lists RPC procedures that require no authentication.
var PublicProcedures = map[string]bool{
	"/discopanel.v1.AuthService/GetAuthStatus":   true,
	"/discopanel.v1.AuthService/Login":           true,
	"/discopanel.v1.AuthService/Register":        true,
	"/discopanel.v1.AuthService/GetOIDCLoginURL": true,
}

// AuthenticatedOnlyProcedures lists RPC procedures that require authentication
// but no specific resource permission.
var AuthenticatedOnlyProcedures = map[string]bool{
	// AuthService - authenticated user operations
	"/discopanel.v1.AuthService/GetCurrentUser": true,
	"/discopanel.v1.AuthService/Logout":         true,
	"/discopanel.v1.AuthService/ChangePassword": true,

	// MinecraftService - reference data, no resource ownership
	"/discopanel.v1.MinecraftService/GetMinecraftVersions": true,
	"/discopanel.v1.MinecraftService/GetModLoaders":        true,
	"/discopanel.v1.MinecraftService/GetDockerImages":      true,
}

// ProcedurePermissions maps each RPC procedure path to the resource and action
// required to invoke it, plus an optional ObjectIDField for per-object scoping.
var ProcedurePermissions = map[string]ProcedurePermission{
	// ── ServerService ──────────────────────────────────────────────────
	"/discopanel.v1.ServerService/ListServers":          {Resource: ResourceServers, Action: ActionRead},
	"/discopanel.v1.ServerService/GetServer":            {Resource: ResourceServers, Action: ActionRead, ObjectIDField: "id"},
	"/discopanel.v1.ServerService/GetServerLogs":        {Resource: ResourceServers, Action: ActionRead, ObjectIDField: "id"},
	"/discopanel.v1.ServerService/ClearServerLogs":      {Resource: ResourceServers, Action: ActionUpdate, ObjectIDField: "id"},
	"/discopanel.v1.ServerService/GetNextAvailablePort": {Resource: ResourceServers, Action: ActionRead},
	"/discopanel.v1.ServerService/CreateServer":         {Resource: ResourceServers, Action: ActionCreate},
	"/discopanel.v1.ServerService/UpdateServer":         {Resource: ResourceServers, Action: ActionUpdate, ObjectIDField: "id"},
	"/discopanel.v1.ServerService/DeleteServer":         {Resource: ResourceServers, Action: ActionDelete, ObjectIDField: "id"},
	"/discopanel.v1.ServerService/StartServer":          {Resource: ResourceServers, Action: ActionStart, ObjectIDField: "id"},
	"/discopanel.v1.ServerService/StopServer":           {Resource: ResourceServers, Action: ActionStop, ObjectIDField: "id"},
	"/discopanel.v1.ServerService/RestartServer":        {Resource: ResourceServers, Action: ActionRestart, ObjectIDField: "id"},
	"/discopanel.v1.ServerService/RecreateServer":       {Resource: ResourceServers, Action: ActionRestart, ObjectIDField: "id"},
	"/discopanel.v1.ServerService/SendCommand":          {Resource: ResourceServers, Action: ActionCommand, ObjectIDField: "id"},

	// ── AuthService (admin) ───────────────────────────────────────────
	"/discopanel.v1.AuthService/GetAuthConfig":      {Resource: ResourceSettings, Action: ActionRead},
	"/discopanel.v1.AuthService/UpdateAuthSettings": {Resource: ResourceSettings, Action: ActionUpdate},

	// ── ConfigService ──────────────────────────────────────────────────
	"/discopanel.v1.ConfigService/GetServerConfig":      {Resource: ResourceServerConfig, Action: ActionRead, ObjectIDField: "server_id"},
	"/discopanel.v1.ConfigService/UpdateServerConfig":   {Resource: ResourceServerConfig, Action: ActionUpdate, ObjectIDField: "server_id"},
	"/discopanel.v1.ConfigService/GetGlobalSettings":    {Resource: ResourceSettings, Action: ActionRead},
	"/discopanel.v1.ConfigService/UpdateGlobalSettings": {Resource: ResourceSettings, Action: ActionUpdate},

	// ── FileService ────────────────────────────────────────────────────
	"/discopanel.v1.FileService/ListFiles":        {Resource: ResourceFiles, Action: ActionRead, ObjectIDField: "server_id"},
	"/discopanel.v1.FileService/GetFile":          {Resource: ResourceFiles, Action: ActionRead, ObjectIDField: "server_id"},
	"/discopanel.v1.FileService/SaveUploadedFile": {Resource: ResourceFiles, Action: ActionCreate, ObjectIDField: "server_id"},
	"/discopanel.v1.FileService/UpdateFile":       {Resource: ResourceFiles, Action: ActionUpdate, ObjectIDField: "server_id"},
	"/discopanel.v1.FileService/DeleteFile":       {Resource: ResourceFiles, Action: ActionDelete, ObjectIDField: "server_id"},
	"/discopanel.v1.FileService/RenameFile":       {Resource: ResourceFiles, Action: ActionUpdate, ObjectIDField: "server_id"},
	"/discopanel.v1.FileService/ExtractArchive":   {Resource: ResourceFiles, Action: ActionUpdate, ObjectIDField: "server_id"},

	// ── ModService ─────────────────────────────────────────────────────
	"/discopanel.v1.ModService/ListMods":          {Resource: ResourceMods, Action: ActionRead, ObjectIDField: "server_id"},
	"/discopanel.v1.ModService/GetMod":            {Resource: ResourceMods, Action: ActionRead, ObjectIDField: "server_id"},
	"/discopanel.v1.ModService/ImportUploadedMod": {Resource: ResourceMods, Action: ActionCreate, ObjectIDField: "server_id"},
	"/discopanel.v1.ModService/UpdateMod":         {Resource: ResourceMods, Action: ActionUpdate, ObjectIDField: "server_id"},
	"/discopanel.v1.ModService/DeleteMod":         {Resource: ResourceMods, Action: ActionDelete, ObjectIDField: "server_id"},

	// ── ModpackService ─────────────────────────────────────────────────
	"/discopanel.v1.ModpackService/SearchModpacks":        {Resource: ResourceModpacks, Action: ActionRead},
	"/discopanel.v1.ModpackService/GetModpack":            {Resource: ResourceModpacks, Action: ActionRead, ObjectIDField: "id"},
	"/discopanel.v1.ModpackService/GetModpackBySlug":      {Resource: ResourceModpacks, Action: ActionRead},
	"/discopanel.v1.ModpackService/GetModpackByURL":       {Resource: ResourceModpacks, Action: ActionRead},
	"/discopanel.v1.ModpackService/SyncModpacks":          {Resource: ResourceModpacks, Action: ActionCreate},
	"/discopanel.v1.ModpackService/ImportUploadedModpack": {Resource: ResourceModpacks, Action: ActionCreate},
	"/discopanel.v1.ModpackService/DeleteModpack":         {Resource: ResourceModpacks, Action: ActionDelete, ObjectIDField: "id"},
	"/discopanel.v1.ModpackService/ToggleFavorite":        {Resource: ResourceModpacks, Action: ActionUpdate, ObjectIDField: "id"},
	"/discopanel.v1.ModpackService/ListFavorites":         {Resource: ResourceModpacks, Action: ActionRead},
	"/discopanel.v1.ModpackService/GetIndexerStatus":      {Resource: ResourceModpacks, Action: ActionRead},
	"/discopanel.v1.ModpackService/GetModpackConfig":      {Resource: ResourceModpacks, Action: ActionRead, ObjectIDField: "id"},
	"/discopanel.v1.ModpackService/GetModpackFiles":       {Resource: ResourceModpacks, Action: ActionRead, ObjectIDField: "id"},
	"/discopanel.v1.ModpackService/GetModpackVersions":    {Resource: ResourceModpacks, Action: ActionRead, ObjectIDField: "id"},
	"/discopanel.v1.ModpackService/SyncModpackFiles":      {Resource: ResourceModpacks, Action: ActionUpdate, ObjectIDField: "id"},

	// ── ModuleService ──────────────────────────────────────────────────
	"/discopanel.v1.ModuleService/ListModuleTemplates":        {Resource: ResourceModuleTemplates, Action: ActionRead},
	"/discopanel.v1.ModuleService/GetModuleTemplate":          {Resource: ResourceModuleTemplates, Action: ActionRead, ObjectIDField: "id"},
	"/discopanel.v1.ModuleService/CreateModuleTemplate":       {Resource: ResourceModuleTemplates, Action: ActionCreate},
	"/discopanel.v1.ModuleService/UpdateModuleTemplate":       {Resource: ResourceModuleTemplates, Action: ActionUpdate, ObjectIDField: "id"},
	"/discopanel.v1.ModuleService/DeleteModuleTemplate":       {Resource: ResourceModuleTemplates, Action: ActionDelete, ObjectIDField: "id"},
	"/discopanel.v1.ModuleService/ListModules":                {Resource: ResourceModules, Action: ActionRead},
	"/discopanel.v1.ModuleService/GetModule":                  {Resource: ResourceModules, Action: ActionRead, ObjectIDField: "id"},
	"/discopanel.v1.ModuleService/CreateModule":               {Resource: ResourceModules, Action: ActionCreate, ObjectIDField: "server_id"},
	"/discopanel.v1.ModuleService/UpdateModule":               {Resource: ResourceModules, Action: ActionUpdate, ObjectIDField: "id"},
	"/discopanel.v1.ModuleService/DeleteModule":               {Resource: ResourceModules, Action: ActionDelete, ObjectIDField: "id"},
	"/discopanel.v1.ModuleService/StartModule":                {Resource: ResourceModules, Action: ActionStart, ObjectIDField: "id"},
	"/discopanel.v1.ModuleService/StopModule":                 {Resource: ResourceModules, Action: ActionStop, ObjectIDField: "id"},
	"/discopanel.v1.ModuleService/RestartModule":              {Resource: ResourceModules, Action: ActionRestart, ObjectIDField: "id"},
	"/discopanel.v1.ModuleService/RecreateModule":             {Resource: ResourceModules, Action: ActionRestart, ObjectIDField: "id"},
	"/discopanel.v1.ModuleService/GetModuleLogs":              {Resource: ResourceModules, Action: ActionRead, ObjectIDField: "id"},
	"/discopanel.v1.ModuleService/GetNextAvailableModulePort": {Resource: ResourceModules, Action: ActionRead},
	"/discopanel.v1.ModuleService/GetAvailableAliases":        {Resource: ResourceModules, Action: ActionRead},
	"/discopanel.v1.ModuleService/GetResolvedAliases":         {Resource: ResourceModules, Action: ActionRead},

	// ── ProxyService ───────────────────────────────────────────────────
	"/discopanel.v1.ProxyService/GetProxyRoutes":      {Resource: ResourceProxy, Action: ActionRead},
	"/discopanel.v1.ProxyService/GetProxyStatus":      {Resource: ResourceProxy, Action: ActionRead},
	"/discopanel.v1.ProxyService/UpdateProxyConfig":   {Resource: ResourceProxy, Action: ActionUpdate},
	"/discopanel.v1.ProxyService/GetProxyListeners":   {Resource: ResourceProxy, Action: ActionRead},
	"/discopanel.v1.ProxyService/CreateProxyListener": {Resource: ResourceProxy, Action: ActionCreate},
	"/discopanel.v1.ProxyService/UpdateProxyListener": {Resource: ResourceProxy, Action: ActionUpdate, ObjectIDField: "id"},
	"/discopanel.v1.ProxyService/DeleteProxyListener": {Resource: ResourceProxy, Action: ActionDelete, ObjectIDField: "id"},
	"/discopanel.v1.ProxyService/GetServerRouting":    {Resource: ResourceProxy, Action: ActionRead, ObjectIDField: "server_id"},
	"/discopanel.v1.ProxyService/UpdateServerRouting": {Resource: ResourceProxy, Action: ActionUpdate, ObjectIDField: "server_id"},

	// ── TaskService ────────────────────────────────────────────────────
	"/discopanel.v1.TaskService/ListTasks":            {Resource: ResourceTasks, Action: ActionRead, ObjectIDField: "server_id"},
	"/discopanel.v1.TaskService/GetTask":              {Resource: ResourceTasks, Action: ActionRead, ObjectIDField: "id"},
	"/discopanel.v1.TaskService/CreateTask":           {Resource: ResourceTasks, Action: ActionCreate, ObjectIDField: "server_id"},
	"/discopanel.v1.TaskService/UpdateTask":           {Resource: ResourceTasks, Action: ActionUpdate, ObjectIDField: "id"},
	"/discopanel.v1.TaskService/DeleteTask":           {Resource: ResourceTasks, Action: ActionDelete, ObjectIDField: "id"},
	"/discopanel.v1.TaskService/ToggleTask":           {Resource: ResourceTasks, Action: ActionUpdate, ObjectIDField: "id"},
	"/discopanel.v1.TaskService/TriggerTask":          {Resource: ResourceTasks, Action: ActionUpdate, ObjectIDField: "id"},
	"/discopanel.v1.TaskService/ListTaskExecutions":   {Resource: ResourceTasks, Action: ActionRead, ObjectIDField: "task_id"},
	"/discopanel.v1.TaskService/ListServerExecutions": {Resource: ResourceTasks, Action: ActionRead, ObjectIDField: "server_id"},
	"/discopanel.v1.TaskService/GetTaskExecution":     {Resource: ResourceTasks, Action: ActionRead, ObjectIDField: "id"},
	"/discopanel.v1.TaskService/CancelExecution":      {Resource: ResourceTasks, Action: ActionUpdate, ObjectIDField: "id"},
	"/discopanel.v1.TaskService/GetSchedulerStatus":   {Resource: ResourceTasks, Action: ActionRead},

	// ── UserService ────────────────────────────────────────────────────
	"/discopanel.v1.UserService/ListUsers":  {Resource: ResourceUsers, Action: ActionRead},
	"/discopanel.v1.UserService/GetUser":    {Resource: ResourceUsers, Action: ActionRead},
	"/discopanel.v1.UserService/CreateUser": {Resource: ResourceUsers, Action: ActionCreate},
	"/discopanel.v1.UserService/UpdateUser": {Resource: ResourceUsers, Action: ActionUpdate},
	"/discopanel.v1.UserService/DeleteUser": {Resource: ResourceUsers, Action: ActionDelete},

	// ── RoleService ────────────────────────────────────────────────────
	"/discopanel.v1.RoleService/ListRoles":           {Resource: ResourceRoles, Action: ActionRead},
	"/discopanel.v1.RoleService/GetRole":             {Resource: ResourceRoles, Action: ActionRead},
	"/discopanel.v1.RoleService/CreateRole":          {Resource: ResourceRoles, Action: ActionCreate},
	"/discopanel.v1.RoleService/UpdateRole":          {Resource: ResourceRoles, Action: ActionUpdate},
	"/discopanel.v1.RoleService/DeleteRole":          {Resource: ResourceRoles, Action: ActionDelete},
	"/discopanel.v1.RoleService/GetPermissionMatrix": {Resource: ResourceRoles, Action: ActionRead},
	"/discopanel.v1.RoleService/UpdatePermissions":   {Resource: ResourceRoles, Action: ActionUpdate},
	"/discopanel.v1.RoleService/AssignRole":          {Resource: ResourceRoles, Action: ActionCreate},
	"/discopanel.v1.RoleService/UnassignRole":        {Resource: ResourceRoles, Action: ActionDelete},
	"/discopanel.v1.RoleService/GetUserRoles":        {Resource: ResourceRoles, Action: ActionRead},

	// ── SupportService ─────────────────────────────────────────────────
	"/discopanel.v1.SupportService/GenerateSupportBundle": {Resource: ResourceSupport, Action: ActionCreate},
	"/discopanel.v1.SupportService/DownloadSupportBundle": {Resource: ResourceSupport, Action: ActionRead},
	"/discopanel.v1.SupportService/UploadSupportBundle":   {Resource: ResourceSupport, Action: ActionCreate},
	"/discopanel.v1.SupportService/GetApplicationLogs":    {Resource: ResourceSupport, Action: ActionRead},

	// ── UploadService ──────────────────────────────────────────────────
	"/discopanel.v1.UploadService/GetUploadStatus": {Resource: ResourceUploads, Action: ActionRead},
	"/discopanel.v1.UploadService/InitUpload":      {Resource: ResourceUploads, Action: ActionCreate},
	"/discopanel.v1.UploadService/UploadChunk":     {Resource: ResourceUploads, Action: ActionCreate},
	"/discopanel.v1.UploadService/CancelUpload":    {Resource: ResourceUploads, Action: ActionDelete},
}

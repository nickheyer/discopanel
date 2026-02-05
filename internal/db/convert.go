package db

import (
	"strconv"

	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// --- Server ---

func DBServerToProto(server *Server) *v1.Server {
	if server == nil {
		return nil
	}

	javaVersion, _ := strconv.ParseInt(server.JavaVersion, 10, 32)

	protoServer := &v1.Server{
		Id:              server.ID,
		Name:            server.Name,
		Description:     server.Description,
		McVersion:       server.MCVersion,
		Port:            int32(server.Port),
		ProxyHostname:   server.ProxyHostname,
		ProxyListenerId: server.ProxyListenerID,
		ProxyPort:       int32(server.ProxyPort),
		MaxPlayers:      int32(server.MaxPlayers),
		Memory:          int32(server.Memory),
		DataPath:        server.DataPath,
		ContainerId:     server.ContainerID,
		JavaVersion:     int32(javaVersion),
		DockerImage:     server.DockerImage,
		AutoStart:       server.AutoStart,
		Detached:        server.Detached,
		TpsCommand:      server.TPSCommand,
		MemoryUsage:     int64(server.MemoryUsage),
		CpuPercent:      server.CPUPercent,
		DiskUsage:       server.DiskUsage,
		DiskTotal:       server.DiskTotal,
		PlayersOnline:   int32(server.PlayersOnline),
		Tps:             server.TPS,
		AdditionalPorts: server.AdditionalPorts,
		CreatedAt:       timestamppb.New(server.CreatedAt),
		UpdatedAt:       timestamppb.New(server.UpdatedAt),
		SlpAvailable:    server.SLPAvailable,
		SlpLatencyMs:    server.SLPLatencyMs,
		Motd:            server.MOTD,
		ServerVersion:   server.ServerVersion,
		ProtocolVersion: int32(server.ProtocolVersion),
		PlayerSample:    server.PlayerSample,
		MaxPlayersSlp:   int32(server.MaxPlayersSLP),
		Favicon:         server.Favicon,
		DockerOverrides: server.DockerOverrides,
		ModLoader:       DBModLoaderToProto(server.ModLoader),
		Status:          DBStatusToProto(server.Status),
	}

	if server.LastStarted != nil {
		protoServer.LastStarted = timestamppb.New(*server.LastStarted)
	}

	return protoServer
}

// --- ModLoader ---

func DBModLoaderToProto(loader ModLoader) v1.ModLoader {
	switch loader {
	case ModLoaderVanilla:
		return v1.ModLoader_MOD_LOADER_VANILLA
	case ModLoaderForge:
		return v1.ModLoader_MOD_LOADER_FORGE
	case ModLoaderFabric:
		return v1.ModLoader_MOD_LOADER_FABRIC
	case ModLoaderQuilt:
		return v1.ModLoader_MOD_LOADER_QUILT
	case ModLoaderPaper:
		return v1.ModLoader_MOD_LOADER_PAPER
	case ModLoaderSpigot:
		return v1.ModLoader_MOD_LOADER_SPIGOT
	case ModLoaderBukkit:
		return v1.ModLoader_MOD_LOADER_BUKKIT
	case ModLoaderPurpur:
		return v1.ModLoader_MOD_LOADER_PURPUR
	case ModLoaderSpongeVanilla:
		return v1.ModLoader_MOD_LOADER_SPONGE_VANILLA
	case ModLoaderMohist:
		return v1.ModLoader_MOD_LOADER_MOHIST
	case ModLoaderCatserver:
		return v1.ModLoader_MOD_LOADER_CATSERVER
	case ModLoaderArclight:
		return v1.ModLoader_MOD_LOADER_ARCLIGHT
	case ModLoaderAutoCurseForge:
		return v1.ModLoader_MOD_LOADER_AUTO_CURSEFORGE
	case ModLoaderModrinth:
		return v1.ModLoader_MOD_LOADER_MODRINTH
	case ModLoaderNeoForge:
		return v1.ModLoader_MOD_LOADER_NEOFORGE
	default:
		return v1.ModLoader_MOD_LOADER_VANILLA
	}
}

func ProtoModLoaderToDB(loader v1.ModLoader) ModLoader {
	switch loader {
	case v1.ModLoader_MOD_LOADER_VANILLA:
		return ModLoaderVanilla
	case v1.ModLoader_MOD_LOADER_FORGE:
		return ModLoaderForge
	case v1.ModLoader_MOD_LOADER_FABRIC:
		return ModLoaderFabric
	case v1.ModLoader_MOD_LOADER_QUILT:
		return ModLoaderQuilt
	case v1.ModLoader_MOD_LOADER_PAPER:
		return ModLoaderPaper
	case v1.ModLoader_MOD_LOADER_SPIGOT:
		return ModLoaderSpigot
	case v1.ModLoader_MOD_LOADER_BUKKIT:
		return ModLoaderBukkit
	case v1.ModLoader_MOD_LOADER_PURPUR:
		return ModLoaderPurpur
	case v1.ModLoader_MOD_LOADER_SPONGE_VANILLA:
		return ModLoaderSpongeVanilla
	case v1.ModLoader_MOD_LOADER_MOHIST:
		return ModLoaderMohist
	case v1.ModLoader_MOD_LOADER_CATSERVER:
		return ModLoaderCatserver
	case v1.ModLoader_MOD_LOADER_ARCLIGHT:
		return ModLoaderArclight
	case v1.ModLoader_MOD_LOADER_AUTO_CURSEFORGE:
		return ModLoaderAutoCurseForge
	case v1.ModLoader_MOD_LOADER_MODRINTH:
		return ModLoaderModrinth
	case v1.ModLoader_MOD_LOADER_NEOFORGE:
		return ModLoaderNeoForge
	default:
		return ModLoaderVanilla
	}
}

// --- ServerStatus ---

func DBStatusToProto(status ServerStatus) v1.ServerStatus {
	switch status {
	case StatusCreating:
		return v1.ServerStatus_SERVER_STATUS_CREATING
	case StatusStarting:
		return v1.ServerStatus_SERVER_STATUS_STARTING
	case StatusRunning:
		return v1.ServerStatus_SERVER_STATUS_RUNNING
	case StatusStopping:
		return v1.ServerStatus_SERVER_STATUS_STOPPING
	case StatusStopped:
		return v1.ServerStatus_SERVER_STATUS_STOPPED
	case StatusError:
		return v1.ServerStatus_SERVER_STATUS_ERROR
	case StatusUnhealthy:
		return v1.ServerStatus_SERVER_STATUS_UNHEALTHY
	default:
		return v1.ServerStatus_SERVER_STATUS_UNSPECIFIED
	}
}

// --- User ---

func DBUserToProto(user *User) *v1.User {
	if user == nil {
		return nil
	}

	protoUser := &v1.User{
		Id:        user.ID,
		Username:  user.Username,
		IsActive:  user.IsActive,
		CreatedAt: timestamppb.New(user.CreatedAt),
		UpdatedAt: timestamppb.New(user.UpdatedAt),
	}

	switch user.Role {
	case RoleAdmin:
		protoUser.Role = v1.UserRole_USER_ROLE_ADMIN
	case RoleEditor:
		protoUser.Role = v1.UserRole_USER_ROLE_EDITOR
	case RoleViewer:
		protoUser.Role = v1.UserRole_USER_ROLE_VIEWER
	default:
		protoUser.Role = v1.UserRole_USER_ROLE_UNSPECIFIED
	}

	if user.Email != nil && *user.Email != "" {
		protoUser.Email = user.Email
	}

	return protoUser
}

func ProtoRoleToDBRole(role v1.UserRole) UserRole {
	switch role {
	case v1.UserRole_USER_ROLE_ADMIN:
		return RoleAdmin
	case v1.UserRole_USER_ROLE_EDITOR:
		return RoleEditor
	case v1.UserRole_USER_ROLE_VIEWER:
		return RoleViewer
	default:
		return RoleViewer
	}
}

// --- Module ---

func DBModuleTemplateTypeToProto(t ModuleTemplateType) v1.ModuleTemplateType {
	switch t {
	case ModuleTemplateTypeBuiltin:
		return v1.ModuleTemplateType_MODULE_TEMPLATE_TYPE_BUILTIN
	case ModuleTemplateTypeCustom:
		return v1.ModuleTemplateType_MODULE_TEMPLATE_TYPE_CUSTOM
	default:
		return v1.ModuleTemplateType_MODULE_TEMPLATE_TYPE_UNSPECIFIED
	}
}

func DBModuleStatusToProto(s ModuleStatus) v1.ModuleStatus {
	switch s {
	case ModuleStatusStopped:
		return v1.ModuleStatus_MODULE_STATUS_STOPPED
	case ModuleStatusStarting:
		return v1.ModuleStatus_MODULE_STATUS_STARTING
	case ModuleStatusRunning:
		return v1.ModuleStatus_MODULE_STATUS_RUNNING
	case ModuleStatusStopping:
		return v1.ModuleStatus_MODULE_STATUS_STOPPING
	case ModuleStatusError:
		return v1.ModuleStatus_MODULE_STATUS_ERROR
	case ModuleStatusCreating:
		return v1.ModuleStatus_MODULE_STATUS_CREATING
	default:
		return v1.ModuleStatus_MODULE_STATUS_UNSPECIFIED
	}
}

func DBModuleTemplateToProto(t *ModuleTemplate) *v1.ModuleTemplate {
	if t == nil {
		return nil
	}
	return &v1.ModuleTemplate{
		Id:                    t.ID,
		Name:                  t.Name,
		Description:           t.Description,
		Type:                  DBModuleTemplateTypeToProto(t.Type),
		DockerImage:           t.DockerImage,
		DefaultEnv:            t.DefaultEnv,
		DefaultVolumes:        t.DefaultVolumes,
		HealthCheckPath:       t.HealthCheckPath,
		HealthCheckPort:       int32(t.HealthCheckPort),
		RequiresServer:        t.RequiresServer,
		SupportsProxy:         t.SupportsProxy,
		Icon:                  t.Icon,
		Category:              t.Category,
		Documentation:         t.Documentation,
		CreatedAt:             timestamppb.New(t.CreatedAt),
		UpdatedAt:             timestamppb.New(t.UpdatedAt),
		Ports:                 t.Ports,
		SuggestedDependencies: t.SuggestedDependencies,
		DefaultHooks:          t.DefaultHooks,
		Metadata:              t.Metadata,
		DefaultCmd:            t.DefaultCmd,
		DefaultAccessUrls:     t.DefaultAccessUrls,
		DefaultMemory:         int32(t.DefaultMemory),
	}
}

func DBModuleToProto(m *Module, serverName, templateName, serverProxyHostname string) *v1.Module {
	if m == nil {
		return nil
	}

	protoModule := &v1.Module{
		Id:                    m.ID,
		Name:                  m.Name,
		ServerId:              m.ServerID,
		TemplateId:            m.TemplateID,
		ContainerId:           m.ContainerID,
		Status:                DBModuleStatusToProto(m.Status),
		Config:                m.Config,
		EnvOverrides:          m.EnvOverrides,
		VolumeOverrides:       m.VolumeOverrides,
		Memory:                int32(m.Memory),
		CpuLimit:              m.CPULimit,
		AutoStart:             m.AutoStart,
		FollowServerLifecycle: m.FollowServerLifecycle,
		Detached:              m.Detached,
		DataPath:              m.DataPath,
		CreatedAt:             timestamppb.New(m.CreatedAt),
		UpdatedAt:             timestamppb.New(m.UpdatedAt),
		MemoryUsage:           m.MemoryUsage,
		CpuPercent:            m.CPUPercent,
		ServerName:            serverName,
		TemplateName:          templateName,
		ServerProxyHostname:   serverProxyHostname,
		Ports:                 m.Ports,
		Dependencies:          m.Dependencies,
		HealthCheckInterval:   int32(m.HealthCheckInterval),
		HealthCheckTimeout:    int32(m.HealthCheckTimeout),
		HealthCheckRetries:    int32(m.HealthCheckRetries),
		EventHooks:            m.EventHooks,
		Metadata:              m.Metadata,
		CmdOverride:           m.CmdOverride,
		AccessUrls:            m.AccessUrls,
	}

	if m.LastStarted != nil {
		protoModule.LastStarted = timestamppb.New(*m.LastStarted)
	}

	return protoModule
}

// --- Task ---

func DBTaskTypeToProto(t TaskType) v1.TaskType {
	switch t {
	case TaskTypeCommand:
		return v1.TaskType_TASK_TYPE_COMMAND
	case TaskTypeBackup:
		return v1.TaskType_TASK_TYPE_BACKUP
	case TaskTypeRestart:
		return v1.TaskType_TASK_TYPE_RESTART
	case TaskTypeStart:
		return v1.TaskType_TASK_TYPE_START
	case TaskTypeStop:
		return v1.TaskType_TASK_TYPE_STOP
	case TaskTypeScript:
		return v1.TaskType_TASK_TYPE_SCRIPT
	default:
		return v1.TaskType_TASK_TYPE_UNSPECIFIED
	}
}

func ProtoTaskTypeToDB(t v1.TaskType) TaskType {
	switch t {
	case v1.TaskType_TASK_TYPE_COMMAND:
		return TaskTypeCommand
	case v1.TaskType_TASK_TYPE_BACKUP:
		return TaskTypeBackup
	case v1.TaskType_TASK_TYPE_RESTART:
		return TaskTypeRestart
	case v1.TaskType_TASK_TYPE_START:
		return TaskTypeStart
	case v1.TaskType_TASK_TYPE_STOP:
		return TaskTypeStop
	case v1.TaskType_TASK_TYPE_SCRIPT:
		return TaskTypeScript
	default:
		return TaskTypeCommand
	}
}

func DBTaskStatusToProto(s TaskStatus) v1.TaskStatus {
	switch s {
	case TaskStatusEnabled:
		return v1.TaskStatus_TASK_STATUS_ENABLED
	case TaskStatusDisabled:
		return v1.TaskStatus_TASK_STATUS_DISABLED
	case TaskStatusPaused:
		return v1.TaskStatus_TASK_STATUS_PAUSED
	default:
		return v1.TaskStatus_TASK_STATUS_UNSPECIFIED
	}
}

func ProtoTaskStatusToDB(s v1.TaskStatus) TaskStatus {
	switch s {
	case v1.TaskStatus_TASK_STATUS_ENABLED:
		return TaskStatusEnabled
	case v1.TaskStatus_TASK_STATUS_DISABLED:
		return TaskStatusDisabled
	case v1.TaskStatus_TASK_STATUS_PAUSED:
		return TaskStatusPaused
	default:
		return TaskStatusEnabled
	}
}

func DBScheduleTypeToProto(s ScheduleType) v1.ScheduleType {
	switch s {
	case ScheduleTypeCron:
		return v1.ScheduleType_SCHEDULE_TYPE_CRON
	case ScheduleTypeInterval:
		return v1.ScheduleType_SCHEDULE_TYPE_INTERVAL
	case ScheduleTypeOnce:
		return v1.ScheduleType_SCHEDULE_TYPE_ONCE
	default:
		return v1.ScheduleType_SCHEDULE_TYPE_UNSPECIFIED
	}
}

func ProtoScheduleTypeToDB(s v1.ScheduleType) ScheduleType {
	switch s {
	case v1.ScheduleType_SCHEDULE_TYPE_CRON:
		return ScheduleTypeCron
	case v1.ScheduleType_SCHEDULE_TYPE_INTERVAL:
		return ScheduleTypeInterval
	case v1.ScheduleType_SCHEDULE_TYPE_ONCE:
		return ScheduleTypeOnce
	default:
		return ScheduleTypeCron
	}
}

func DBExecutionStatusToProto(s ExecutionStatus) v1.ExecutionStatus {
	switch s {
	case ExecutionStatusPending:
		return v1.ExecutionStatus_EXECUTION_STATUS_PENDING
	case ExecutionStatusRunning:
		return v1.ExecutionStatus_EXECUTION_STATUS_RUNNING
	case ExecutionStatusCompleted:
		return v1.ExecutionStatus_EXECUTION_STATUS_COMPLETED
	case ExecutionStatusFailed:
		return v1.ExecutionStatus_EXECUTION_STATUS_FAILED
	case ExecutionStatusSkipped:
		return v1.ExecutionStatus_EXECUTION_STATUS_SKIPPED
	case ExecutionStatusCancelled:
		return v1.ExecutionStatus_EXECUTION_STATUS_CANCELLED
	case ExecutionStatusTimeout:
		return v1.ExecutionStatus_EXECUTION_STATUS_TIMEOUT
	default:
		return v1.ExecutionStatus_EXECUTION_STATUS_UNSPECIFIED
	}
}

func DBTaskToProto(task *ScheduledTask) *v1.ScheduledTask {
	if task == nil {
		return nil
	}

	protoTask := &v1.ScheduledTask{
		Id:            task.ID,
		ServerId:      task.ServerID,
		Name:          task.Name,
		Description:   task.Description,
		TaskType:      DBTaskTypeToProto(task.TaskType),
		Status:        DBTaskStatusToProto(task.Status),
		Schedule:      DBScheduleTypeToProto(task.Schedule),
		CronExpr:      task.CronExpr,
		IntervalSecs:  int32(task.IntervalSecs),
		Timezone:      task.Timezone,
		Config:        task.Config,
		Timeout:       int32(task.Timeout),
		RetryCount:    int32(task.RetryCount),
		RetryDelay:    int32(task.RetryDelay),
		RequireOnline: task.RequireOnline,
		FailureNotify: task.FailureNotify,
		CreatedAt:     timestamppb.New(task.CreatedAt),
		UpdatedAt:     timestamppb.New(task.UpdatedAt),
	}

	if task.RunAt != nil {
		protoTask.RunAt = timestamppb.New(*task.RunAt)
	}
	if task.NextRun != nil {
		protoTask.NextRun = timestamppb.New(*task.NextRun)
	}
	if task.LastRun != nil {
		protoTask.LastRun = timestamppb.New(*task.LastRun)
	}

	return protoTask
}

func DBExecutionToProto(exec *TaskExecution) *v1.TaskExecution {
	if exec == nil {
		return nil
	}

	protoExec := &v1.TaskExecution{
		Id:        exec.ID,
		TaskId:    exec.TaskID,
		ServerId:  exec.ServerID,
		Status:    DBExecutionStatusToProto(exec.Status),
		StartedAt: timestamppb.New(exec.StartedAt),
		Duration:  exec.Duration,
		Output:    exec.Output,
		Error:     exec.Error,
		RetryNum:  int32(exec.RetryNum),
		Trigger:   exec.Trigger,
	}

	if exec.EndedAt != nil {
		protoExec.EndedAt = timestamppb.New(*exec.EndedAt)
	}

	return protoExec
}

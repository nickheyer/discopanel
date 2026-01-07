package services

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/scheduler"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Compile-time check that TaskService implements the interface
var _ discopanelv1connect.TaskServiceHandler = (*TaskService)(nil)

// TaskService implements the Task service
type TaskService struct {
	store     *storage.Store
	scheduler *scheduler.Scheduler
	log       *logger.Logger
}

// NewTaskService creates a new task service
func NewTaskService(store *storage.Store, sched *scheduler.Scheduler, log *logger.Logger) *TaskService {
	return &TaskService{
		store:     store,
		scheduler: sched,
		log:       log,
	}
}

// dbTaskTypeToProto converts database task type to proto
func dbTaskTypeToProto(t storage.TaskType) v1.TaskType {
	switch t {
	case storage.TaskTypeCommand:
		return v1.TaskType_TASK_TYPE_COMMAND
	case storage.TaskTypeBackup:
		return v1.TaskType_TASK_TYPE_BACKUP
	case storage.TaskTypeRestart:
		return v1.TaskType_TASK_TYPE_RESTART
	case storage.TaskTypeStart:
		return v1.TaskType_TASK_TYPE_START
	case storage.TaskTypeStop:
		return v1.TaskType_TASK_TYPE_STOP
	case storage.TaskTypeScript:
		return v1.TaskType_TASK_TYPE_SCRIPT
	default:
		return v1.TaskType_TASK_TYPE_UNSPECIFIED
	}
}

// protoTaskTypeToDB converts proto task type to database
func protoTaskTypeToDB(t v1.TaskType) storage.TaskType {
	switch t {
	case v1.TaskType_TASK_TYPE_COMMAND:
		return storage.TaskTypeCommand
	case v1.TaskType_TASK_TYPE_BACKUP:
		return storage.TaskTypeBackup
	case v1.TaskType_TASK_TYPE_RESTART:
		return storage.TaskTypeRestart
	case v1.TaskType_TASK_TYPE_START:
		return storage.TaskTypeStart
	case v1.TaskType_TASK_TYPE_STOP:
		return storage.TaskTypeStop
	case v1.TaskType_TASK_TYPE_SCRIPT:
		return storage.TaskTypeScript
	default:
		return storage.TaskTypeCommand
	}
}

// dbTaskStatusToProto converts database task status to proto
func dbTaskStatusToProto(s storage.TaskStatus) v1.TaskStatus {
	switch s {
	case storage.TaskStatusEnabled:
		return v1.TaskStatus_TASK_STATUS_ENABLED
	case storage.TaskStatusDisabled:
		return v1.TaskStatus_TASK_STATUS_DISABLED
	case storage.TaskStatusPaused:
		return v1.TaskStatus_TASK_STATUS_PAUSED
	default:
		return v1.TaskStatus_TASK_STATUS_UNSPECIFIED
	}
}

// protoTaskStatusToDB converts proto task status to database
func protoTaskStatusToDB(s v1.TaskStatus) storage.TaskStatus {
	switch s {
	case v1.TaskStatus_TASK_STATUS_ENABLED:
		return storage.TaskStatusEnabled
	case v1.TaskStatus_TASK_STATUS_DISABLED:
		return storage.TaskStatusDisabled
	case v1.TaskStatus_TASK_STATUS_PAUSED:
		return storage.TaskStatusPaused
	default:
		return storage.TaskStatusEnabled
	}
}

// dbScheduleTypeToProto converts database schedule type to proto
func dbScheduleTypeToProto(s storage.ScheduleType) v1.ScheduleType {
	switch s {
	case storage.ScheduleTypeCron:
		return v1.ScheduleType_SCHEDULE_TYPE_CRON
	case storage.ScheduleTypeInterval:
		return v1.ScheduleType_SCHEDULE_TYPE_INTERVAL
	case storage.ScheduleTypeOnce:
		return v1.ScheduleType_SCHEDULE_TYPE_ONCE
	default:
		return v1.ScheduleType_SCHEDULE_TYPE_UNSPECIFIED
	}
}

// protoScheduleTypeToDB converts proto schedule type to database
func protoScheduleTypeToDB(s v1.ScheduleType) storage.ScheduleType {
	switch s {
	case v1.ScheduleType_SCHEDULE_TYPE_CRON:
		return storage.ScheduleTypeCron
	case v1.ScheduleType_SCHEDULE_TYPE_INTERVAL:
		return storage.ScheduleTypeInterval
	case v1.ScheduleType_SCHEDULE_TYPE_ONCE:
		return storage.ScheduleTypeOnce
	default:
		return storage.ScheduleTypeCron
	}
}

// dbExecutionStatusToProto converts database execution status to proto
func dbExecutionStatusToProto(s storage.ExecutionStatus) v1.ExecutionStatus {
	switch s {
	case storage.ExecutionStatusPending:
		return v1.ExecutionStatus_EXECUTION_STATUS_PENDING
	case storage.ExecutionStatusRunning:
		return v1.ExecutionStatus_EXECUTION_STATUS_RUNNING
	case storage.ExecutionStatusCompleted:
		return v1.ExecutionStatus_EXECUTION_STATUS_COMPLETED
	case storage.ExecutionStatusFailed:
		return v1.ExecutionStatus_EXECUTION_STATUS_FAILED
	case storage.ExecutionStatusSkipped:
		return v1.ExecutionStatus_EXECUTION_STATUS_SKIPPED
	case storage.ExecutionStatusCancelled:
		return v1.ExecutionStatus_EXECUTION_STATUS_CANCELLED
	case storage.ExecutionStatusTimeout:
		return v1.ExecutionStatus_EXECUTION_STATUS_TIMEOUT
	default:
		return v1.ExecutionStatus_EXECUTION_STATUS_UNSPECIFIED
	}
}

// dbTaskToProto converts a database task to proto
func dbTaskToProto(task *storage.ScheduledTask) *v1.ScheduledTask {
	if task == nil {
		return nil
	}

	protoTask := &v1.ScheduledTask{
		Id:            task.ID,
		ServerId:      task.ServerID,
		Name:          task.Name,
		Description:   task.Description,
		TaskType:      dbTaskTypeToProto(task.TaskType),
		Status:        dbTaskStatusToProto(task.Status),
		Schedule:      dbScheduleTypeToProto(task.Schedule),
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

// dbExecutionToProto converts a database execution to proto
func dbExecutionToProto(exec *storage.TaskExecution) *v1.TaskExecution {
	if exec == nil {
		return nil
	}

	protoExec := &v1.TaskExecution{
		Id:        exec.ID,
		TaskId:    exec.TaskID,
		ServerId:  exec.ServerID,
		Status:    dbExecutionStatusToProto(exec.Status),
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

// ListTasks lists all tasks for a server
func (s *TaskService) ListTasks(ctx context.Context, req *connect.Request[v1.ListTasksRequest]) (*connect.Response[v1.ListTasksResponse], error) {
	tasks, err := s.store.ListScheduledTasks(ctx, req.Msg.ServerId)
	if err != nil {
		s.log.Error("Failed to list tasks: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list tasks"))
	}

	protoTasks := make([]*v1.ScheduledTask, len(tasks))
	for i, task := range tasks {
		protoTasks[i] = dbTaskToProto(task)
	}

	return connect.NewResponse(&v1.ListTasksResponse{
		Tasks: protoTasks,
	}), nil
}

// GetTask gets a specific task
func (s *TaskService) GetTask(ctx context.Context, req *connect.Request[v1.GetTaskRequest]) (*connect.Response[v1.GetTaskResponse], error) {
	task, err := s.store.GetScheduledTask(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task not found"))
	}

	return connect.NewResponse(&v1.GetTaskResponse{
		Task: dbTaskToProto(task),
	}), nil
}

// CreateTask creates a new scheduled task
func (s *TaskService) CreateTask(ctx context.Context, req *connect.Request[v1.CreateTaskRequest]) (*connect.Response[v1.CreateTaskResponse], error) {
	msg := req.Msg

	// Validate server exists
	_, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Validate required fields
	if msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	// Validate cron expression if using cron schedule
	scheduleType := protoScheduleTypeToDB(msg.Schedule)
	if scheduleType == storage.ScheduleTypeCron && msg.CronExpr != "" {
		if err := s.scheduler.ValidateCronExpr(msg.CronExpr); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid cron expression: %v", err))
		}
	}

	// Create task
	task := &storage.ScheduledTask{
		ID:            uuid.New().String(),
		ServerID:      msg.ServerId,
		Name:          msg.Name,
		Description:   msg.Description,
		TaskType:      protoTaskTypeToDB(msg.TaskType),
		Status:        storage.TaskStatusEnabled,
		Schedule:      scheduleType,
		CronExpr:      msg.CronExpr,
		IntervalSecs:  int(msg.IntervalSecs),
		Timezone:      msg.Timezone,
		Config:        msg.Config,
		Timeout:       int(msg.Timeout),
		RetryCount:    int(msg.RetryCount),
		RetryDelay:    int(msg.RetryDelay),
		RequireOnline: msg.RequireOnline,
	}

	// Set defaults
	if task.Timezone == "" {
		task.Timezone = "UTC"
	}
	if task.Timeout == 0 {
		task.Timeout = 300 // 5 minutes default
	}

	// Set run_at for once schedule
	if msg.RunAt != nil {
		runAt := msg.RunAt.AsTime()
		task.RunAt = &runAt
	}

	// Calculate next run
	nextRun, err := s.scheduler.CalculateNextRun(task)
	if err != nil {
		s.log.Debug("Could not calculate next run: %v", err)
	}
	task.NextRun = nextRun

	// Save to database
	if err := s.store.CreateScheduledTask(ctx, task); err != nil {
		s.log.Error("Failed to create task: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create task"))
	}

	s.log.Info("Created scheduled task: %s for server %s", task.Name, task.ServerID)

	return connect.NewResponse(&v1.CreateTaskResponse{
		Task: dbTaskToProto(task),
	}), nil
}

// UpdateTask updates an existing task
func (s *TaskService) UpdateTask(ctx context.Context, req *connect.Request[v1.UpdateTaskRequest]) (*connect.Response[v1.UpdateTaskResponse], error) {
	msg := req.Msg

	task, err := s.store.GetScheduledTask(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task not found"))
	}

	// Update fields if provided
	if msg.Name != nil {
		task.Name = *msg.Name
	}
	if msg.Description != nil {
		task.Description = *msg.Description
	}
	if msg.TaskType != nil {
		task.TaskType = protoTaskTypeToDB(*msg.TaskType)
	}
	if msg.Schedule != nil {
		task.Schedule = protoScheduleTypeToDB(*msg.Schedule)
	}
	if msg.CronExpr != nil {
		// Validate cron expression
		if task.Schedule == storage.ScheduleTypeCron && *msg.CronExpr != "" {
			if err := s.scheduler.ValidateCronExpr(*msg.CronExpr); err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid cron expression: %v", err))
			}
		}
		task.CronExpr = *msg.CronExpr
	}
	if msg.IntervalSecs != nil {
		task.IntervalSecs = int(*msg.IntervalSecs)
	}
	if msg.RunAt != nil {
		runAt := msg.RunAt.AsTime()
		task.RunAt = &runAt
	}
	if msg.Timezone != nil {
		task.Timezone = *msg.Timezone
	}
	if msg.Config != nil {
		task.Config = *msg.Config
	}
	if msg.Timeout != nil {
		task.Timeout = int(*msg.Timeout)
	}
	if msg.RetryCount != nil {
		task.RetryCount = int(*msg.RetryCount)
	}
	if msg.RetryDelay != nil {
		task.RetryDelay = int(*msg.RetryDelay)
	}
	if msg.RequireOnline != nil {
		task.RequireOnline = *msg.RequireOnline
	}

	// Recalculate next run
	nextRun, _ := s.scheduler.CalculateNextRun(task)
	task.NextRun = nextRun

	// Save changes
	if err := s.store.UpdateScheduledTask(ctx, task); err != nil {
		s.log.Error("Failed to update task: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update task"))
	}

	s.log.Info("Updated scheduled task: %s", task.Name)

	return connect.NewResponse(&v1.UpdateTaskResponse{
		Task: dbTaskToProto(task),
	}), nil
}

// DeleteTask deletes a task
func (s *TaskService) DeleteTask(ctx context.Context, req *connect.Request[v1.DeleteTaskRequest]) (*connect.Response[v1.DeleteTaskResponse], error) {
	task, err := s.store.GetScheduledTask(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task not found"))
	}

	if err := s.store.DeleteScheduledTask(ctx, req.Msg.Id); err != nil {
		s.log.Error("Failed to delete task: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete task"))
	}

	s.log.Info("Deleted scheduled task: %s", task.Name)

	return connect.NewResponse(&v1.DeleteTaskResponse{}), nil
}

// ToggleTask toggles task enabled/disabled status
func (s *TaskService) ToggleTask(ctx context.Context, req *connect.Request[v1.ToggleTaskRequest]) (*connect.Response[v1.ToggleTaskResponse], error) {
	task, err := s.store.GetScheduledTask(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task not found"))
	}

	task.Status = protoTaskStatusToDB(req.Msg.Status)

	// Recalculate next run if enabling
	if task.Status == storage.TaskStatusEnabled {
		nextRun, _ := s.scheduler.CalculateNextRun(task)
		task.NextRun = nextRun
	}

	if err := s.store.UpdateScheduledTask(ctx, task); err != nil {
		s.log.Error("Failed to toggle task: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to toggle task"))
	}

	s.log.Info("Toggled task %s to status %s", task.Name, task.Status)

	return connect.NewResponse(&v1.ToggleTaskResponse{
		Task: dbTaskToProto(task),
	}), nil
}

// TriggerTask manually triggers a task execution
func (s *TaskService) TriggerTask(ctx context.Context, req *connect.Request[v1.TriggerTaskRequest]) (*connect.Response[v1.TriggerTaskResponse], error) {
	execution, err := s.scheduler.TriggerTask(ctx, req.Msg.Id)
	if err != nil {
		s.log.Error("Failed to trigger task: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to trigger task: %v", err))
	}

	return connect.NewResponse(&v1.TriggerTaskResponse{
		Execution: dbExecutionToProto(execution),
	}), nil
}

// ListTaskExecutions gets execution history for a task
func (s *TaskService) ListTaskExecutions(ctx context.Context, req *connect.Request[v1.ListTaskExecutionsRequest]) (*connect.Response[v1.ListTaskExecutionsResponse], error) {
	limit := int(req.Msg.Limit)
	if limit == 0 {
		limit = 50 // Default limit
	}

	executions, err := s.store.ListTaskExecutions(ctx, req.Msg.TaskId, limit)
	if err != nil {
		s.log.Error("Failed to list task executions: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list executions"))
	}

	protoExecs := make([]*v1.TaskExecution, len(executions))
	for i, exec := range executions {
		protoExecs[i] = dbExecutionToProto(exec)
	}

	return connect.NewResponse(&v1.ListTaskExecutionsResponse{
		Executions: protoExecs,
	}), nil
}

// ListServerExecutions gets execution history for a server
func (s *TaskService) ListServerExecutions(ctx context.Context, req *connect.Request[v1.ListServerExecutionsRequest]) (*connect.Response[v1.ListServerExecutionsResponse], error) {
	limit := int(req.Msg.Limit)
	if limit == 0 {
		limit = 50 // Default limit
	}

	executions, err := s.store.ListServerTaskExecutions(ctx, req.Msg.ServerId, limit)
	if err != nil {
		s.log.Error("Failed to list server executions: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list executions"))
	}

	protoExecs := make([]*v1.TaskExecution, len(executions))
	for i, exec := range executions {
		protoExecs[i] = dbExecutionToProto(exec)
	}

	return connect.NewResponse(&v1.ListServerExecutionsResponse{
		Executions: protoExecs,
	}), nil
}

// GetTaskExecution gets a specific execution
func (s *TaskService) GetTaskExecution(ctx context.Context, req *connect.Request[v1.GetTaskExecutionRequest]) (*connect.Response[v1.GetTaskExecutionResponse], error) {
	execution, err := s.store.GetTaskExecution(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("execution not found"))
	}

	return connect.NewResponse(&v1.GetTaskExecutionResponse{
		Execution: dbExecutionToProto(execution),
	}), nil
}

// CancelExecution cancels a running execution
func (s *TaskService) CancelExecution(ctx context.Context, req *connect.Request[v1.CancelExecutionRequest]) (*connect.Response[v1.CancelExecutionResponse], error) {
	if err := s.scheduler.CancelExecution(req.Msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("execution not found or already finished"))
	}

	// Wait briefly for cancellation to be recorded
	execution, err := s.store.GetTaskExecution(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("execution not found"))
	}

	return connect.NewResponse(&v1.CancelExecutionResponse{
		Execution: dbExecutionToProto(execution),
	}), nil
}

// GetSchedulerStatus gets the scheduler status
func (s *TaskService) GetSchedulerStatus(ctx context.Context, req *connect.Request[v1.GetSchedulerStatusRequest]) (*connect.Response[v1.GetSchedulerStatusResponse], error) {
	status := s.scheduler.GetStatus()

	response := &v1.GetSchedulerStatusResponse{
		Running:           status.Running,
		ActiveTasks:       int32(status.ActiveTasks),
		RunningExecutions: int32(status.RunningExecutions),
	}

	if !status.LastCheck.IsZero() {
		response.LastCheck = timestamppb.New(status.LastCheck)
	}
	if !status.NextCheck.IsZero() {
		response.NextCheck = timestamppb.New(status.NextCheck)
	}

	return connect.NewResponse(response), nil
}

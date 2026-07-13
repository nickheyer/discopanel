package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/nickheyer/discopanel/internal/activity"
	"github.com/nickheyer/discopanel/internal/command"
	appconfig "github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/events"
	"github.com/nickheyer/discopanel/internal/lifecycle"
	"github.com/nickheyer/discopanel/internal/metrics"
	"github.com/nickheyer/discopanel/internal/webhook"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

// Manages scheduled tasks for all servers
type Scheduler struct {
	store         *storage.Store
	docker        *docker.Client
	sender        *command.Sender
	lifecycle     *lifecycle.Manager
	appConfig     *appconfig.Config
	metrics       *metrics.Collector
	rec           *activity.Recorder
	log           *logger.Logger
	checkInterval time.Duration

	// State management
	running  bool
	mu       sync.RWMutex
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Execution tracking
	runningExecutions map[string]context.CancelFunc // Maps execution id to its cancel func
	inFlightTasks     map[string]bool               // Task ids currently executing
	executionMu       sync.RWMutex

	// Cron parser
	cronParser cron.Parser

	// Stats
	lastCheck time.Time
	nextCheck time.Time
}

// Holds scheduler configuration
type Config struct {
	CheckInterval time.Duration // How often to check for due tasks
}

// Returns default scheduler configuration
func DefaultConfig() Config {
	return Config{
		CheckInterval: 10 * time.Second,
	}
}

// Creates a new task scheduler
func NewScheduler(store *storage.Store, docker *docker.Client, sender *command.Sender, lifecycleManager *lifecycle.Manager, appCfg *appconfig.Config, metricsCollector *metrics.Collector, rec *activity.Recorder, log *logger.Logger, config ...Config) *Scheduler {
	cfg := DefaultConfig()
	if len(config) > 0 {
		cfg = config[0]
	}

	return &Scheduler{
		store:             store,
		docker:            docker,
		sender:            sender,
		lifecycle:         lifecycleManager,
		appConfig:         appCfg,
		metrics:           metricsCollector,
		rec:               rec,
		log:               log,
		checkInterval:     cfg.CheckInterval,
		stopChan:          make(chan struct{}),
		runningExecutions: make(map[string]context.CancelFunc),
		inFlightTasks:     make(map[string]bool),
		cronParser:        cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

// Begins the scheduler loop
func (s *Scheduler) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("scheduler already running")
	}

	s.running = true
	s.stopChan = make(chan struct{})

	s.wg.Add(1)
	go s.runLoop()

	s.log.Info("Task scheduler started (check interval: %v)", s.checkInterval)
	return nil
}

// Gracefully stops the scheduler
func (s *Scheduler) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	close(s.stopChan)
	s.mu.Unlock()

	// Wait for scheduler loop to finish
	s.wg.Wait()

	// Cancel all running executions
	s.executionMu.Lock()
	for _, cancel := range s.runningExecutions {
		cancel()
	}
	s.runningExecutions = make(map[string]context.CancelFunc)
	s.executionMu.Unlock()

	s.log.Info("Task scheduler stopped")
	return nil
}

// Returns whether the scheduler is running
func (s *Scheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// Returns current scheduler status
func (s *Scheduler) GetStatus() SchedulerStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.executionMu.RLock()
	runningCount := len(s.runningExecutions)
	s.executionMu.RUnlock()

	// Count active tasks
	ctx := context.Background()
	tasks, _ := s.store.ListAllScheduledTasks(ctx)
	activeCount := 0
	for _, task := range tasks {
		if task.Status == storage.TaskStatusEnabled {
			activeCount++
		}
	}

	return SchedulerStatus{
		Running:           s.running,
		ActiveTasks:       activeCount,
		RunningExecutions: runningCount,
		LastCheck:         s.lastCheck,
		NextCheck:         s.nextCheck,
	}
}

// Represents the current state of the scheduler
type SchedulerStatus struct {
	Running           bool
	ActiveTasks       int
	RunningExecutions int
	LastCheck         time.Time
	NextCheck         time.Time
}

// Main scheduler loop
func (s *Scheduler) runLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	// Run initial check
	s.checkAndRunDueTasks()

	for {
		select {
		case <-ticker.C:
			s.checkAndRunDueTasks()
		case <-s.stopChan:
			return
		}
	}
}

// Checks for due tasks and executes them
func (s *Scheduler) checkAndRunDueTasks() {
	s.mu.Lock()
	s.lastCheck = time.Now()
	s.nextCheck = s.lastCheck.Add(s.checkInterval)
	s.mu.Unlock()

	ctx := context.Background()

	// Get all due tasks
	tasks, err := s.store.ListDueScheduledTasks(ctx, time.Now())
	if err != nil {
		s.log.Error("Failed to list due tasks: %v", err)
		return
	}

	for _, task := range tasks {
		// Execute task asynchronously
		s.wg.Add(1)
		go func(t *storage.ScheduledTask) {
			defer s.wg.Done()
			s.executeTask(t, "scheduled", v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_UNSPECIFIED, nil)
		}(task)
	}
}

// Manually triggers a task execution
func (s *Scheduler) TriggerTask(ctx context.Context, taskID string) (*storage.TaskExecution, error) {
	task, err := s.store.GetScheduledTask(ctx, taskID)
	if err != nil {
		return nil, err
	}

	execution, err := s.executeTask(task, "manual", v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_UNSPECIFIED, nil)
	return execution, err
}

// Schedulers subscription to the central event bus
func (s *Scheduler) HandleServerEvent(ctx context.Context, event events.Event) {
	tasks, err := s.store.ListEventTriggeredTasks(ctx, event.ServerID, event.Type)
	if err != nil {
		s.log.Error("Failed to list event-triggered tasks for %s: %v", event.Type, err)
		return
	}
	for _, task := range tasks {
		s.wg.Add(1)
		go func(t *storage.ScheduledTask) {
			defer s.wg.Done()
			s.executeTaskForEvent(t, event.Type, event.Data)
		}(task)
	}
}

// Runs a task from an event, threads type to webhooks
func (s *Scheduler) executeTaskForEvent(task *storage.ScheduledTask, eventType v1.TriggeredEventType, eventData map[string]any) {
	s.executeTask(task, "event", eventType, eventData)
}

// Marks a task in flight unless it already is
func (s *Scheduler) tryBeginTask(taskID string) bool {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	if s.inFlightTasks[taskID] {
		return false
	}
	s.inFlightTasks[taskID] = true
	return true
}

// Clears the task's in-flight mark
func (s *Scheduler) endTask(taskID string) {
	s.executionMu.Lock()
	delete(s.inFlightTasks, taskID)
	s.executionMu.Unlock()
}

// Runs a single task, trigger names what drove it
func (s *Scheduler) executeTask(task *storage.ScheduledTask, trigger string, eventType v1.TriggeredEventType, eventData map[string]any) (*storage.TaskExecution, error) {
	ctx := context.Background()

	if !s.tryBeginTask(task.ID) {
		s.log.Debug("Task %s: skipped, previous run still in flight", task.Name)
		return nil, fmt.Errorf("task %q is already running", task.Name)
	}
	defer s.endTask(task.ID)

	// Advance schedule before running so re-listing never doubles
	s.updateNextRun(task)

	// Check if server exists
	server, err := s.store.GetServer(ctx, task.ServerID)
	if err != nil {
		s.log.Error("Task %s: server not found: %v", task.Name, err)
		return nil, err
	}

	// Checks if server is online, webhook tasks always fire
	if task.RequireOnline && task.TaskType != storage.TaskTypeWebhook && server.Status != storage.StatusRunning {
		s.log.Debug("Task %s: skipped (server offline)", task.Name)

		// Create skipped execution record
		execution := &storage.TaskExecution{
			ID:        uuid.New().String(),
			TaskID:    task.ID,
			ServerID:  task.ServerID,
			Status:    storage.ExecutionStatusSkipped,
			StartedAt: time.Now(),
			Trigger:   trigger,
			Error:     "server offline",
		}
		now := time.Now()
		execution.EndedAt = &now
		s.store.CreateTaskExecution(ctx, execution)
		return execution, nil
	}

	// Create execution record
	execution := &storage.TaskExecution{
		ID:        uuid.New().String(),
		TaskID:    task.ID,
		ServerID:  task.ServerID,
		Status:    storage.ExecutionStatusRunning,
		StartedAt: time.Now(),
		Trigger:   trigger,
	}
	if err := s.store.CreateTaskExecution(ctx, execution); err != nil {
		s.log.Error("Task %s: failed to create execution record: %v", task.Name, err)
		return nil, err
	}

	// Create cancellable context with timeout
	timeout := time.Duration(task.Timeout) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute // Default timeout
	}
	execCtx, cancel := context.WithTimeout(activity.WithTrace(activity.WithSource(ctx, "scheduler")), timeout)

	// Track running execution
	s.executionMu.Lock()
	s.runningExecutions[execution.ID] = cancel
	s.executionMu.Unlock()

	defer func() {
		cancel()
		s.executionMu.Lock()
		delete(s.runningExecutions, execution.ID)
		s.executionMu.Unlock()
	}()

	s.log.Info("Task %s: executing on server %s (trigger: %s)", task.Name, server.Name, trigger)

	// Executes the task, retrying on failure if configured
	var output string
	var execErr error

	for attempt := 0; ; attempt++ {
		output, execErr = s.runTaskType(execCtx, server, task, eventType, eventData)
		if execErr == nil || attempt >= task.RetryCount || execCtx.Err() != nil {
			break
		}

		retryDelay := time.Duration(task.RetryDelay) * time.Second
		if retryDelay <= 0 {
			retryDelay = time.Minute
		}
		s.log.Warn("Task %s: attempt %d failed, retrying in %v: %v", task.Name, attempt+1, retryDelay, execErr)

		select {
		case <-execCtx.Done():
		case <-time.After(retryDelay):
		}
		if execCtx.Err() != nil {
			break
		}
		execution.RetryNum = attempt + 1
	}

	// Update execution record
	endTime := time.Now()
	execution.EndedAt = &endTime
	execution.Duration = endTime.Sub(execution.StartedAt).Milliseconds()
	execution.Output = output

	if execErr != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			execution.Status = storage.ExecutionStatusTimeout
			execution.Error = "execution timed out"
		} else if execCtx.Err() == context.Canceled {
			execution.Status = storage.ExecutionStatusCancelled
			execution.Error = "execution cancelled"
		} else {
			execution.Status = storage.ExecutionStatusFailed
			execution.Error = execErr.Error()
		}
		s.log.Error("Task %s: failed: %v", task.Name, execErr)
	} else {
		execution.Status = storage.ExecutionStatusCompleted
		s.log.Info("Task %s: completed successfully", task.Name)
	}

	s.store.UpdateTaskExecution(ctx, execution)

	return execution, execErr
}

// Dispatches a single execution attempt to its executor
func (s *Scheduler) runTaskType(ctx context.Context, server *storage.Server, task *storage.ScheduledTask, eventType v1.TriggeredEventType, eventData map[string]any) (string, error) {
	switch task.TaskType {
	case storage.TaskTypeCommand:
		return s.executeCommandTask(ctx, server, task)
	case storage.TaskTypeRestart:
		return s.executeRestartTask(ctx, server, task)
	case storage.TaskTypeStart:
		return s.executeStartTask(ctx, server, task)
	case storage.TaskTypeStop:
		return s.executeStopTask(ctx, server, task)
	case storage.TaskTypeBackup:
		return s.executeBackupTask(ctx, server, task)
	case storage.TaskTypeScript:
		return s.executeScriptTask(ctx, server, task)
	case storage.TaskTypeWebhook:
		return s.executeWebhookTask(ctx, server, task, eventType, eventData)
	default:
		return "", fmt.Errorf("unknown task type: %s", task.TaskType)
	}
}

// Cancels a running execution
func (s *Scheduler) CancelExecution(executionID string) error {
	s.executionMu.RLock()
	cancel, exists := s.runningExecutions[executionID]
	s.executionMu.RUnlock()

	if !exists {
		return fmt.Errorf("execution not found or already finished")
	}

	cancel()
	return nil
}

// Calculates and persists the next run time
func (s *Scheduler) updateNextRun(task *storage.ScheduledTask) {
	ctx := context.Background()
	now := time.Now()
	var nextRun *time.Time
	var status storage.TaskStatus

	switch task.Schedule {
	case storage.ScheduleTypeCron:
		if task.CronExpr != "" {
			schedule, err := s.cronParser.Parse(task.CronExpr)
			if err == nil {
				next := schedule.Next(now)
				nextRun = &next
			}
		}
	case storage.ScheduleTypeInterval:
		if task.IntervalSecs > 0 {
			next := now.Add(time.Duration(task.IntervalSecs) * time.Second)
			nextRun = &next
		}
	case storage.ScheduleTypeOnce:
		// Once tasks never repeat, disable on first fire
		task.Status = storage.TaskStatusDisabled
		status = storage.TaskStatusDisabled
		nextRun = nil
	case storage.ScheduleTypeEvent:
		// Event-triggered tasks have no time-based next run
		nextRun = nil
	}

	if err := s.store.UpdateTaskNextRun(ctx, task.ID, nextRun, &now, status); err != nil {
		s.log.Error("Task %s: failed to persist next run: %v", task.Name, err)
	}
}

// Task type executors

// Configuration for command tasks
type CommandTaskConfig struct {
	Command string `json:"command"`
}

func (s *Scheduler) executeCommandTask(ctx context.Context, server *storage.Server, task *storage.ScheduledTask) (string, error) {
	var config CommandTaskConfig
	if task.Config != "" {
		if err := json.Unmarshal([]byte(task.Config), &config); err != nil {
			return "", fmt.Errorf("invalid command config: %w", err)
		}
	}

	if config.Command == "" {
		return "", fmt.Errorf("no command specified")
	}

	if server.ContainerID == "" {
		return "", fmt.Errorf("server has no container")
	}

	output, err := s.sender.SendCommand(ctx, server.ID, config.Command)
	if err == nil {
		s.rec.Record(ctx, server.ID, "task.command", activity.Attrs{"command": config.Command, "task": task.Name}, "ran command %q (task %q)", config.Command, task.Name)
	}
	return output, err
}

func (s *Scheduler) executeRestartTask(ctx context.Context, server *storage.Server, _ *storage.ScheduledTask) (string, error) {
	if err := s.lifecycle.Restart(ctx, server.ID); err != nil {
		return "", err
	}
	return "server restarted successfully", nil
}

func (s *Scheduler) executeStartTask(ctx context.Context, server *storage.Server, _ *storage.ScheduledTask) (string, error) {
	if err := s.lifecycle.Start(ctx, server.ID); err != nil {
		return "", err
	}
	return "server started successfully", nil
}

func (s *Scheduler) executeStopTask(ctx context.Context, server *storage.Server, _ *storage.ScheduledTask) (string, error) {
	if err := s.lifecycle.Stop(ctx, server.ID); err != nil {
		return "", err
	}
	return "server stopped successfully", nil
}

// Configuration for script tasks
type ScriptTaskConfig struct {
	ScriptPath string   `json:"script_path"`
	Args       []string `json:"args"`
}

func (s *Scheduler) executeScriptTask(ctx context.Context, server *storage.Server, task *storage.ScheduledTask) (string, error) {
	// Script tasks execute inside the container
	var config ScriptTaskConfig
	if task.Config != "" {
		if err := json.Unmarshal([]byte(task.Config), &config); err != nil {
			return "", fmt.Errorf("invalid config: %w", err)
		}
	}

	if config.ScriptPath == "" {
		return "", fmt.Errorf("no script/executable specified")
	}

	execCmd := []string{config.ScriptPath}
	stdout, stderr, err := s.docker.Exec(ctx, server.ContainerID, append(execCmd, config.Args...))
	if err != nil {
		return "", err
	}
	s.rec.Record(ctx, server.ID, "task.script", activity.Attrs{"script": config.ScriptPath, "task": task.Name}, "ran script %s (task %q)", config.ScriptPath, task.Name)
	if strings.TrimSpace(stderr) != "" {
		return stdout + "\n[stderr]\n" + stderr, nil
	}
	return stdout, nil
}

// Calculates the next run time based on schedule
func (s *Scheduler) CalculateNextRun(task *storage.ScheduledTask) (*time.Time, error) {
	now := time.Now()

	switch task.Schedule {
	case storage.ScheduleTypeCron:
		if task.CronExpr == "" {
			return nil, fmt.Errorf("cron expression required")
		}
		schedule, err := s.cronParser.Parse(task.CronExpr)
		if err != nil {
			return nil, fmt.Errorf("invalid cron expression: %w", err)
		}
		next := schedule.Next(now)
		return &next, nil

	case storage.ScheduleTypeInterval:
		if task.IntervalSecs <= 0 {
			return nil, fmt.Errorf("interval must be positive")
		}
		next := now.Add(time.Duration(task.IntervalSecs) * time.Second)
		return &next, nil

	case storage.ScheduleTypeOnce:
		if task.RunAt == nil {
			return nil, fmt.Errorf("run_at time required for once schedule")
		}
		if task.RunAt.Before(now) {
			return nil, nil // Already passed
		}
		return task.RunAt, nil

	case storage.ScheduleTypeEvent:
		// No scheduled time, execution is triggered via OnEvent
		return nil, nil

	default:
		return nil, fmt.Errorf("unknown schedule type: %s", task.Schedule)
	}
}

// Validates a cron expression
func (s *Scheduler) ValidateCronExpr(expr string) error {
	_, err := s.cronParser.Parse(expr)
	return err
}

func (s *Scheduler) executeWebhookTask(ctx context.Context, server *storage.Server, task *storage.ScheduledTask, eventType v1.TriggeredEventType, eventData map[string]any) (string, error) {
	var cfg webhook.Config
	if task.Config != "" {
		if err := json.Unmarshal([]byte(task.Config), &cfg); err != nil {
			return "", fmt.Errorf("invalid webhook config: %w", err)
		}
	}
	if cfg.URL == "" {
		return "", fmt.Errorf("webhook URL is required")
	}

	// Event runs pass the firing event, schedules fall back
	var event string
	switch {
	case eventType != v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_UNSPECIFIED:
		event = webhookEventName(eventType)
	case len(task.EventTriggers) > 0:
		event = webhookEventName(task.EventTriggers[0])
	default:
		event = "manual"
	}

	// Pull live count from metrics so payloads report players accurately
	if s.metrics != nil {
		if m := s.metrics.GetMetrics(server.ID); m != nil {
			server.PlayersOnline = m.PlayersOnline
		}
	}

	payload := webhook.BuildPayload(event, server, eventData)

	result := webhook.Deliver(ctx, cfg, payload)
	output := fmt.Sprintf("HTTP %d in %dms (attempt %d)", result.ResponseCode, result.DurationMs, result.Attempts)
	if result.ResponseBody != "" {
		output += "\n" + result.ResponseBody
	}
	if result.Success {
		return output, nil
	}
	return output, fmt.Errorf("%s", result.ErrorMessage)
}

// Maps event types to lowercase webhook event names
func webhookEventName(t v1.TriggeredEventType) string {
	switch t {
	case v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START:
		return "server_start"
	case v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_STOP:
		return "server_stop"
	case v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_RESTART:
		return "server_restart"
	case v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_HEALTHY:
		return "server_healthy"
	case v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_PLAYER_JOIN:
		return "player_join"
	case v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_PLAYER_LEAVE:
		return "player_leave"
	default:
		return "manual"
	}
}

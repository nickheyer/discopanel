package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/pkg/logger"
)

// Scheduler manages scheduled tasks for all servers
type Scheduler struct {
	store         *storage.Store
	docker        *docker.Client
	log           *logger.Logger
	checkInterval time.Duration

	// State management
	running  bool
	mu       sync.RWMutex
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Execution tracking
	runningExecutions map[string]context.CancelFunc // executionID -> cancel func
	executionMu       sync.RWMutex

	// Cron parser
	cronParser cron.Parser

	// Stats
	lastCheck time.Time
	nextCheck time.Time
}

// Config holds scheduler configuration
type Config struct {
	CheckInterval time.Duration // How often to check for due tasks
}

// DefaultConfig returns default scheduler configuration
func DefaultConfig() Config {
	return Config{
		CheckInterval: 10 * time.Second,
	}
}

// NewScheduler creates a new task scheduler
func NewScheduler(store *storage.Store, docker *docker.Client, log *logger.Logger, config ...Config) *Scheduler {
	cfg := DefaultConfig()
	if len(config) > 0 {
		cfg = config[0]
	}

	return &Scheduler{
		store:             store,
		docker:            docker,
		log:               log,
		checkInterval:     cfg.CheckInterval,
		stopChan:          make(chan struct{}),
		runningExecutions: make(map[string]context.CancelFunc),
		cronParser:        cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

// Start begins the scheduler loop
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

// Stop gracefully stops the scheduler
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

// IsRunning returns whether the scheduler is running
func (s *Scheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// GetStatus returns current scheduler status
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

// SchedulerStatus represents the current state of the scheduler
type SchedulerStatus struct {
	Running           bool
	ActiveTasks       int
	RunningExecutions int
	LastCheck         time.Time
	NextCheck         time.Time
}

// runLoop is the main scheduler loop
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

// checkAndRunDueTasks checks for due tasks and executes them
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
			s.executeTask(t, "scheduled")
		}(task)
	}
}

// TriggerTask manually triggers a task execution
func (s *Scheduler) TriggerTask(ctx context.Context, taskID string) (*storage.TaskExecution, error) {
	task, err := s.store.GetScheduledTask(ctx, taskID)
	if err != nil {
		return nil, err
	}

	execution, err := s.executeTask(task, "manual")
	return execution, err
}

// executeTask runs a single task
func (s *Scheduler) executeTask(task *storage.ScheduledTask, trigger string) (*storage.TaskExecution, error) {
	ctx := context.Background()

	// Check if server exists
	server, err := s.store.GetServer(ctx, task.ServerID)
	if err != nil {
		s.log.Error("Task %s: server not found: %v", task.Name, err)
		return nil, err
	}

	// Check if server is online (if required)
	if task.RequireOnline && server.Status != storage.StatusRunning {
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

		// Update next run time
		s.updateNextRun(task)
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
	execCtx, cancel := context.WithTimeout(ctx, timeout)

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

	// Execute the task based on type
	var output string
	var execErr error

	switch task.TaskType {
	case storage.TaskTypeCommand:
		output, execErr = s.executeCommandTask(execCtx, server, task)
	case storage.TaskTypeRestart:
		output, execErr = s.executeRestartTask(execCtx, server, task)
	case storage.TaskTypeStart:
		output, execErr = s.executeStartTask(execCtx, server, task)
	case storage.TaskTypeStop:
		output, execErr = s.executeStopTask(execCtx, server, task)
	case storage.TaskTypeBackup:
		output, execErr = s.executeBackupTask(execCtx, server, task)
	case storage.TaskTypeScript:
		output, execErr = s.executeScriptTask(execCtx, server, task)
	default:
		execErr = fmt.Errorf("unknown task type: %s", task.TaskType)
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

	// Update next run time
	s.updateNextRun(task)

	return execution, execErr
}

// CancelExecution cancels a running execution
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

// updateNextRun calculates and updates the next run time for a task
func (s *Scheduler) updateNextRun(task *storage.ScheduledTask) {
	ctx := context.Background()
	now := time.Now()
	var nextRun *time.Time

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
		// Once tasks don't repeat, disable after execution
		task.Status = storage.TaskStatusDisabled
		nextRun = nil
	}

	s.store.UpdateTaskNextRun(ctx, task.ID, nextRun, &now)
}

// Task type executors

// CommandTaskConfig represents configuration for command tasks
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

	output, err := s.docker.ExecCommand(ctx, server.ContainerID, config.Command)
	return output, err
}

func (s *Scheduler) executeRestartTask(ctx context.Context, server *storage.Server, task *storage.ScheduledTask) (string, error) {
	if server.ContainerID == "" {
		return "", fmt.Errorf("server has no container")
	}

	// Stop container
	found, err := s.docker.StopContainer(ctx, server.ContainerID)
	if err != nil {
		return "", fmt.Errorf("failed to stop: %w", err)
	}
	if !found {
		server.ContainerID = ""
		server.Status = storage.StatusStopped
		s.store.UpdateServer(ctx, server)
		return "container not found, marked as stopped", nil
	}

	// Wait a moment
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(2 * time.Second):
	}

	// Start container
	if err := s.docker.StartContainer(ctx, server.ContainerID); err != nil {
		return "", fmt.Errorf("failed to start: %w", err)
	}

	// Update server status
	server.Status = storage.StatusStarting
	now := time.Now()
	server.LastStarted = &now
	s.store.UpdateServer(ctx, server)

	return "server restarted successfully", nil
}

func (s *Scheduler) executeStartTask(ctx context.Context, server *storage.Server, task *storage.ScheduledTask) (string, error) {
	if server.ContainerID == "" {
		return "", fmt.Errorf("server has no container")
	}

	if err := s.docker.StartContainer(ctx, server.ContainerID); err != nil {
		return "", fmt.Errorf("failed to start: %w", err)
	}

	server.Status = storage.StatusStarting
	now := time.Now()
	server.LastStarted = &now
	s.store.UpdateServer(ctx, server)

	return "server started successfully", nil
}

func (s *Scheduler) executeStopTask(ctx context.Context, server *storage.Server, task *storage.ScheduledTask) (string, error) {
	if server.ContainerID == "" {
		return "", fmt.Errorf("server has no container")
	}

	found, err := s.docker.StopContainer(ctx, server.ContainerID)
	if err != nil {
		return "", fmt.Errorf("failed to stop: %w", err)
	}
	if !found {
		server.ContainerID = ""
		server.Status = storage.StatusStopped
		s.store.UpdateServer(ctx, server)
		return "container not found, marked as stopped", nil
	}

	server.Status = storage.StatusStopping
	s.store.UpdateServer(ctx, server)

	return "server stopped successfully", nil
}

// BackupTaskConfig represents configuration for backup tasks
type BackupTaskConfig struct {
	BackupName    string   `json:"backup_name"`
	Paths         []string `json:"paths"`
	Compress      bool     `json:"compress"`
	RetentionDays int      `json:"retention_days"`
}

func (s *Scheduler) executeBackupTask(ctx context.Context, server *storage.Server, task *storage.ScheduledTask) (string, error) {
	// Backup functionality will be implemented when the backup system is added
	// For now, return a placeholder indicating this is ready for backup implementation
	return "", fmt.Errorf("backup task type not yet implemented")
}

// ScriptTaskConfig represents configuration for script tasks
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
	return s.docker.Exec(ctx, server.ContainerID, append(execCmd, config.Args...))
}

// CalculateNextRun calculates the next run time for a task based on its schedule
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

	default:
		return nil, fmt.Errorf("unknown schedule type: %s", task.Schedule)
	}
}

// ValidateCronExpr validates a cron expression
func (s *Scheduler) ValidateCronExpr(expr string) error {
	_, err := s.cronParser.Parse(expr)
	return err
}

package module

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/nickheyer/discopanel/internal/alias"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

// EventDispatcher handles server events and triggers module hooks
type EventDispatcher struct {
	manager *Manager
	store   *storage.Store
	docker  *docker.Client
	logger  *logger.Logger
}

// NewEventDispatcher creates a new event dispatcher
func NewEventDispatcher(manager *Manager, store *storage.Store, docker *docker.Client, log *logger.Logger) *EventDispatcher {
	return &EventDispatcher{
		manager: manager,
		store:   store,
		docker:  docker,
		logger:  log,
	}
}

// OnServerEvent dispatches an event to all modules for a server
func (d *EventDispatcher) OnServerEvent(ctx context.Context, serverID string, eventType v1.ModuleEventType) {
	modules, err := d.store.ListServerModules(ctx, serverID)
	if err != nil {
		d.logger.Error("Failed to list modules for event dispatch: %v", err)
		return
	}

	for _, module := range modules {
		if len(module.EventHooks) == 0 {
			continue
		}

		for _, hook := range module.EventHooks {
			if hook == nil || hook.Event != eventType {
				continue
			}

			// Execute hook asynchronously
			go d.executeHook(context.Background(), module, hook, serverID)
		}
	}
}

// executeHook executes a single event hook
func (d *EventDispatcher) executeHook(ctx context.Context, module *storage.Module, hook *v1.ModuleEventHook, serverID string) {
	// Apply delay if configured
	if hook.DelaySeconds > 0 {
		d.logger.Debug("Delaying hook action for module %s by %d seconds", module.Name, hook.DelaySeconds)
		time.Sleep(time.Duration(hook.DelaySeconds) * time.Second)
	}

	// Evaluate condition if specified
	if hook.Condition != "" {
		server, err := d.store.GetServer(ctx, serverID)
		if err != nil {
			d.logger.Error("Failed to get server for condition evaluation: %v", err)
			return
		}
		if !d.evaluateCondition(hook.Condition, server, module) {
			d.logger.Debug("Condition not met for hook on module %s: %s", module.Name, hook.Condition)
			return
		}
	}

	d.logger.Info("Executing hook action %s for module %s (event: %s)",
		hook.Action.String(), module.Name, hook.Event.String())

	switch hook.Action {
	case v1.ModuleEventAction_MODULE_EVENT_ACTION_START:
		if err := d.startModule(ctx, module); err != nil {
			d.logger.Error("Hook failed to start module %s: %v", module.Name, err)
		}

	case v1.ModuleEventAction_MODULE_EVENT_ACTION_STOP:
		if err := d.manager.StopModule(ctx, module.ID); err != nil {
			d.logger.Error("Hook failed to stop module %s: %v", module.Name, err)
		}

	case v1.ModuleEventAction_MODULE_EVENT_ACTION_RESTART:
		if err := d.manager.RestartModule(ctx, module.ID); err != nil {
			d.logger.Error("Hook failed to restart module %s: %v", module.Name, err)
		}

	case v1.ModuleEventAction_MODULE_EVENT_ACTION_EXEC:
		if err := d.execInModule(ctx, module, hook.Command); err != nil {
			d.logger.Error("Hook failed to exec in module %s: %v", module.Name, err)
		}

	case v1.ModuleEventAction_MODULE_EVENT_ACTION_RCON:
		if err := d.sendRCON(ctx, serverID, hook.Command); err != nil {
			d.logger.Error("Hook failed to send RCON for module %s: %v", module.Name, err)
		}

	default:
		d.logger.Warn("Unknown hook action for module %s: %v", module.Name, hook.Action)
	}
}

// startModule starts a module, creating container if needed
func (d *EventDispatcher) startModule(ctx context.Context, module *storage.Module) error {
	if module.ContainerID == "" {
		return d.manager.CreateAndStartModule(ctx, module.ID, true)
	}
	return d.manager.StartModule(ctx, module.ID)
}

// execInModule executes a command inside a module container
func (d *EventDispatcher) execInModule(ctx context.Context, module *storage.Module, command string) error {
	if module.ContainerID == "" {
		return nil // Cannot exec in non-existent container
	}

	_, err := d.docker.Exec(ctx, module.ContainerID, []string{command})
	return err
}

// sendRCON sends an RCON command to the parent server via rcon-cli
func (d *EventDispatcher) sendRCON(ctx context.Context, serverID string, command string) error {
	server, err := d.store.GetServer(ctx, serverID)
	if err != nil {
		return err
	}

	if server.ContainerID == "" {
		d.logger.Warn("Cannot send RCON: server %s has no container", server.Name)
		return nil
	}

	// Use ExecCommand which runs rcon-cli inside the server container
	_, err = d.docker.ExecCommand(ctx, server.ContainerID, command)
	return err
}

// Helper methods for common event triggers

// OnServerStart triggers MODULE_EVENT_TYPE_SERVER_START for all modules
func (d *EventDispatcher) OnServerStart(ctx context.Context, serverID string) {
	d.OnServerEvent(ctx, serverID, v1.ModuleEventType_MODULE_EVENT_TYPE_SERVER_START)
}

// OnServerStop triggers MODULE_EVENT_TYPE_SERVER_STOP for all modules
func (d *EventDispatcher) OnServerStop(ctx context.Context, serverID string) {
	d.OnServerEvent(ctx, serverID, v1.ModuleEventType_MODULE_EVENT_TYPE_SERVER_STOP)
}

// OnServerHealthy triggers MODULE_EVENT_TYPE_SERVER_HEALTHY for all modules
func (d *EventDispatcher) OnServerHealthy(ctx context.Context, serverID string) {
	d.OnServerEvent(ctx, serverID, v1.ModuleEventType_MODULE_EVENT_TYPE_SERVER_HEALTHY)
}

// OnPlayerJoin triggers MODULE_EVENT_TYPE_PLAYER_JOIN for all modules
func (d *EventDispatcher) OnPlayerJoin(ctx context.Context, serverID string) {
	d.OnServerEvent(ctx, serverID, v1.ModuleEventType_MODULE_EVENT_TYPE_PLAYER_JOIN)
}

// OnPlayerLeave triggers MODULE_EVENT_TYPE_PLAYER_LEAVE for all modules
func (d *EventDispatcher) OnPlayerLeave(ctx context.Context, serverID string) {
	d.OnServerEvent(ctx, serverID, v1.ModuleEventType_MODULE_EVENT_TYPE_PLAYER_LEAVE)
}

// evaluateCondition evaluates a simple condition expression using the alias system
// Condition format: <alias> <operator> <value>
// Examples:
//   - "{{server.players_online}} == 0"
//   - "{{server.players_online}} > 5"
//   - "{{server.status}} == running"
//   - "{{module.status}} == stopped"
//
// The alias system dynamically resolves any field from Server or Module structs.
// See alias.GetAvailableAliases() for all available aliases.
func (d *EventDispatcher) evaluateCondition(condition string, server *storage.Server, module *storage.Module) bool {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return true
	}

	// Build alias context for resolution
	ctx := &alias.Context{
		Server: server,
		Module: module,
	}

	// Resolve all aliases in the condition string first
	resolved := alias.Substitute(condition, ctx)

	// Parse condition: <resolved_value> <operator> <expected_value>
	var actualValue, operator, expectedValue string

	// Try different operators in order of specificity
	operators := []string{"==", "!=", "<=", ">=", "<", ">"}
	for _, op := range operators {
		if parts := strings.SplitN(resolved, op, 2); len(parts) == 2 {
			actualValue = strings.TrimSpace(parts[0])
			operator = op
			expectedValue = strings.TrimSpace(parts[1])
			break
		}
	}

	if operator == "" {
		d.logger.Warn("Invalid condition format (no operator found): %s", condition)
		return false
	}

	// Compare values
	return d.compareValues(actualValue, operator, expectedValue)
}

// compareValues compares two values using the specified operator
func (d *EventDispatcher) compareValues(actual, operator, expected string) bool {
	// Try numeric comparison first
	actualNum, actualErr := strconv.ParseFloat(actual, 64)
	expectedNum, expectedErr := strconv.ParseFloat(expected, 64)

	if actualErr == nil && expectedErr == nil {
		// Both are numeric
		switch operator {
		case "==":
			return actualNum == expectedNum
		case "!=":
			return actualNum != expectedNum
		case "<":
			return actualNum < expectedNum
		case ">":
			return actualNum > expectedNum
		case "<=":
			return actualNum <= expectedNum
		case ">=":
			return actualNum >= expectedNum
		}
	}

	// String comparison
	actual = strings.ToLower(actual)
	expected = strings.ToLower(expected)
	switch operator {
	case "==":
		return actual == expected
	case "!=":
		return actual != expected
	default:
		d.logger.Warn("Operator %s not supported for string comparison", operator)
		return false
	}
}

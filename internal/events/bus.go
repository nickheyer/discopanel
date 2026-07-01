package events

import (
	"context"
	"sync"

	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

// A single server lifecycle event
type Event struct {
	Type     v1.TriggeredEventType
	ServerID string
	Data     map[string]any
}

// Handler reacts to an emitted event
// NOTE: Handlers are invoked synchronously and must return promptly!
type Handler func(ctx context.Context, event Event)

// A generic fan-out event dispatcher
type Bus struct {
	log      *logger.Logger
	mu       sync.RWMutex
	handlers []Handler
}

// Creates an empty event bus
func NewBus(log *logger.Logger) *Bus {
	return &Bus{log: log}
}

// Registers a handler to receive every emitted event
func (b *Bus) Subscribe(h Handler) {
	if h == nil {
		return
	}
	b.mu.Lock()
	b.handlers = append(b.handlers, h)
	b.mu.Unlock()
}

// Delivers an event to all registered handlers in registration order
func (b *Bus) Emit(ctx context.Context, event Event) {
	b.mu.RLock()
	handlers := make([]Handler, len(b.handlers))
	copy(handlers, b.handlers)
	b.mu.RUnlock()

	if b.log != nil {
		b.log.Debug("event bus: %s for server %s (%d handlers)", event.Type, event.ServerID, len(handlers))
	}
	for _, h := range handlers {
		h(ctx, event)
	}
}

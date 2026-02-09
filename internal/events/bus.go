package events

import (
	"sync"
)

// EventType identifies different event categories
type EventType string

const (
	EventServerUpdate  EventType = "server.update"
	EventServerMetrics EventType = "server.metrics"
	EventServerDeleted EventType = "server.deleted"
	EventModuleUpdate  EventType = "module.update"
)

// Event represents a state change notification
type Event struct {
	Type     EventType
	ServerID string
	ModuleID string
}

// Bus is an in-process event bus with fan-out to subscribers
type Bus struct {
	mu          sync.Mutex
	subscribers map[uint64]chan Event
	nextID      uint64
}

// NewBus creates a new event bus
func NewBus() *Bus {
	return &Bus{
		subscribers: make(map[uint64]chan Event),
	}
}

// Subscribe returns a channel that receives events and a function to unsubscribe
func (b *Bus) Subscribe(bufSize int) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID
	b.nextID++
	ch := make(chan Event, bufSize)
	b.subscribers[id] = ch

	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subscribers[id]; ok {
			delete(b.subscribers, id)
			close(ch)
		}
	}
}

// Publish sends an event to all subscribers (non-blocking)
func (b *Bus) Publish(evt Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, ch := range b.subscribers {
		select {
		case ch <- evt:
		default:
			// slow subscriber, drop event
		}
	}
}

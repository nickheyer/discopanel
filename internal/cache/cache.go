package cache

import (
	"sync"
	"time"
)

// TTLCache is a generic time-based cache with configurable TTL per key
type TTLCache[K comparable, V any] struct {
	mu    sync.RWMutex
	items map[K]*CacheItem[V]
}

// CacheItem holds a cached value with expiration
type CacheItem[V any] struct {
	Value     V
	ExpiresAt time.Time
}

// NewTTLCache creates a new TTL cache
func NewTTLCache[K comparable, V any]() *TTLCache[K, V] {
	return &TTLCache[K, V]{
		items: make(map[K]*CacheItem[V]),
	}
}

// Get retrieves a value from the cache
func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	var zero V
	item, exists := c.items[key]
	if !exists {
		return zero, false
	}
	
	// Check if expired
	if time.Now().After(item.ExpiresAt) {
		return zero, false
	}
	
	return item.Value, true
}

// Set stores a value in the cache with a TTL
func (c *TTLCache[K, V]) Set(key K, value V, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.items[key] = &CacheItem[V]{
		Value:     value,
		ExpiresAt: time.Now().Add(ttl),
	}
}

// Delete removes a value from the cache
func (c *TTLCache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	delete(c.items, key)
}

// Clear removes all items from the cache
func (c *TTLCache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.items = make(map[K]*CacheItem[V])
}

// CleanExpired removes expired items from the cache
func (c *TTLCache[K, V]) CleanExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	now := time.Now()
	for key, item := range c.items {
		if now.After(item.ExpiresAt) {
			delete(c.items, key)
		}
	}
}
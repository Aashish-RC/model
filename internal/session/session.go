package session

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Binding represents an affinity binding between a session and a specific target/key.
type Binding struct {
	SessionID string
	OrgID     string
	TargetID  string
	KeyID     string
	Model     string
	BoundAt   time.Time
	ExpiresAt time.Time
}

// IsExpired checks whether the affinity binding has passed its TTL.
func (b *Binding) IsExpired(now time.Time) bool {
	return now.After(b.ExpiresAt)
}

// Store abstracts session affinity storage (in-memory or distributed KV).
type Store interface {
	Get(ctx context.Context, key string) (*Binding, error)
	Set(ctx context.Context, key string, binding *Binding, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// MemoryStore implements an in-memory thread-safe session affinity store.
type MemoryStore struct {
	mu       sync.RWMutex
	bindings map[string]*Binding
}

// NewMemoryStore initializes a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		bindings: make(map[string]*Binding),
	}
}

func (s *MemoryStore) Get(ctx context.Context, key string) (*Binding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	b, exists := s.bindings[key]
	if !exists {
		return nil, nil
	}
	if b.IsExpired(time.Now()) {
		return nil, nil
	}
	return b, nil
}

func (s *MemoryStore) Set(ctx context.Context, key string, binding *Binding, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bindings[key] = binding
	return nil
}

func (s *MemoryStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.bindings, key)
	return nil
}

// EvictExpired removes expired bindings to reclaim memory.
func (s *MemoryStore) EvictExpired(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for k, b := range s.bindings {
		if b.IsExpired(now) {
			delete(s.bindings, k)
			count++
		}
	}
	return count
}

// Manager coordinates session affinity resolution, bindings, and eviction.
type Manager struct {
	store      Store
	DefaultTTL time.Duration
}

// NewManager creates a new session affinity manager.
func NewManager(store Store, defaultTTL time.Duration) *Manager {
	if defaultTTL <= 0 {
		defaultTTL = 30 * time.Minute
	}
	if store == nil {
		store = NewMemoryStore()
	}
	return &Manager{
		store:      store,
		DefaultTTL: defaultTTL,
	}
}

func (m *Manager) makeKey(orgID, sessionID string) string {
	return fmt.Sprintf("session:%s:%s", orgID, sessionID)
}

// Resolve looks up any active binding for the session.
func (m *Manager) Resolve(ctx context.Context, orgID, sessionID string) (*Binding, bool) {
	if sessionID == "" {
		return nil, false
	}
	key := m.makeKey(orgID, sessionID)
	binding, err := m.store.Get(ctx, key)
	if err != nil || binding == nil {
		return nil, false
	}
	return binding, true
}

// Bind establishes or refreshes affinity for a session.
func (m *Manager) Bind(ctx context.Context, orgID, sessionID, targetID, keyID, model string, ttl time.Duration) error {
	if sessionID == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = m.DefaultTTL
	}
	now := time.Now()
	b := &Binding{
		SessionID: sessionID,
		OrgID:     orgID,
		TargetID:  targetID,
		KeyID:     keyID,
		Model:     model,
		BoundAt:   now,
		ExpiresAt: now.Add(ttl),
	}
	key := m.makeKey(orgID, sessionID)
	return m.store.Set(ctx, key, b, ttl)
}

// Unbind removes an affinity binding (e.g. after a failure or client reset).
func (m *Manager) Unbind(ctx context.Context, orgID, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	key := m.makeKey(orgID, sessionID)
	return m.store.Delete(ctx, key)
}

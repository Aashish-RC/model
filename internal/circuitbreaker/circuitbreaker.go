package circuitbreaker

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrCircuitOpen = errors.New("circuit breaker is open; upstream provider is unavailable")
)

// State represents the circuit state.
type State string

const (
	StateClosed   State = "closed"
	StateOpen     State = "open"
	StateHalfOpen State = "half-open"
)

// Config configures circuit breaker thresholds.
type Config struct {
	FailureThreshold int           // Consecutive failures to trip circuit (default: 5)
	SuccessThreshold int           // Consecutive successes in half-open to close (default: 2)
	CooloffDuration  time.Duration // Time to stay in open state before half-open (default: 30s)
	MaxCooloff       time.Duration // Maximum cooloff duration under exponential growth
}

// DefaultConfig provides recommended settings.
func DefaultConfig() Config {
	return Config{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		CooloffDuration:  30 * time.Second,
		MaxCooloff:       5 * time.Minute,
	}
}

// Breaker implements a tri-state circuit breaker.
type Breaker struct {
	name   string
	cfg    Config
	mu     sync.RWMutex
	state  State
	fails  int
	succs  int
	opened time.Time
	cool   time.Duration
}

// NewBreaker creates a circuit breaker.
func NewBreaker(name string, cfg Config) *Breaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 2
	}
	if cfg.CooloffDuration <= 0 {
		cfg.CooloffDuration = 30 * time.Second
	}
	if cfg.MaxCooloff <= 0 {
		cfg.MaxCooloff = 5 * time.Minute
	}
	return &Breaker{
		name:  name,
		cfg:   cfg,
		state: StateClosed,
		cool:  cfg.CooloffDuration,
	}
}

// State returns current state, evaluating cooloff transitions if necessary.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.checkTransition(time.Now())
	return b.state
}

func (b *Breaker) checkTransition(now time.Time) {
	if b.state == StateOpen {
		if now.Sub(b.opened) >= b.cool {
			b.state = StateHalfOpen
			b.succs = 0
		}
	}
}

// Allow reports whether a request is permitted to proceed.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	b.checkTransition(now)

	switch b.state {
	case StateClosed:
		return true
	case StateHalfOpen:
		// Allow a single canary request at a time
		return true
	case StateOpen:
		return false
	default:
		return true
	}
}

// RecordSuccess records a successful upstream call.
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.fails = 0
	if b.state == StateHalfOpen {
		b.succs++
		if b.succs >= b.cfg.SuccessThreshold {
			b.state = StateClosed
			b.succs = 0
			b.cool = b.cfg.CooloffDuration // reset cooloff
		}
	}
}

// RecordFailure records an upstream failure (e.g. 5xx, timeout, connect refuse).
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	b.fails++

	if b.state == StateClosed && b.fails >= b.cfg.FailureThreshold {
		b.trip(now)
	} else if b.state == StateHalfOpen {
		// Immediate trip on canary failure, doubling cooloff
		b.cool *= 2
		if b.cool > b.cfg.MaxCooloff {
			b.cool = b.cfg.MaxCooloff
		}
		b.trip(now)
	}
}

func (b *Breaker) trip(now time.Time) {
	b.state = StateOpen
	b.opened = now
	b.succs = 0
}

// Reset resets the breaker to Closed state.
func (b *Breaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.state = StateClosed
	b.fails = 0
	b.succs = 0
	b.cool = b.cfg.CooloffDuration
}

// Registry manages circuit breakers per provider name.
type Registry struct {
	mu       sync.RWMutex
	breakers map[string]*Breaker
	cfg      Config
}

// NewRegistry creates a new circuit breaker registry.
func NewRegistry(cfg Config) *Registry {
	return &Registry{
		breakers: make(map[string]*Breaker),
		cfg:      cfg,
	}
}

// Get returns or creates a circuit breaker for the given provider.
func (r *Registry) Get(provider string) *Breaker {
	r.mu.RLock()
	b, exists := r.breakers[provider]
	r.mu.RUnlock()
	if exists {
		return b
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if b, exists = r.breakers[provider]; exists {
		return b
	}

	b = NewBreaker(provider, r.cfg)
	r.breakers[provider] = b
	return b
}

// Probe executes an active health check on a provider breaker.
func (r *Registry) Probe(ctx context.Context, provider string, checkFn func(ctx context.Context) error) error {
	b := r.Get(provider)
	err := checkFn(ctx)
	if err != nil {
		b.RecordFailure()
		return err
	}
	b.RecordSuccess()
	return nil
}

package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ra1/model-gateway/internal/cache"
	"github.com/ra1/model-gateway/internal/circuitbreaker"
	"github.com/ra1/model-gateway/internal/rules"
)

var (
	ErrInvalidConfig = errors.New("runtime configuration validation failed")
)

// RuntimeConfig represents the hot-swappable gateway configuration snapshot.
type RuntimeConfig struct {
	DefaultRoutingStrategy string                  `json:"default_routing_strategy"`
	FallbackChains         map[string][]string     `json:"fallback_chains"`
	CELRules               []*rules.Rule           `json:"cel_rules"`
	Cache                  cache.Config            `json:"cache"`
	CircuitBreaker         circuitbreaker.Config   `json:"circuit_breaker"`
	Revision               string                  `json:"revision"`
	UpdatedAt              time.Time               `json:"updated_at"`
}

// DefaultRuntimeConfig provides baseline configuration defaults.
func DefaultRuntimeConfig() *RuntimeConfig {
	cfg := &RuntimeConfig{
		DefaultRoutingStrategy: "lowest_latency",
		FallbackChains: map[string][]string{
			"gpt-4o": {"claude-3-5-sonnet-20241022", "gemini-1.5-pro"},
		},
		CELRules:       make([]*rules.Rule, 0),
		Cache:          cache.DefaultConfig(),
		CircuitBreaker: circuitbreaker.DefaultConfig(),
		UpdatedAt:      time.Now(),
	}
	cfg.ComputeRevision()
	return cfg
}

// ComputeRevision generates a deterministic SHA-256 fingerprint of the configuration content.
func (c *RuntimeConfig) ComputeRevision() {
	data, _ := json.Marshal(struct {
		Strat     string                `json:"strat"`
		Fallbacks map[string][]string   `json:"fallbacks"`
		Rules     []*rules.Rule         `json:"rules"`
		Cache     cache.Config          `json:"cache"`
		CB        circuitbreaker.Config `json:"cb"`
	}{
		Strat:     c.DefaultRoutingStrategy,
		Fallbacks: c.FallbackChains,
		Rules:     c.CELRules,
		Cache:     c.Cache,
		CB:        c.CircuitBreaker,
	})

	sum := sha256.Sum256(data)
	c.Revision = hex.EncodeToString(sum[:8])
}

// Manager manages atomic snapshot swapping of RuntimeConfig.
type Manager struct {
	current atomic.Pointer[RuntimeConfig]
}

// NewManager initializes the config manager with an initial valid configuration.
func NewManager(initial *RuntimeConfig) (*Manager, error) {
	if initial == nil {
		initial = DefaultRuntimeConfig()
	}

	if err := Validate(initial); err != nil {
		return nil, fmt.Errorf("initial config invalid: %w", err)
	}

	m := &Manager{}
	m.current.Store(initial)
	return m, nil
}

// Get returns the currently active configuration snapshot.
func (m *Manager) Get() *RuntimeConfig {
	return m.current.Load()
}

// Update attempts to validate and atomically swap the active configuration snapshot.
func (m *Manager) Update(newCfg *RuntimeConfig) error {
	if newCfg == nil {
		return errors.New("cannot update with nil configuration")
	}

	// Two-Phase Validation: test compile rules & check constraints
	if err := Validate(newCfg); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}

	newCfg.UpdatedAt = time.Now()
	newCfg.ComputeRevision()

	// Atomic pointer swap
	m.current.Store(newCfg)
	return nil
}

// Validate validates configuration integrity and pre-compiles CEL rules to catch errors.
func Validate(cfg *RuntimeConfig) error {
	if cfg.DefaultRoutingStrategy == "" {
		return errors.New("default_routing_strategy must not be empty")
	}

	// Validate CEL rules by compiling them
	if len(cfg.CELRules) > 0 {
		_, err := rules.NewEngine(cfg.CELRules)
		if err != nil {
			return fmt.Errorf("invalid CEL rules in configuration: %w", err)
		}
	}

	return nil
}

package keypool

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ra1/model-gateway/internal/provider"
)

var (
	ErrNoKeysAvailable      = errors.New("no upstream keys available in pool")
	ErrAllKeysInCooldown    = errors.New("all upstream keys in pool are currently cooling down")
	ErrEnterprisePoolNotFound = errors.New("no dedicated enterprise pool found for organization")
)

type PoolType string

const (
	PoolShared     PoolType = "shared"
	PoolEnterprise PoolType = "enterprise_dedicated"
)

type KeyState int

const (
	KeyStateActive KeyState = iota
	KeyStateCoolingDown
	KeyStateDisabled
)

// Key represents a managed upstream provider API credential.
type Key struct {
	ID               string
	Provider         string
	APIKey           string
	BaseURL          string
	PoolType         PoolType
	EnterpriseOrgID  string
	LimitRPM         int
	LimitTPM         int
	CurrentRPM       atomic.Int64
	CurrentTPM       atomic.Int64
	Headers          map[string]string
	State            KeyState
	ConsecutiveFails atomic.Int32
	CooldownUntil    atomic.Int64 // Unix nanoseconds
	LastSuccess      time.Time
	mu               sync.RWMutex
}

// ToCredential converts a key into a provider.Credential.
func (k *Key) ToCredential() *provider.Credential {
	return &provider.Credential{
		ID:           k.ID,
		ProviderName: k.Provider,
		APIKey:       k.APIKey,
		BaseURL:      k.BaseURL,
		Headers:      k.Headers,
	}
}

// ComputeHeadroom calculates the available capacity ratio [0.0, 1.0].
func (k *Key) ComputeHeadroom() float64 {
	hRPM := 1.0
	if k.LimitRPM > 0 {
		current := float64(k.CurrentRPM.Load())
		hRPM = 1.0 - (current / float64(k.LimitRPM))
		if hRPM < 0.0 {
			hRPM = 0.0
		}
	}

	hTPM := 1.0
	if k.LimitTPM > 0 {
		current := float64(k.CurrentTPM.Load())
		hTPM = 1.0 - (current / float64(k.LimitTPM))
		if hTPM < 0.0 {
			hTPM = 0.0
		}
	}

	return math.Min(hRPM, hTPM)
}

// IsEligible checks whether the key is active or has finished its cooldown window.
func (k *Key) IsEligible(now time.Time) bool {
	k.mu.RLock()
	state := k.State
	k.mu.RUnlock()

	if state == KeyStateDisabled {
		return false
	}

	cooldownUntil := k.CooldownUntil.Load()
	if cooldownUntil > 0 {
		if now.UnixNano() >= cooldownUntil {
			// Auto-rejoin: cooldown has elapsed
			return true
		}
		return false
	}

	return state == KeyStateActive
}

// MarkSuccess resets fail counts and re-admits key to active state.
func (k *Key) MarkSuccess() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.ConsecutiveFails.Store(0)
	k.CooldownUntil.Store(0)
	k.State = KeyStateActive
	k.LastSuccess = time.Now()
}

// MarkFailure triggers exponential growing cooldown.
func (k *Key) MarkFailure(baseCooldown, maxCooldown time.Duration) time.Duration {
	k.mu.Lock()
	defer k.mu.Unlock()

	fails := k.ConsecutiveFails.Add(1)
	multiplier := math.Pow(2, float64(fails-1))
	cooldown := time.Duration(float64(baseCooldown) * multiplier)
	if cooldown > maxCooldown {
		cooldown = maxCooldown
	}

	now := time.Now()
	k.CooldownUntil.Store(now.Add(cooldown).UnixNano())
	k.State = KeyStateCoolingDown
	return cooldown
}

// Manager orchestrates key pools, selection, and cooldown tracking.
type Manager struct {
	mu            sync.RWMutex
	keys          map[string]*Key // Key ID -> Key
	BaseCooldown  time.Duration
	MaxCooldown   time.Duration
}

// NewManager creates a key pool manager.
func NewManager() *Manager {
	return &Manager{
		keys:         make(map[string]*Key),
		BaseCooldown: 10 * time.Second,
		MaxCooldown:  30 * time.Minute,
	}
}

// AddKey registers an upstream key.
func (m *Manager) AddKey(key *Key) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys[key.ID] = key
}

// SelectKey picks the key with highest headroom matching provider and enterprise org boundaries.
func (m *Manager) SelectKey(providerName string, orgID string, isEnterprise bool) (*Key, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	var candidates []*Key
	anyMatchingKeys := false
	allInCooldown := true

	for _, k := range m.keys {
		if k.Provider != providerName {
			continue
		}

		// Enterprise Pool Separation Rule
		if isEnterprise {
			if k.PoolType != PoolEnterprise || k.EnterpriseOrgID != orgID {
				continue
			}
		} else {
			if k.PoolType != PoolShared {
				continue
			}
		}

		anyMatchingKeys = true
		if k.IsEligible(now) {
			candidates = append(candidates, k)
			allInCooldown = false
		}
	}

	if !anyMatchingKeys {
		if isEnterprise {
			return nil, ErrEnterprisePoolNotFound
		}
		return nil, ErrNoKeysAvailable
	}

	if len(candidates) == 0 {
		if allInCooldown {
			return nil, ErrAllKeysInCooldown
		}
		return nil, ErrNoKeysAvailable
	}

	// Pick candidate with highest headroom
	var bestKey *Key
	var highestHeadroom = -1.0

	for _, k := range candidates {
		headroom := k.ComputeHeadroom()
		if headroom > highestHeadroom {
			highestHeadroom = headroom
			bestKey = k
		}
	}

	return bestKey, nil
}

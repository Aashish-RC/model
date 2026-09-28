package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrKeyNotFound        = errors.New("virtual key not found")
	ErrKeyInactive        = errors.New("virtual key is inactive or revoked")
	ErrKeyExpired         = errors.New("virtual key has expired")
	ErrModelNotAllowed    = errors.New("requested model is not in virtual key allowlist")
	ErrBudgetExceeded     = errors.New("virtual key budget exceeded")
	ErrRateLimitExceeded  = errors.New("virtual key rate limit exceeded")
)

// VirtualKey represents the key metadata and limits.
type VirtualKey struct {
	ID              string
	KeyHash         string
	KeyPrefix       string
	Name            string
	OrgID           string
	TeamID          string
	UserID          string
	ModelsAllowlist []string
	MaxBudgetMicro  int64
	SpendMicro      int64
	RPMLimit        int
	TPMLimit        int
	IsActive        bool
	ExpiresAt       *time.Time
	CreatedAt       time.Time
}

// HashKey computes SHA-256 hex string of the raw virtual key.
func HashKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

// GenerateRawKey produces a new cryptographically secure virtual key string: sk-gw-<prefix>-<hex>
func GenerateRawKey(prefix string) (rawKey string, keyHash string, keyPrefix string, err error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	tokenHex := hex.EncodeToString(b)
	if prefix == "" {
		prefix = "live"
	}

	keyPrefix = prefix
	rawKey = fmt.Sprintf("sk-gw-%s-%s", prefix, tokenHex)
	keyHash = HashKey(rawKey)
	return rawKey, keyHash, keyPrefix, nil
}

// MemoryKeyStore provides an in-memory, thread-safe implementation of key management.
type MemoryKeyStore struct {
	mu   sync.RWMutex
	keys map[string]*VirtualKey // KeyHash -> VirtualKey
	byID map[string]*VirtualKey // ID -> VirtualKey
}

func NewMemoryKeyStore() *MemoryKeyStore {
	return &MemoryKeyStore{
		keys: make(map[string]*VirtualKey),
		byID: make(map[string]*VirtualKey),
	}
}

// SaveKey stores a virtual key.
func (s *MemoryKeyStore) SaveKey(key *VirtualKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[key.KeyHash] = key
	s.byID[key.ID] = key
	return nil
}

// Authenticate verifies the raw bearer key, checks expiration, status, budget, and model allowlist.
func (s *MemoryKeyStore) Authenticate(rawKey string, requestedModel string) (*VirtualKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hash := HashKey(rawKey)
	k, exists := s.keys[hash]
	if !exists {
		return nil, ErrKeyNotFound
	}

	if !k.IsActive {
		return nil, ErrKeyInactive
	}

	now := time.Now()
	if k.ExpiresAt != nil && now.After(*k.ExpiresAt) {
		return nil, ErrKeyExpired
	}

	if k.MaxBudgetMicro > 0 && k.SpendMicro >= k.MaxBudgetMicro {
		return nil, ErrBudgetExceeded
	}

	// Model Allowlist check
	if len(k.ModelsAllowlist) > 0 {
		allowed := false
		for _, m := range k.ModelsAllowlist {
			if m == requestedModel || m == "*" {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, ErrModelNotAllowed
		}
	}

	return k, nil
}

// RevokeKey marks the key inactive by ID.
func (s *MemoryKeyStore) RevokeKey(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	k, exists := s.byID[id]
	if !exists {
		return ErrKeyNotFound
	}
	k.IsActive = false
	return nil
}

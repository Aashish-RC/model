package store

import (
	"testing"
	"time"
)

func TestVirtualKeys_Lifecycle(t *testing.T) {
	store := NewMemoryKeyStore()

	rawKey, keyHash, prefix, err := GenerateRawKey("prod")
	if err != nil {
		t.Fatalf("generation error: %v", err)
	}

	if prefix != "prod" {
		t.Errorf("expected prefix prod, got %s", prefix)
	}

	keyObj := &VirtualKey{
		ID:              "vk-123",
		KeyHash:         keyHash,
		KeyPrefix:       prefix,
		Name:            "Production Agent Key",
		OrgID:           "org-alpha",
		TeamID:          "team-core",
		ModelsAllowlist: []string{"gpt-4o", "claude-3-5-sonnet"},
		IsActive:        true,
	}

	if err := store.SaveKey(keyObj); err != nil {
		t.Fatalf("save error: %v", err)
	}

	// 1. Authenticate with allowed model
	authed, err := store.Authenticate(rawKey, "gpt-4o")
	if err != nil {
		t.Fatalf("expected successful auth, got %v", err)
	}
	if authed.OrgID != "org-alpha" {
		t.Errorf("expected org-alpha, got %s", authed.OrgID)
	}

	// 2. Reject unallowed model
	_, err = store.Authenticate(rawKey, "unauthorized-model")
	if err != ErrModelNotAllowed {
		t.Errorf("expected ErrModelNotAllowed, got %v", err)
	}

	// 3. Revoke key
	if err := store.RevokeKey("vk-123"); err != nil {
		t.Fatalf("revoke error: %v", err)
	}

	// 4. Authenticate revoked key
	_, err = store.Authenticate(rawKey, "gpt-4o")
	if err != ErrKeyInactive {
		t.Errorf("expected ErrKeyInactive, got %v", err)
	}
}

func TestVirtualKeys_Expiration(t *testing.T) {
	store := NewMemoryKeyStore()
	rawKey, keyHash, _, _ := GenerateRawKey("test")

	past := time.Now().Add(-1 * time.Hour)
	keyObj := &VirtualKey{
		ID:        "vk-expired",
		KeyHash:   keyHash,
		IsActive:  true,
		ExpiresAt: &past,
	}
	_ = store.SaveKey(keyObj)

	_, err := store.Authenticate(rawKey, "gpt-4o")
	if err != ErrKeyExpired {
		t.Errorf("expected ErrKeyExpired, got %v", err)
	}
}

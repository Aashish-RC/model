package keypool

import (
	"testing"
	"time"
)

func TestKey_HeadroomCalculation(t *testing.T) {
	key := &Key{
		ID:       "key-1",
		Provider: "openai",
		LimitRPM: 100,
		LimitTPM: 1000,
	}
	key.CurrentRPM.Store(20)  // 80% left
	key.CurrentTPM.Store(500) // 50% left

	headroom := key.ComputeHeadroom()
	if headroom != 0.5 {
		t.Errorf("expected headroom 0.5 (min of 0.8 and 0.5), got %f", headroom)
	}
}

func TestKey_GrowingCooldownAndAutoRejoin(t *testing.T) {
	key := &Key{
		ID:       "key-fail",
		Provider: "openai",
	}

	baseCooldown := 100 * time.Millisecond
	maxCooldown := 1 * time.Second

	// First failure: 100ms
	d1 := key.MarkFailure(baseCooldown, maxCooldown)
	if d1 != 100*time.Millisecond {
		t.Errorf("expected 100ms cooldown, got %v", d1)
	}
	if key.IsEligible(time.Now()) {
		t.Errorf("expected key to not be eligible immediately after failure")
	}

	// Second failure: 200ms
	d2 := key.MarkFailure(baseCooldown, maxCooldown)
	if d2 != 200*time.Millisecond {
		t.Errorf("expected 200ms cooldown, got %v", d2)
	}

	// Wait past cooldown
	future := time.Now().Add(250 * time.Millisecond)
	if !key.IsEligible(future) {
		t.Errorf("expected key to auto-rejoin after cooldown elapsed")
	}

	// Mark success
	key.MarkSuccess()
	if key.ConsecutiveFails.Load() != 0 {
		t.Errorf("expected consecutive fails reset to 0, got %d", key.ConsecutiveFails.Load())
	}
}

func TestManager_KeySelectionAndEnterpriseSeparation(t *testing.T) {
	mgr := NewManager()

	sharedKey1 := &Key{
		ID:       "shared-low",
		Provider: "openai",
		PoolType: PoolShared,
		LimitRPM: 100,
	}
	sharedKey1.CurrentRPM.Store(80) // 20% headroom

	sharedKey2 := &Key{
		ID:       "shared-high",
		Provider: "openai",
		PoolType: PoolShared,
		LimitRPM: 100,
	}
	sharedKey2.CurrentRPM.Store(10) // 90% headroom

	entKey := &Key{
		ID:              "enterprise-key-1",
		Provider:        "openai",
		PoolType:        PoolEnterprise,
		EnterpriseOrgID: "org-ent",
		LimitRPM:        100,
	}

	mgr.AddKey(sharedKey1)
	mgr.AddKey(sharedKey2)
	mgr.AddKey(entKey)

	// Test 1: Shared selection picks sharedKey2 (higher headroom)
	k, err := mgr.SelectKey("openai", "any-org", false)
	if err != nil {
		t.Fatalf("expected key, got error: %v", err)
	}
	if k.ID != "shared-high" {
		t.Errorf("expected shared-high with 90%% headroom, got %s", k.ID)
	}

	// Test 2: Enterprise org gets its dedicated key
	entK, err := mgr.SelectKey("openai", "org-ent", true)
	if err != nil {
		t.Fatalf("expected enterprise key, got error: %v", err)
	}
	if entK.ID != "enterprise-key-1" {
		t.Errorf("expected enterprise-key-1, got %s", entK.ID)
	}

	// Test 3: Unregistered enterprise org gets error, never touches shared pool
	_, err = mgr.SelectKey("openai", "org-unknown", true)
	if err != ErrEnterprisePoolNotFound {
		t.Errorf("expected ErrEnterprisePoolNotFound, got %v", err)
	}
}

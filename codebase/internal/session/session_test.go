package session

import (
	"context"
	"testing"
	"time"
)

func TestSessionAffinity_SequentialCallsRouteToSameKey(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(NewMemoryStore(), 10*time.Minute)

	orgID := "org-finance"
	sessionID := "sess-conv-12345"

	// First call: no binding
	b, found := mgr.Resolve(ctx, orgID, sessionID)
	if found || b != nil {
		t.Fatalf("expected no initial binding, got %+v", b)
	}

	// Bind to target-1, key-alpha
	err := mgr.Bind(ctx, orgID, sessionID, "target-1", "key-alpha", "gpt-4o", 10*time.Minute)
	if err != nil {
		t.Fatalf("bind error: %v", err)
	}

	// Second call: should resolve to target-1, key-alpha
	b, found = mgr.Resolve(ctx, orgID, sessionID)
	if !found || b == nil {
		t.Fatalf("expected binding to be found")
	}
	if b.TargetID != "target-1" || b.KeyID != "key-alpha" {
		t.Errorf("expected target-1 / key-alpha, got %s / %s", b.TargetID, b.KeyID)
	}
}

func TestSessionAffinity_TenantIsolationPreventsCrossTalk(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(NewMemoryStore(), 10*time.Minute)

	sessionID := "shared-session-name"

	// Org A binds to target-A
	_ = mgr.Bind(ctx, "org-A", sessionID, "target-A", "key-A", "gpt-4o", 10*time.Minute)
	// Org B binds to target-B
	_ = mgr.Bind(ctx, "org-B", sessionID, "target-B", "key-B", "gpt-4o", 10*time.Minute)

	// Resolve for Org A
	bA, foundA := mgr.Resolve(ctx, "org-A", sessionID)
	if !foundA || bA.TargetID != "target-A" {
		t.Errorf("expected target-A for org-A, got %+v", bA)
	}

	// Resolve for Org B
	bB, foundB := mgr.Resolve(ctx, "org-B", sessionID)
	if !foundB || bB.TargetID != "target-B" {
		t.Errorf("expected target-B for org-B, got %+v", bB)
	}
}

func TestSessionAffinity_TTLExpiration(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(NewMemoryStore(), 20*time.Millisecond)

	orgID := "org-1"
	sessionID := "sess-expiring"

	_ = mgr.Bind(ctx, orgID, sessionID, "target-1", "key-1", "gpt-4o", 20*time.Millisecond)

	b, found := mgr.Resolve(ctx, orgID, sessionID)
	if !found || b == nil {
		t.Fatalf("expected active binding before expiration")
	}

	// Wait for TTL expiry
	time.Sleep(30 * time.Millisecond)

	b, found = mgr.Resolve(ctx, orgID, sessionID)
	if found || b != nil {
		t.Fatalf("expected binding to have expired, got %+v", b)
	}
}

func TestSessionAffinity_FailureImmediatelyUnbinds(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(NewMemoryStore(), 10*time.Minute)

	orgID := "org-1"
	sessionID := "sess-fail"

	_ = mgr.Bind(ctx, orgID, sessionID, "target-faulty", "key-bad", "gpt-4o", 10*time.Minute)

	b, found := mgr.Resolve(ctx, orgID, sessionID)
	if !found || b.TargetID != "target-faulty" {
		t.Fatalf("expected active binding")
	}

	// Upstream failure triggers unbind
	_ = mgr.Unbind(ctx, orgID, sessionID)

	b, found = mgr.Resolve(ctx, orgID, sessionID)
	if found || b != nil {
		t.Fatalf("expected binding to be cleared after unbind")
	}
}

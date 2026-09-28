package circuitbreaker

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCircuitBreaker_TripsAfterThresholdFailures(t *testing.T) {
	cfg := Config{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		CooloffDuration:  50 * time.Millisecond,
		MaxCooloff:       500 * time.Millisecond,
	}
	b := NewBreaker("test-provider", cfg)

	if b.State() != StateClosed {
		t.Fatalf("expected initial state closed, got %s", b.State())
	}
	if !b.Allow() {
		t.Fatalf("expected allow() to be true in closed state")
	}

	// 2 failures: should stay closed
	b.RecordFailure()
	b.RecordFailure()
	if b.State() != StateClosed {
		t.Fatalf("expected state closed after 2 failures, got %s", b.State())
	}

	// 3rd failure: trips to open
	b.RecordFailure()
	if b.State() != StateOpen {
		t.Fatalf("expected state open after 3 failures, got %s", b.State())
	}
	if b.Allow() {
		t.Fatalf("expected allow() to be false when circuit is open")
	}
}

func TestCircuitBreaker_TransitionsToHalfOpenAndRecovers(t *testing.T) {
	cfg := Config{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		CooloffDuration:  30 * time.Millisecond,
		MaxCooloff:       200 * time.Millisecond,
	}
	b := NewBreaker("test-provider", cfg)

	// Trip circuit
	b.RecordFailure()
	b.RecordFailure()
	if b.State() != StateOpen {
		t.Fatalf("expected state open")
	}

	// Wait for cooloff
	time.Sleep(40 * time.Millisecond)

	// Next state check or allow() should transition to Half-Open
	if !b.Allow() {
		t.Fatalf("expected allow() to be true in half-open state for canary")
	}
	if b.State() != StateHalfOpen {
		t.Fatalf("expected state half-open, got %s", b.State())
	}

	// Canary success 1
	b.RecordSuccess()
	if b.State() != StateHalfOpen {
		t.Fatalf("expected still half-open after 1 success")
	}

	// Canary success 2 -> recovers to Closed
	b.RecordSuccess()
	if b.State() != StateClosed {
		t.Fatalf("expected recovered to closed, got %s", b.State())
	}
}

func TestCircuitBreaker_CanaryFailureDoublesCooloff(t *testing.T) {
	cfg := Config{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		CooloffDuration:  20 * time.Millisecond,
		MaxCooloff:       200 * time.Millisecond,
	}
	b := NewBreaker("test-provider", cfg)

	// Trip
	b.RecordFailure()
	b.RecordFailure()

	// Wait for half-open
	time.Sleep(25 * time.Millisecond)
	_ = b.Allow() // Trigger transition to half-open

	// Canary fails
	b.RecordFailure()
	if b.State() != StateOpen {
		t.Fatalf("expected immediately tripped back to open")
	}

	// Should still be open after 20ms because cooloff doubled to 40ms
	time.Sleep(25 * time.Millisecond)
	if b.State() != StateOpen {
		t.Fatalf("expected still open after 25ms because cooloff doubled to 40ms")
	}
}

func TestRegistry_ProbeFunction(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry(DefaultConfig())

	// Successful probe
	err := reg.Probe(ctx, "mock-openai", func(ctx context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected probe error: %v", err)
	}

	b := reg.Get("mock-openai")
	if b.State() != StateClosed {
		t.Fatalf("expected state closed, got %s", b.State())
	}

	// Failing probe
	testErr := errors.New("connection refused")
	_ = reg.Probe(ctx, "mock-broken", func(ctx context.Context) error {
		return testErr
	})
	bBroken := reg.Get("mock-broken")
	if bBroken.fails != 1 {
		t.Errorf("expected 1 recorded failure on broken provider, got %d", bBroken.fails)
	}
}

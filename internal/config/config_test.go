package config

import (
	"errors"
	"testing"

	"github.com/ra1/model-gateway/internal/rules"
)

func TestRuntimeConfig_AtomicSwapNoDowntime(t *testing.T) {
	initial := DefaultRuntimeConfig()
	initial.DefaultRoutingStrategy = "lowest_latency"

	mgr, err := NewManager(initial)
	if err != nil {
		t.Fatalf("failed to init manager: %v", err)
	}

	snap1 := mgr.Get()
	if snap1.DefaultRoutingStrategy != "lowest_latency" {
		t.Errorf("expected lowest_latency, got %s", snap1.DefaultRoutingStrategy)
	}

	// Update to least_busy
	updated := DefaultRuntimeConfig()
	updated.DefaultRoutingStrategy = "least_busy"

	err = mgr.Update(updated)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	snap2 := mgr.Get()
	if snap2.DefaultRoutingStrategy != "least_busy" {
		t.Errorf("expected least_busy after swap, got %s", snap2.DefaultRoutingStrategy)
	}

	// Ensure snap1 remained immutable
	if snap1.DefaultRoutingStrategy != "lowest_latency" {
		t.Errorf("snap1 was mutated!")
	}
}

func TestRuntimeConfig_InvalidConfigRollback(t *testing.T) {
	mgr, _ := NewManager(nil)
	origRev := mgr.Get().Revision

	// Attempt update with broken CEL syntax
	broken := DefaultRuntimeConfig()
	broken.CELRules = []*rules.Rule{
		{
			ID:        "broken-rule",
			Condition: "request.model == == bad syntax",
		},
	}

	err := mgr.Update(broken)
	if err == nil {
		t.Fatalf("expected update to fail on invalid CEL rule")
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig, got %v", err)
	}

	// Active configuration must remain untouched
	current := mgr.Get()
	if current.Revision != origRev {
		t.Errorf("expected original revision %s, got %s", origRev, current.Revision)
	}
	if len(current.CELRules) != 0 {
		t.Errorf("expected empty CEL rules, got %v", current.CELRules)
	}
}

func TestRuntimeConfig_EmptyStrategyRejected(t *testing.T) {
	mgr, _ := NewManager(nil)
	invalid := DefaultRuntimeConfig()
	invalid.DefaultRoutingStrategy = ""

	err := mgr.Update(invalid)
	if err == nil {
		t.Fatalf("expected error for empty strategy")
	}
}

package policy

import (
	"context"
	"testing"
)

func TestPolicy_ModelAllowlist_BlocksDisallowedModel(t *testing.T) {
	engine := NewEngine()

	policy := Policy{
		ID:    "intern-restriction",
		Scope: "team:interns",
		Rules: Rules{
			AllowedModels: []string{"gpt-4o-mini", "claude-3-haiku*"},
		},
	}
	if err := engine.RegisterPolicy(policy); err != nil {
		t.Fatalf("failed to register policy: %v", err)
	}

	ctx := context.Background()

	// Allowed model: gpt-4o-mini
	err := engine.Evaluate(ctx, RequestContext{
		Team:  "interns",
		Model: "gpt-4o-mini",
	})
	if err != nil {
		t.Errorf("expected allowed model to succeed, got %v", err)
	}

	// Allowed model by glob: claude-3-haiku-20240307
	err = engine.Evaluate(ctx, RequestContext{
		Team:  "interns",
		Model: "claude-3-haiku-20240307",
	})
	if err != nil {
		t.Errorf("expected glob matched model to succeed, got %v", err)
	}

	// Disallowed model: gpt-4o
	err = engine.Evaluate(ctx, RequestContext{
		Team:  "interns",
		Model: "gpt-4o",
	})
	if err == nil {
		t.Fatalf("expected error for disallowed model, got nil")
	}

	pvErr, ok := err.(*PolicyViolationError)
	if !ok {
		t.Fatalf("expected PolicyViolationError, got %T", err)
	}
	if pvErr.Code != CodeForbiddenModel {
		t.Errorf("expected code %s, got %s", CodeForbiddenModel, pvErr.Code)
	}

	// Different team should not be blocked
	err = engine.Evaluate(ctx, RequestContext{
		Team:  "researchers",
		Model: "gpt-4o",
	})
	if err != nil {
		t.Errorf("different team should not be blocked by interns policy: %v", err)
	}
}

func TestPolicy_TokenCap_RejectsExcessiveMaxTokens(t *testing.T) {
	engine := NewEngine()

	policy := Policy{
		ID:    "max-tokens-cap",
		Scope: "*",
		Rules: Rules{
			MaxTokensLimit: 4096,
			MaxInputTokens: 8192,
		},
	}
	if err := engine.RegisterPolicy(policy); err != nil {
		t.Fatalf("failed to register policy: %v", err)
	}

	ctx := context.Background()

	// Valid max_tokens
	err := engine.Evaluate(ctx, RequestContext{
		MaxTokens:            2048,
		EstimatedInputTokens: 1000,
	})
	if err != nil {
		t.Errorf("expected valid tokens to pass, got %v", err)
	}

	// Excessive max_tokens
	err = engine.Evaluate(ctx, RequestContext{
		MaxTokens:            8192,
		EstimatedInputTokens: 1000,
	})
	if err == nil {
		t.Fatalf("expected error for excessive max tokens, got nil")
	}
	pvErr, ok := err.(*PolicyViolationError)
	if !ok || pvErr.Code != CodeExcessiveMaxTokens {
		t.Errorf("expected CodeExcessiveMaxTokens, got %v", err)
	}

	// Excessive input tokens
	err = engine.Evaluate(ctx, RequestContext{
		MaxTokens:            1024,
		EstimatedInputTokens: 12000,
	})
	if err == nil {
		t.Fatalf("expected error for excessive input tokens, got nil")
	}
	pvErr, ok = err.(*PolicyViolationError)
	if !ok || pvErr.Code != CodeExcessiveInputTokens {
		t.Errorf("expected CodeExcessiveInputTokens, got %v", err)
	}
}

func TestPolicy_IPAllowlist_AllowsInternalCIDR(t *testing.T) {
	engine := NewEngine()

	policy := Policy{
		ID:    "internal-network-only",
		Scope: "org:acme",
		Rules: Rules{
			IPAllowlist: []string{"10.0.0.0/8", "192.168.1.0/24", "127.0.0.1"},
		},
	}
	if err := engine.RegisterPolicy(policy); err != nil {
		t.Fatalf("failed to register policy: %v", err)
	}

	ctx := context.Background()

	// 10.1.2.3 in 10.0.0.0/8
	err := engine.Evaluate(ctx, RequestContext{
		Org:      "acme",
		ClientIP: "10.1.2.3:49210",
	})
	if err != nil {
		t.Errorf("expected 10.1.2.3 to be allowed: %v", err)
	}

	// 192.168.1.50 in 192.168.1.0/24
	err = engine.Evaluate(ctx, RequestContext{
		Org:      "acme",
		ClientIP: "192.168.1.50",
	})
	if err != nil {
		t.Errorf("expected 192.168.1.50 to be allowed: %v", err)
	}

	// 127.0.0.1 exact match
	err = engine.Evaluate(ctx, RequestContext{
		Org:      "acme",
		ClientIP: "127.0.0.1",
	})
	if err != nil {
		t.Errorf("expected 127.0.0.1 to be allowed: %v", err)
	}
}

func TestPolicy_IPAllowlist_BlocksExternalIP(t *testing.T) {
	engine := NewEngine()

	policy := Policy{
		ID:    "internal-network-only",
		Scope: "org:acme",
		Rules: Rules{
			IPAllowlist: []string{"10.0.0.0/8"},
		},
	}
	if err := engine.RegisterPolicy(policy); err != nil {
		t.Fatalf("failed to register policy: %v", err)
	}

	ctx := context.Background()

	// Public IP 203.0.113.195 should be blocked
	err := engine.Evaluate(ctx, RequestContext{
		Org:      "acme",
		ClientIP: "203.0.113.195",
	})
	if err == nil {
		t.Fatalf("expected error for external IP, got nil")
	}

	pvErr, ok := err.(*PolicyViolationError)
	if !ok || pvErr.Code != CodeIPForbidden {
		t.Errorf("expected CodeIPForbidden, got %v", err)
	}
}

func TestPolicy_RequiredHeadersEnforcement(t *testing.T) {
	engine := NewEngine()

	policy := Policy{
		ID:    "audit-headers-required",
		Scope: "*",
		Rules: Rules{
			RequiredHeaders: []string{"X-Cost-Center", "X-Client-App-ID"},
		},
	}
	if err := engine.RegisterPolicy(policy); err != nil {
		t.Fatalf("failed to register policy: %v", err)
	}

	ctx := context.Background()

	// Missing both
	err := engine.Evaluate(ctx, RequestContext{
		Headers: map[string]string{},
	})
	if err == nil {
		t.Fatalf("expected missing header error, got nil")
	}

	// Having only one
	err = engine.Evaluate(ctx, RequestContext{
		Headers: map[string]string{
			"X-Cost-Center": "eng-ml-infra",
		},
	})
	if err == nil {
		t.Fatalf("expected missing X-Client-App-ID error, got nil")
	}
	pvErr, ok := err.(*PolicyViolationError)
	if !ok || pvErr.Code != CodeMissingRequiredHeader {
		t.Errorf("expected CodeMissingRequiredHeader, got %v", err)
	}

	// Having both (case-insensitive test)
	err = engine.Evaluate(ctx, RequestContext{
		Headers: map[string]string{
			"x-cost-center":    "eng-ml-infra",
			"x-client-app-id": "service-worker-01",
		},
	})
	if err != nil {
		t.Errorf("expected both headers to pass: %v", err)
	}
}

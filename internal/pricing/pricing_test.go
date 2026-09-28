package pricing

import (
	"testing"
)

func TestRegistry_CalculateCostMicro(t *testing.T) {
	reg := NewRegistry()

	// gpt-4o:
	// 1,000 input tokens * $0.000005 = $0.005 -> 5,000 micro-dollars
	// 500 output tokens * $0.000015 = $0.0075 -> 7,500 micro-dollars
	// Total = 12,500 micro-dollars ($0.0125)
	cost := reg.CalculateCostMicro("gpt-4o", 1000, 500, 0)
	if cost != 12500 {
		t.Errorf("expected 12500 micro-dollars, got %d", cost)
	}

	// Local model should be 0
	localCost := reg.CalculateCostMicro("local/llama-3", 1000, 500, 0)
	if localCost != 0 {
		t.Errorf("expected 0 micro-dollars for local model, got %d", localCost)
	}
}

func TestRegistry_GetPricing(t *testing.T) {
	reg := NewRegistry()

	p, ok := reg.GetPricing("claude-3-5-sonnet")
	if !ok {
		t.Fatalf("expected claude-3-5-sonnet to be found")
	}
	if p.Provider != "anthropic" || p.MaxContextTokens != 200000 {
		t.Errorf("unexpected pricing details: %+v", p)
	}
}

package fallback

import (
	"testing"

	"github.com/ra1/model-gateway/internal/schema"
)

func TestPolicy_GetNextModel(t *testing.T) {
	p := NewPolicy()
	p.SetChain("gpt-4o", []string{"claude-3-5-sonnet", "gemini-1.5-pro"})

	visited := map[string]bool{"gpt-4o": true}

	// 1. First fallback is claude-3-5-sonnet
	m1, err := p.GetNextModel("gpt-4o", visited)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m1 != "claude-3-5-sonnet" {
		t.Errorf("expected claude-3-5-sonnet, got %s", m1)
	}
	visited[m1] = true

	// 2. Second fallback is gemini-1.5-pro
	m2, err := p.GetNextModel("gpt-4o", visited)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m2 != "gemini-1.5-pro" {
		t.Errorf("expected gemini-1.5-pro, got %s", m2)
	}
	visited[m2] = true

	// 3. Chain exhausted
	_, err = p.GetNextModel("gpt-4o", visited)
	if err != ErrFallbackChainExhausted {
		t.Errorf("expected ErrFallbackChainExhausted, got %v", err)
	}
}

func TestAnnotateDowngrade(t *testing.T) {
	resp := schema.NewChatResponse("test-id", "gpt-4o")

	// Same model: not downgraded
	AnnotateDowngrade(resp, "gpt-4o", "gpt-4o")
	if resp.Metadata["downgraded"] == "true" {
		t.Errorf("expected downgraded to not be true for same model")
	}

	// Downgraded model
	AnnotateDowngrade(resp, "gpt-4o", "claude-3-5-sonnet")
	if resp.Model != "claude-3-5-sonnet" {
		t.Errorf("expected effective model claude-3-5-sonnet, got %s", resp.Model)
	}
	if resp.Metadata["downgraded"] != "true" {
		t.Errorf("expected downgraded=true in metadata")
	}
	if resp.Metadata["requested_model"] != "gpt-4o" {
		t.Errorf("expected requested_model=gpt-4o, got %s", resp.Metadata["requested_model"])
	}
}

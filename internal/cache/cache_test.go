package cache

import (
	"testing"
	"time"

	"github.com/ra1/model-gateway/internal/schema"
)

func TestCache_ExactMatch_ReturnsCachedResponse(t *testing.T) {
	engine := NewEngine(Config{MaxEntries: 100, DefaultTTL: 10 * time.Minute})

	req := &schema.CanonicalChatRequest{
		RequestedModel: "gpt-4o",
		Messages: []schema.ChatMessage{
			{Role: "user", Content: "What is 2+2?"},
		},
	}
	cacheKey := HashRequest("org-finance", req)

	resp := &schema.CanonicalChatResponse{
		ID:    "resp-cached-1",
		Model: "gpt-4o",
		Choices: []schema.Choice{
			{
				Message: schema.ChatMessage{Role: "assistant", Content: "4"},
			},
		},
	}

	// Initial check: miss
	_, found := engine.GetExact(cacheKey)
	if found {
		t.Fatalf("expected initial cache miss")
	}

	// Put
	engine.PutExact(cacheKey, resp, 10*time.Minute)

	// Second check: hit
	cached, found := engine.GetExact(cacheKey)
	if !found || cached == nil {
		t.Fatalf("expected cache hit")
	}
	if cached.ID != "resp-cached-1" {
		t.Errorf("expected cached ID resp-cached-1, got %s", cached.ID)
	}
}

func TestCache_SemanticMatch_ExceedsThreshold095(t *testing.T) {
	engine := NewEngine(Config{
		MaxEntries:        100,
		DefaultTTL:        10 * time.Minute,
		SemanticThreshold: 0.95,
	})

	// Baseline indexed prompt: "What is the capital of France?"
	vecBase := Vector{0.10, 0.20, 0.90, 0.05}
	resp := &schema.CanonicalChatResponse{
		ID: "resp-paris",
		Choices: []schema.Choice{
			{Message: schema.ChatMessage{Role: "assistant", Content: "Paris."}},
		},
	}
	engine.PutSemantic("entry-paris", vecBase, "What is the capital of France?", resp, 10*time.Minute)

	// Very similar query vector (cosine similarity ~0.999)
	vecQueryHigh := Vector{0.101, 0.201, 0.899, 0.051}
	cached, sim, found := engine.GetSemantic(vecQueryHigh)
	if !found || cached == nil {
		t.Fatalf("expected semantic cache hit for similarity >= 0.95")
	}
	if sim < 0.95 {
		t.Errorf("expected similarity >= 0.95, got %f", sim)
	}
	if cached.ID != "resp-paris" {
		t.Errorf("expected resp-paris, got %s", cached.ID)
	}
}

func TestCache_SemanticMatch_RejectsBelowThreshold095(t *testing.T) {
	engine := NewEngine(Config{
		MaxEntries:        100,
		DefaultTTL:        10 * time.Minute,
		SemanticThreshold: 0.95,
	})

	vecBase := Vector{0.10, 0.20, 0.90, 0.05}
	resp := &schema.CanonicalChatResponse{ID: "resp-paris"}
	engine.PutSemantic("entry-paris", vecBase, "capital of france", resp, 10*time.Minute)

	// Dissimilar query vector: "What is quantum mechanics?"
	vecQueryLow := Vector{0.80, 0.10, 0.05, 0.90}
	cached, sim, found := engine.GetSemantic(vecQueryLow)
	if found || cached != nil {
		t.Fatalf("expected semantic cache miss for low similarity (%f)", sim)
	}
	if sim >= 0.95 {
		t.Errorf("expected similarity < 0.95, got %f", sim)
	}
}

func TestCache_LRUEviction(t *testing.T) {
	engine := NewEngine(Config{MaxEntries: 2, DefaultTTL: 10 * time.Minute})

	r1 := &schema.CanonicalChatResponse{ID: "r1"}
	r2 := &schema.CanonicalChatResponse{ID: "r2"}
	r3 := &schema.CanonicalChatResponse{ID: "r3"}

	engine.PutExact("k1", r1, 10*time.Minute)
	engine.PutExact("k2", r2, 10*time.Minute)

	// Access k1 so k2 becomes the oldest
	_, _ = engine.GetExact("k1")

	// Insert k3 -> should evict k2
	engine.PutExact("k3", r3, 10*time.Minute)

	if _, found := engine.GetExact("k2"); found {
		t.Errorf("expected k2 to be evicted by LRU")
	}
	if _, found := engine.GetExact("k1"); !found {
		t.Errorf("expected k1 to be retained")
	}
	if _, found := engine.GetExact("k3"); !found {
		t.Errorf("expected k3 to be retained")
	}
}

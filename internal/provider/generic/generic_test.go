package generic

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ra1/model-gateway/internal/provider"
	"github.com/ra1/model-gateway/internal/schema"
	"github.com/ra1/model-gateway/test/mockprovider"
)

func TestGenericAdapter_Execute(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	adapter := NewAdapter(http.DefaultClient)
	cred := &provider.Credential{
		BaseURL: mock.URL + "/v1",
	}

	req := &schema.CanonicalChatRequest{
		RequestedModel: "local/llama-3.1-70b",
		Messages: []schema.ChatMessage{
			{Role: schema.RoleUser, Content: "Hello local server"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := adapter.Execute(ctx, req, cred)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if resp.Model != "local/llama-3.1-70b" {
		t.Errorf("expected model local/llama-3.1-70b preserved, got %s", resp.Model)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
}

func TestGenericAdapter_SupportsModel(t *testing.T) {
	adapter := NewAdapter(http.DefaultClient)

	tests := []struct {
		model    string
		expected bool
	}{
		{"local/llama-3", true},
		{"vllm/mistral", true},
		{"ollama/qwen", true},
		{"self-hosted/deepseek", true},
		{"gpt-4o", false},
	}

	for _, tt := range tests {
		got := adapter.SupportsModel(tt.model)
		if got != tt.expected {
			t.Errorf("SupportsModel(%s) = %v, expected %v", tt.model, got, tt.expected)
		}
	}
}

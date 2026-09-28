package anthropic

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ra1/model-gateway/internal/provider"
	"github.com/ra1/model-gateway/internal/schema"
	"github.com/ra1/model-gateway/test/mockprovider"
)

func TestAnthropicAdapter_Execute(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	adapter := NewAdapter(http.DefaultClient)
	cred := &provider.Credential{
		APIKey:  "ant-test-key",
		BaseURL: mock.URL + "/v1",
	}

	req := &schema.CanonicalChatRequest{
		RequestedModel: "claude-3-5-sonnet",
		Messages: []schema.ChatMessage{
			{Role: schema.RoleSystem, Content: "Be helpful"},
			{Role: schema.RoleUser, Content: "Hello Claude"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := adapter.Execute(ctx, req, cred)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if resp.Model != "claude-3-5-sonnet" {
		t.Errorf("expected claude-3-5-sonnet, got %s", resp.Model)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Usage.PromptTokens != 15 || resp.Usage.CompletionTokens != 10 {
		t.Errorf("unexpected usage: %+v", resp.Usage)
	}

	history := mock.GetCallHistory()
	if len(history) != 1 {
		t.Fatalf("expected 1 mock call, got %d", len(history))
	}
	if history[0].Headers.Get("x-api-key") != "ant-test-key" {
		t.Errorf("expected x-api-key header ant-test-key, got %s", history[0].Headers.Get("x-api-key"))
	}
}

func TestTranslateToAnthropic_AlternationAndSystem(t *testing.T) {
	req := &schema.CanonicalChatRequest{
		RequestedModel: "claude-3-5-sonnet",
		Messages: []schema.ChatMessage{
			{Role: schema.RoleSystem, Content: "System prompt"},
			{Role: schema.RoleUser, Content: "User message 1"},
			{Role: schema.RoleUser, Content: "User message 2"},
		},
	}

	payload, err := translateToAnthropic(req)
	if err != nil {
		t.Fatalf("translation error: %v", err)
	}

	if payload.System != "System prompt" {
		t.Errorf("expected System prompt, got %s", payload.System)
	}
	// Consecutively repeated user messages should coalesce into 1 alternating turn
	if len(payload.Messages) != 1 {
		t.Fatalf("expected 1 coalesced turn, got %d", len(payload.Messages))
	}
	if len(payload.Messages[0].Content) != 2 {
		t.Errorf("expected 2 content blocks inside coalesced user turn, got %d", len(payload.Messages[0].Content))
	}
}

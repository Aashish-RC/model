package gemini

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ra1/model-gateway/internal/provider"
	"github.com/ra1/model-gateway/internal/schema"
	"github.com/ra1/model-gateway/test/mockprovider"
)

func TestGeminiAdapter_Execute(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	adapter := NewAdapter(http.DefaultClient)
	cred := &provider.Credential{
		APIKey:  "goog-test-key",
		BaseURL: mock.URL + "/v1beta",
	}

	req := &schema.CanonicalChatRequest{
		RequestedModel: "gemini-1.5-pro",
		Messages: []schema.ChatMessage{
			{Role: schema.RoleSystem, Content: "Act as an expert"},
			{Role: schema.RoleUser, Content: "Hello Gemini"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := adapter.Execute(ctx, req, cred)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if resp.Model != "gemini-1.5-pro" {
		t.Errorf("expected gemini-1.5-pro, got %s", resp.Model)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Usage.PromptTokens != 20 || resp.Usage.CompletionTokens != 12 {
		t.Errorf("unexpected usage: %+v", resp.Usage)
	}

	history := mock.GetCallHistory()
	if len(history) != 1 {
		t.Fatalf("expected 1 mock call, got %d", len(history))
	}
	if history[0].Headers.Get("x-goog-api-key") != "goog-test-key" {
		t.Errorf("expected x-goog-api-key header goog-test-key, got %s", history[0].Headers.Get("x-goog-api-key"))
	}
}

func TestTranslateToGemini_SystemInstructionAndRoles(t *testing.T) {
	req := &schema.CanonicalChatRequest{
		RequestedModel: "gemini-1.5-pro",
		Messages: []schema.ChatMessage{
			{Role: schema.RoleSystem, Content: "System prompt"},
			{Role: schema.RoleUser, Content: "User message"},
			{Role: schema.RoleAssistant, Content: "Model message"},
		},
	}

	payload, err := translateToGemini(req)
	if err != nil {
		t.Fatalf("translation error: %v", err)
	}

	if payload.SystemInstruction == nil || len(payload.SystemInstruction.Parts) == 0 {
		t.Fatalf("expected SystemInstruction to be populated")
	}
	if payload.SystemInstruction.Parts[0].Text != "System prompt" {
		t.Errorf("expected System prompt, got %s", payload.SystemInstruction.Parts[0].Text)
	}

	if len(payload.Contents) != 2 {
		t.Fatalf("expected 2 content turns, got %d", len(payload.Contents))
	}
	if payload.Contents[0].Role != "user" || payload.Contents[1].Role != "model" {
		t.Errorf("expected roles user and model, got %s and %s", payload.Contents[0].Role, payload.Contents[1].Role)
	}
}

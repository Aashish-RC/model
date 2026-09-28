package openai

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/ra1/model-gateway/internal/provider"
	"github.com/ra1/model-gateway/internal/schema"
	"github.com/ra1/model-gateway/test/mockprovider"
)

func TestOpenAIAdapter_Execute(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	adapter := NewAdapter(http.DefaultClient)
	cred := &provider.Credential{
		APIKey:  "sk-test-openai",
		BaseURL: mock.URL + "/v1",
	}

	req := &schema.CanonicalChatRequest{
		RequestedModel: "gpt-4o",
		Messages: []schema.ChatMessage{
			{Role: schema.RoleUser, Content: "Hello OpenAI"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := adapter.Execute(ctx, req, cred)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if resp.Model != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", resp.Model)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Usage.TotalTokens != 20 {
		t.Errorf("expected 20 total tokens, got %d", resp.Usage.TotalTokens)
	}

	history := mock.GetCallHistory()
	if len(history) != 1 {
		t.Fatalf("expected 1 mock call, got %d", len(history))
	}
	if history[0].APIKey != "Bearer sk-test-openai" {
		t.Errorf("expected header Bearer sk-test-openai, got %s", history[0].APIKey)
	}
}

func TestOpenAIAdapter_ExecuteStream(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	adapter := NewAdapter(http.DefaultClient)
	cred := &provider.Credential{
		APIKey:  "sk-test-stream",
		BaseURL: mock.URL + "/v1",
	}

	req := &schema.CanonicalChatRequest{
		RequestedModel: "gpt-4o",
		Stream:         true,
		Messages: []schema.ChatMessage{
			{Role: schema.RoleUser, Content: "Stream to me"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	it, err := adapter.ExecuteStream(ctx, req, cred)
	if err != nil {
		t.Fatalf("execute stream error: %v", err)
	}
	defer it.Close()

	var chunks []*schema.StreamChunk
	for {
		chunk, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected chunk error: %v", err)
		}
		chunks = append(chunks, chunk)
	}

	if len(chunks) < 5 {
		t.Errorf("expected at least 5 stream chunks, got %d", len(chunks))
	}
}

func TestOpenAIAdapter_UpstreamError(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	adapter := NewAdapter(http.DefaultClient)
	cred := &provider.Credential{
		APIKey:  "sk-test-err",
		BaseURL: mock.URL + "/v1",
		Headers: map[string]string{
			"X-Mock-Status": "429",
		},
	}

	req := &schema.CanonicalChatRequest{
		RequestedModel: "gpt-4o",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := adapter.Execute(ctx, req, cred)
	if err == nil {
		t.Fatalf("expected error on 429 status, got nil")
	}

	upErr, ok := err.(*UpstreamError)
	if !ok {
		t.Fatalf("expected *UpstreamError type, got %T", err)
	}
	if upErr.StatusCode != 429 {
		t.Errorf("expected 429 status code, got %d", upErr.StatusCode)
	}
}

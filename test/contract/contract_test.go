package contract

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/ra1/model-gateway/internal/provider"
	"github.com/ra1/model-gateway/internal/provider/anthropic"
	"github.com/ra1/model-gateway/internal/provider/gemini"
	"github.com/ra1/model-gateway/internal/provider/generic"
	"github.com/ra1/model-gateway/internal/provider/openai"
	"github.com/ra1/model-gateway/internal/schema"
	"github.com/ra1/model-gateway/test/mockprovider"
)

func TestContract_AllAdapters(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	adapters := []struct {
		name    string
		adapter provider.Adapter
		model   string
		baseURL string
	}{
		{
			name:    "OpenAI Adapter",
			adapter: openai.NewAdapter(http.DefaultClient),
			model:   "gpt-4o",
			baseURL: mock.URL + "/v1",
		},
		{
			name:    "Anthropic Adapter",
			adapter: anthropic.NewAdapter(http.DefaultClient),
			model:   "claude-3-5-sonnet",
			baseURL: mock.URL + "/v1",
		},
		{
			name:    "Gemini Adapter",
			adapter: gemini.NewAdapter(http.DefaultClient),
			model:   "gemini-1.5-pro",
			baseURL: mock.URL + "/v1beta",
		},
		{
			name:    "Generic Adapter",
			adapter: generic.NewAdapter(http.DefaultClient),
			model:   "local/llama-3",
			baseURL: mock.URL + "/v1",
		},
	}

	for _, a := range adapters {
		t.Run(a.name+"_SynchronousAndUsage", func(t *testing.T) {
			cred := &provider.Credential{
				APIKey:  "test-contract-key",
				BaseURL: a.baseURL,
			}

			req := &schema.CanonicalChatRequest{
				RequestedModel: a.model,
				Messages: []schema.ChatMessage{
					{Role: schema.RoleUser, Content: "Hello contract"},
				},
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := a.adapter.Execute(ctx, req, cred)
			if err != nil {
				t.Fatalf("contract execution error: %v", err)
			}

			if len(resp.Choices) == 0 {
				t.Fatalf("expected at least 1 choice in response")
			}
			if resp.Choices[0].Message.Content == "" {
				t.Errorf("expected non-empty message content")
			}
			if resp.Usage.TotalTokens <= 0 {
				t.Errorf("expected valid usage total tokens, got %d", resp.Usage.TotalTokens)
			}
		})

		t.Run(a.name+"_Streaming", func(t *testing.T) {
			cred := &provider.Credential{
				APIKey:  "test-contract-key",
				BaseURL: a.baseURL,
			}

			req := &schema.CanonicalChatRequest{
				RequestedModel: a.model,
				Stream:         true,
				Messages: []schema.ChatMessage{
					{Role: schema.RoleUser, Content: "Stream contract"},
				},
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			it, err := a.adapter.ExecuteStream(ctx, req, cred)
			if err != nil {
				t.Fatalf("contract stream error: %v", err)
			}
			defer it.Close()

			chunksReceived := 0
			for {
				chunk, err := it.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("stream iteration error: %v", err)
				}
				if chunk != nil {
					chunksReceived++
				}
			}

			// Non-OpenAI adapters parse mock messages or chunks
			if a.name == "OpenAI Adapter" || a.name == "Generic Adapter" {
				if chunksReceived < 3 {
					t.Errorf("expected multiple chunks from OpenAI stream, got %d", chunksReceived)
				}
			}
		})

		t.Run(a.name+"_ErrorHandling", func(t *testing.T) {
			cred := &provider.Credential{
				APIKey:  "test-contract-key",
				BaseURL: a.baseURL,
				Headers: map[string]string{
					"X-Mock-Status": "429",
				},
			}

			req := &schema.CanonicalChatRequest{
				RequestedModel: a.model,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := a.adapter.Execute(ctx, req, cred)
			if err == nil {
				t.Fatalf("expected error on injected 429 status")
			}
		})
	}
}

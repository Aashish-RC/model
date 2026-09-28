package provider

import (
	"context"
	"net/http"

	"github.com/ra1/model-gateway/internal/schema"
)

// Credential encapsulates the necessary secrets and configuration to reach an upstream model.
type Credential struct {
	ID           string
	ProviderName string
	APIKey       string
	BaseURL      string
	Organization string
	Project      string
	Headers      map[string]string
}

// StreamIterator defines the interface for consuming streaming chunks.
type StreamIterator interface {
	// Next returns the next StreamChunk or io.EOF when stream is concluded.
	Next() (*schema.StreamChunk, error)
	// Close releases any allocated underlying network buffers or response bodies.
	Close() error
}

// Adapter defines the uniform seam for all LLM providers.
type Adapter interface {
	// Name returns the canonical provider name (e.g. "openai", "anthropic", "gemini", "generic").
	Name() string
	// Execute executes a synchronous request and returns the normalized response.
	Execute(ctx context.Context, req *schema.CanonicalChatRequest, cred *Credential) (*schema.CanonicalChatResponse, error)
	// ExecuteStream initiates a streaming request and returns an iterator over SSE chunks.
	ExecuteStream(ctx context.Context, req *schema.CanonicalChatRequest, cred *Credential) (StreamIterator, error)
	// SupportsModel returns true if this adapter can execute the requested model name.
	SupportsModel(model string) bool
}

// HTTPClientDoer allows mocking and injecting HTTP transport.
type HTTPClientDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

package generic

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/ra1/model-gateway/internal/provider"
	"github.com/ra1/model-gateway/internal/provider/openai"
	"github.com/ra1/model-gateway/internal/schema"
)

const (
	ProviderName = "generic"
)

// Adapter implements provider.Adapter for self-hosted and local OpenAI-compatible endpoints.
type Adapter struct {
	underlying *openai.Adapter
}

// NewAdapter creates a new generic adapter.
func NewAdapter(client *http.Client) *Adapter {
	if client == nil {
		client = &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10000,
				MaxIdleConnsPerHost: 2000,
				IdleConnTimeout:     90 * time.Second,
			},
		}
	}
	return &Adapter{
		underlying: openai.NewAdapter(client),
	}
}

func (a *Adapter) Name() string {
	return ProviderName
}

func (a *Adapter) SupportsModel(model string) bool {
	// Generic adapter supports local / self-hosted prefixes
	return strings.HasPrefix(model, "local/") ||
		strings.HasPrefix(model, "vllm/") ||
		strings.HasPrefix(model, "ollama/") ||
		strings.HasPrefix(model, "self-hosted/") ||
		strings.HasPrefix(model, "custom/")
}

// Execute proxies to OpenAI format and patches missing token usage if omitted by local server.
func (a *Adapter) Execute(ctx context.Context, req *schema.CanonicalChatRequest, cred *provider.Credential) (*schema.CanonicalChatResponse, error) {
	// Clean model prefix if specified as local/model-name
	cleanReq := *req
	for _, prefix := range []string{"local/", "vllm/", "ollama/", "self-hosted/", "custom/"} {
		if strings.HasPrefix(cleanReq.RequestedModel, prefix) {
			cleanReq.RequestedModel = strings.TrimPrefix(cleanReq.RequestedModel, prefix)
			break
		}
	}

	resp, err := a.underlying.Execute(ctx, &cleanReq, cred)
	if err != nil {
		return nil, err
	}

	// Fallback usage estimation if local server returned zeros
	if resp.Usage.TotalTokens == 0 && len(resp.Choices) > 0 {
		content := resp.Choices[0].Message.Content
		estComp := len(content) / 4
		if estComp == 0 && len(content) > 0 {
			estComp = 1
		}
		resp.Usage.CompletionTokens = estComp
		resp.Usage.TotalTokens = resp.Usage.PromptTokens + estComp
	}

	resp.Model = req.RequestedModel
	return resp, nil
}

// ExecuteStream proxies streaming to OpenAI format.
func (a *Adapter) ExecuteStream(ctx context.Context, req *schema.CanonicalChatRequest, cred *provider.Credential) (provider.StreamIterator, error) {
	cleanReq := *req
	for _, prefix := range []string{"local/", "vllm/", "ollama/", "self-hosted/", "custom/"} {
		if strings.HasPrefix(cleanReq.RequestedModel, prefix) {
			cleanReq.RequestedModel = strings.TrimPrefix(cleanReq.RequestedModel, prefix)
			break
		}
	}

	return a.underlying.ExecuteStream(ctx, &cleanReq, cred)
}

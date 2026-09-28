package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ra1/model-gateway/internal/provider"
	"github.com/ra1/model-gateway/internal/schema"
)

const (
	DefaultBaseURL = "https://api.openai.com/v1"
	ProviderName   = "openai"
)

// Adapter implements the provider.Adapter interface for OpenAI.
type Adapter struct {
	Client *http.Client
}

// NewAdapter creates a new OpenAI adapter with standard HTTP pooling.
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
	return &Adapter{Client: client}
}

func (a *Adapter) Name() string {
	return ProviderName
}

func (a *Adapter) SupportsModel(model string) bool {
	// Matches gpt-*, o1-*, text-embedding-*, etc.
	return strings.HasPrefix(model, "gpt-") ||
		strings.HasPrefix(model, "o1-") ||
		strings.HasPrefix(model, "o3-") ||
		strings.HasPrefix(model, "chatgpt-")
}

func (a *Adapter) getBaseURL(cred *provider.Credential) string {
	if cred != nil && cred.BaseURL != "" {
		return strings.TrimRight(cred.BaseURL, "/")
	}
	return DefaultBaseURL
}

func (a *Adapter) prepareHeaders(req *http.Request, cred *provider.Credential) {
	req.Header.Set("Content-Type", "application/json")
	if cred != nil {
		if cred.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+cred.APIKey)
		}
		if cred.Organization != "" {
			req.Header.Set("OpenAI-Organization", cred.Organization)
		}
		if cred.Project != "" {
			req.Header.Set("OpenAI-Project", cred.Project)
		}
		for k, v := range cred.Headers {
			req.Header.Set(k, v)
		}
	}
}

// Execute performs synchronous completion.
func (a *Adapter) Execute(ctx context.Context, req *schema.CanonicalChatRequest, cred *provider.Credential) (*schema.CanonicalChatResponse, error) {
	url := a.getBaseURL(cred) + "/chat/completions"

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request error: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create http request error: %w", err)
	}
	a.prepareHeaders(httpReq, cred)

	resp, err := a.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("upstream request error: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response error: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp schema.ErrorResponse
		if jsonErr := json.Unmarshal(respBytes, &errResp); jsonErr == nil && errResp.Error.Message != "" {
			return nil, &UpstreamError{
				StatusCode: resp.StatusCode,
				Message:    errResp.Error.Message,
				Type:       errResp.Error.Type,
				Code:       errResp.Error.Code,
				Headers:    resp.Header.Clone(),
			}
		}
		return nil, &UpstreamError{
			StatusCode: resp.StatusCode,
			Message:    string(respBytes),
			Headers:    resp.Header.Clone(),
		}
	}

	var chatResp schema.CanonicalChatResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshal chat response error: %w", err)
	}

	return &chatResp, nil
}

// ExecuteStream initiates SSE streaming.
func (a *Adapter) ExecuteStream(ctx context.Context, req *schema.CanonicalChatRequest, cred *provider.Credential) (provider.StreamIterator, error) {
	url := a.getBaseURL(cred) + "/chat/completions"

	streamReq := *req
	streamReq.Stream = true
	if streamReq.StreamOptions == nil {
		streamReq.StreamOptions = &schema.StreamOptions{IncludeUsage: true}
	}

	bodyBytes, err := json.Marshal(&streamReq)
	if err != nil {
		return nil, fmt.Errorf("marshal stream request error: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create stream request error: %w", err)
	}
	a.prepareHeaders(httpReq, cred)

	resp, err := a.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("upstream stream request error: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, &UpstreamError{
			StatusCode: resp.StatusCode,
			Message:    string(respBytes),
			Headers:    resp.Header.Clone(),
		}
	}

	return &OpenAIStreamIterator{
		reader: bufio.NewReader(resp.Body),
		closer: resp.Body,
	}, nil
}

// UpstreamError wraps structured HTTP error details from upstream.
type UpstreamError struct {
	StatusCode int
	Message    string
	Type       string
	Code       string
	Headers    http.Header
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream error (status %d): %s", e.StatusCode, e.Message)
}

// OpenAIStreamIterator reads SSE lines from upstream OpenAI.
type OpenAIStreamIterator struct {
	reader *bufio.Reader
	closer io.Closer
}

func (it *OpenAIStreamIterator) Next() (*schema.StreamChunk, error) {
	for {
		line, err := it.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF && len(line) == 0 {
				return nil, io.EOF
			}
			if err == io.EOF {
				// Parse trailing line if present
			} else {
				return nil, err
			}
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ":") {
			// Ping or empty keepalive
			continue
		}

		if !strings.HasPrefix(trimmed, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		if data == "[DONE]" {
			return nil, io.EOF
		}

		var chunk schema.StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// Skip unparseable chunk without aborting stream
			continue
		}

		return &chunk, nil
	}
}

func (it *OpenAIStreamIterator) Close() error {
	if it.closer != nil {
		return it.closer.Close()
	}
	return nil
}

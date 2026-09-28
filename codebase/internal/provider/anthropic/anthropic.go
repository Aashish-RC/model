package anthropic

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
	"github.com/ra1/model-gateway/internal/provider/openai"
	"github.com/ra1/model-gateway/internal/schema"
)

const (
	DefaultBaseURL = "https://api.anthropic.com/v1"
	ProviderName   = "anthropic"
	APIVersion     = "2023-06-01"
)

// Adapter implements provider.Adapter for Anthropic Claude.
type Adapter struct {
	Client *http.Client
}

// NewAdapter creates an Anthropic adapter.
func NewAdapter(client *http.Client) *Adapter {
	if client == nil {
		client = &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        1000,
				MaxIdleConnsPerHost: 100,
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
	return strings.HasPrefix(model, "claude-")
}

func (a *Adapter) getBaseURL(cred *provider.Credential) string {
	if cred != nil && cred.BaseURL != "" {
		return strings.TrimRight(cred.BaseURL, "/")
	}
	return DefaultBaseURL
}

func (a *Adapter) prepareHeaders(req *http.Request, cred *provider.Credential) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", APIVersion)
	if cred != nil {
		if cred.APIKey != "" {
			req.Header.Set("x-api-key", cred.APIKey)
		}
		for k, v := range cred.Headers {
			req.Header.Set(k, v)
		}
	}
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicRequestPayload struct {
	Model         string             `json:"model"`
	System        string             `json:"system,omitempty"`
	Messages      []anthropicMessage `json:"messages"`
	MaxTokens     int                `json:"max_tokens"`
	Temperature   *float32           `json:"temperature,omitempty"`
	TopP          *float32           `json:"top_p,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
}

func translateToAnthropic(req *schema.CanonicalChatRequest) (*anthropicRequestPayload, error) {
	payload := &anthropicRequestPayload{
		Model:         req.RequestedModel,
		MaxTokens:     req.GetEffectiveMaxTokens(),
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		Stream:        req.Stream,
		StopSequences: req.Stop,
	}

	if payload.MaxTokens <= 0 {
		payload.MaxTokens = 4096 // Anthropic requires max_tokens
	}

	var systemParts []string
	var anthropicMsgs []anthropicMessage

	for _, msg := range req.Messages {
		if msg.Role == schema.RoleSystem {
			systemParts = append(systemParts, msg.Content)
			continue
		}

		role := "user"
		if msg.Role == schema.RoleAssistant {
			role = "assistant"
		}

		block := anthropicContentBlock{
			Type: "text",
			Text: msg.Content,
		}

		// Ensure strict alternation by coalescing if previous role matches
		if len(anthropicMsgs) > 0 && anthropicMsgs[len(anthropicMsgs)-1].Role == role {
			anthropicMsgs[len(anthropicMsgs)-1].Content = append(anthropicMsgs[len(anthropicMsgs)-1].Content, block)
		} else {
			anthropicMsgs = append(anthropicMsgs, anthropicMessage{
				Role:    role,
				Content: []anthropicContentBlock{block},
			})
		}
	}

	if len(systemParts) > 0 {
		payload.System = strings.Join(systemParts, "\n\n")
	}

	// Anthropic requires at least 1 message turn; if empty, provide blank turn
	if len(anthropicMsgs) == 0 {
		anthropicMsgs = append(anthropicMsgs, anthropicMessage{
			Role:    "user",
			Content: []anthropicContentBlock{{Type: "text", Text: " "}},
		})
	}

	payload.Messages = anthropicMsgs
	return payload, nil
}

// Execute performs synchronous Anthropic completion.
func (a *Adapter) Execute(ctx context.Context, req *schema.CanonicalChatRequest, cred *provider.Credential) (*schema.CanonicalChatResponse, error) {
	url := a.getBaseURL(cred) + "/messages"

	payload, err := translateToAnthropic(req)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request error: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create anthropic http request error: %w", err)
	}
	a.prepareHeaders(httpReq, cred)

	resp, err := a.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("upstream anthropic error: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read anthropic response error: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &openai.UpstreamError{
			StatusCode: resp.StatusCode,
			Message:    string(respBytes),
			Headers:    resp.Header.Clone(),
		}
	}

	var anthropicResp struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respBytes, &anthropicResp); err != nil {
		return nil, fmt.Errorf("unmarshal anthropic response error: %w", err)
	}

	var responseText string
	for _, c := range anthropicResp.Content {
		if c.Type == "text" {
			responseText += c.Text
		}
	}

	finishReason := schema.FinishReasonStop
	if anthropicResp.StopReason == "max_tokens" {
		finishReason = schema.FinishReasonLength
	}

	chatResp := schema.NewChatResponse(anthropicResp.ID, anthropicResp.Model)
	chatResp.Choices = []schema.Choice{
		{
			Index: 0,
			Message: schema.ChatMessage{
				Role:    schema.RoleAssistant,
				Content: responseText,
			},
			FinishReason: finishReason,
		},
	}
	chatResp.Usage = schema.Usage{
		PromptTokens:     anthropicResp.Usage.InputTokens,
		CompletionTokens: anthropicResp.Usage.OutputTokens,
		TotalTokens:      anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens,
	}

	return chatResp, nil
}

// ExecuteStream initiates streaming for Anthropic.
func (a *Adapter) ExecuteStream(ctx context.Context, req *schema.CanonicalChatRequest, cred *provider.Credential) (provider.StreamIterator, error) {
	url := a.getBaseURL(cred) + "/messages"

	payload, err := translateToAnthropic(req)
	if err != nil {
		return nil, err
	}
	payload.Stream = true

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic stream request error: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create anthropic stream request error: %w", err)
	}
	a.prepareHeaders(httpReq, cred)

	resp, err := a.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("upstream anthropic stream error: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, &openai.UpstreamError{
			StatusCode: resp.StatusCode,
			Message:    string(respBytes),
			Headers:    resp.Header.Clone(),
		}
	}

	return &AnthropicStreamIterator{
		reader: bufio.NewReader(resp.Body),
		closer: resp.Body,
		model:  req.RequestedModel,
	}, nil
}

// AnthropicStreamIterator translates Anthropic SSE events to canonical StreamChunks.
type AnthropicStreamIterator struct {
	reader *bufio.Reader
	closer io.Closer
	model  string
}

func (it *AnthropicStreamIterator) Next() (*schema.StreamChunk, error) {
	for {
		line, err := it.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF && len(line) == 0 {
				return nil, io.EOF
			}
			if err != io.EOF {
				return nil, err
			}
		}

		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}

		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if event.Type == "message_stop" {
			return nil, io.EOF
		}

		if event.Type == "content_block_delta" && event.Delta.Text != "" {
			return &schema.StreamChunk{
				ID:      "msg-chunk",
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   it.model,
				Choices: []schema.StreamChoice{
					{
						Index: 0,
						Delta: schema.StreamDelta{
							Content: event.Delta.Text,
						},
					},
				},
			}, nil
		}
	}
}

func (it *AnthropicStreamIterator) Close() error {
	if it.closer != nil {
		return it.closer.Close()
	}
	return nil
}

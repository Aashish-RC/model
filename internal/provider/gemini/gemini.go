package gemini

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
	DefaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	ProviderName   = "gemini"
)

// Adapter implements provider.Adapter for Google Gemini.
type Adapter struct {
	Client *http.Client
}

// NewAdapter creates a Gemini adapter.
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
	return strings.HasPrefix(model, "gemini-")
}

func (a *Adapter) getBaseURL(cred *provider.Credential) string {
	if cred != nil && cred.BaseURL != "" {
		return strings.TrimRight(cred.BaseURL, "/")
	}
	return DefaultBaseURL
}

type geminiPart struct {
	Text string `json:"text,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiRequestPayload struct {
	Contents          []geminiContent `json:"contents"`
	SystemInstruction *geminiContent  `json:"system_instruction,omitempty"`
	GenerationConfig  *struct {
		Temperature     *float32 `json:"temperature,omitempty"`
		TopP            *float32 `json:"top_p,omitempty"`
		MaxOutputTokens *int     `json:"max_output_tokens,omitempty"`
		StopSequences   []string `json:"stop_sequences,omitempty"`
	} `json:"generationConfig,omitempty"`
}

func translateToGemini(req *schema.CanonicalChatRequest) (*geminiRequestPayload, error) {
	payload := &geminiRequestPayload{}

	maxTokens := req.GetEffectiveMaxTokens()
	var maxTokensPtr *int
	if maxTokens > 0 {
		maxTokensPtr = &maxTokens
	}

	payload.GenerationConfig = &struct {
		Temperature     *float32 `json:"temperature,omitempty"`
		TopP            *float32 `json:"top_p,omitempty"`
		MaxOutputTokens *int     `json:"max_output_tokens,omitempty"`
		StopSequences   []string `json:"stop_sequences,omitempty"`
	}{
		Temperature:     req.Temperature,
		TopP:            req.TopP,
		MaxOutputTokens: maxTokensPtr,
		StopSequences:   req.Stop,
	}

	var contents []geminiContent
	var systemParts []geminiPart

	for _, msg := range req.Messages {
		if msg.Role == schema.RoleSystem {
			systemParts = append(systemParts, geminiPart{Text: msg.Content})
			continue
		}

		role := "user"
		if msg.Role == schema.RoleAssistant {
			role = "model"
		}

		contents = append(contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: msg.Content}},
		})
	}

	if len(systemParts) > 0 {
		payload.SystemInstruction = &geminiContent{
			Role:  "system",
			Parts: systemParts,
		}
	}

	if len(contents) == 0 {
		contents = append(contents, geminiContent{
			Role:  "user",
			Parts: []geminiPart{{Text: " "}},
		})
	}

	payload.Contents = contents
	return payload, nil
}

// Execute performs synchronous Gemini completion.
func (a *Adapter) Execute(ctx context.Context, req *schema.CanonicalChatRequest, cred *provider.Credential) (*schema.CanonicalChatResponse, error) {
	url := fmt.Sprintf("%s/models/%s:generateContent", a.getBaseURL(cred), req.RequestedModel)

	payload, err := translateToGemini(req)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal gemini request error: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create gemini request error: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if cred != nil {
		if cred.APIKey != "" {
			httpReq.Header.Set("x-goog-api-key", cred.APIKey)
		}
		for k, v := range cred.Headers {
			httpReq.Header.Set(k, v)
		}
	}

	resp, err := a.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("upstream gemini error: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read gemini response error: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &openai.UpstreamError{
			StatusCode: resp.StatusCode,
			Message:    string(respBytes),
			Headers:    resp.Header.Clone(),
		}
	}

	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
				Role string `json:"role"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
			Index        int    `json:"index"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}

	if err := json.Unmarshal(respBytes, &geminiResp); err != nil {
		return nil, fmt.Errorf("unmarshal gemini response error: %w", err)
	}

	chatResp := schema.NewChatResponse(fmt.Sprintf("gemini-%d", time.Now().UnixNano()), req.RequestedModel)
	if len(geminiResp.Candidates) > 0 {
		cand := geminiResp.Candidates[0]
		var text string
		for _, p := range cand.Content.Parts {
			text += p.Text
		}

		finish := schema.FinishReasonStop
		if cand.FinishReason == "MAX_TOKENS" {
			finish = schema.FinishReasonLength
		}

		chatResp.Choices = []schema.Choice{
			{
				Index: cand.Index,
				Message: schema.ChatMessage{
					Role:    schema.RoleAssistant,
					Content: text,
				},
				FinishReason: finish,
			},
		}
	}

	chatResp.Usage = schema.Usage{
		PromptTokens:     geminiResp.UsageMetadata.PromptTokenCount,
		CompletionTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
		TotalTokens:      geminiResp.UsageMetadata.TotalTokenCount,
	}

	return chatResp, nil
}

// ExecuteStream initiates streaming for Gemini.
func (a *Adapter) ExecuteStream(ctx context.Context, req *schema.CanonicalChatRequest, cred *provider.Credential) (provider.StreamIterator, error) {
	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", a.getBaseURL(cred), req.RequestedModel)

	payload, err := translateToGemini(req)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal gemini stream request error: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create gemini stream request error: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if cred != nil {
		if cred.APIKey != "" {
			httpReq.Header.Set("x-goog-api-key", cred.APIKey)
		}
		for k, v := range cred.Headers {
			httpReq.Header.Set(k, v)
		}
	}

	resp, err := a.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("upstream gemini stream error: %w", err)
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

	return &GeminiStreamIterator{
		reader: bufio.NewReader(resp.Body),
		closer: resp.Body,
		model:  req.RequestedModel,
	}, nil
}

// GeminiStreamIterator parses Gemini SSE candidates stream into canonical StreamChunks.
type GeminiStreamIterator struct {
	reader *bufio.Reader
	closer io.Closer
	model  string
}

func (it *GeminiStreamIterator) Next() (*schema.StreamChunk, error) {
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
		var geminiChunk struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}

		if err := json.Unmarshal([]byte(data), &geminiChunk); err != nil {
			continue
		}

		var text string
		for _, cand := range geminiChunk.Candidates {
			for _, p := range cand.Content.Parts {
				text += p.Text
			}
		}

		if text != "" {
			return &schema.StreamChunk{
				ID:      "gemini-chunk",
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   it.model,
				Choices: []schema.StreamChoice{
					{
						Index: 0,
						Delta: schema.StreamDelta{
							Content: text,
						},
					},
				},
			}, nil
		}
	}
}

func (it *GeminiStreamIterator) Close() error {
	if it.closer != nil {
		return it.closer.Close()
	}
	return nil
}

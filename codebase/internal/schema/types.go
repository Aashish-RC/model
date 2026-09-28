package schema

import (
	"encoding/json"
	"time"
)

// Role constants
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// FinishReason constants
const (
	FinishReasonStop          = "stop"
	FinishReasonLength        = "length"
	FinishReasonToolCalls     = "tool_calls"
	FinishReasonContentFilter = "content_filter"
)

// ContentPartType constants
const (
	ContentTypeText     = "text"
	ContentTypeImageURL = "image_url"
)

// ContentPart represents a chunk of message content (text or image).
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL holds URL or base64 data for image content.
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// FunctionCall represents legacy function calling or tool call function spec.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCall represents a function tool invocation requested by the model.
type ToolCall struct {
	Index    int          `json:"index,omitempty"`
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// ToolDefinition defines a function tool available to the model.
type ToolDefinition struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition describes a callable function and its JSON schema.
type FunctionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      bool            `json:"strict,omitempty"`
}

// ChatMessage represents a single message turn in the conversation.
type ChatMessage struct {
	Role             string        `json:"role"`
	Content          string        `json:"content,omitempty"`
	MultiContent     []ContentPart `json:"multi_content,omitempty"`
	Name             string        `json:"name,omitempty"`
	ToolCallID       string        `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall    `json:"tool_calls,omitempty"`
	ReasoningContent string        `json:"reasoning_content,omitempty"`
}

// UnmarshalJSON supports both string content and array of ContentPart.
func (m *ChatMessage) UnmarshalJSON(data []byte) error {
	type Alias ChatMessage
	aux := struct {
		RawContent json.RawMessage `json:"content"`
		*Alias
	}{
		Alias: (*Alias)(m),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if len(aux.RawContent) > 0 {
		var s string
		if err := json.Unmarshal(aux.RawContent, &s); err == nil {
			m.Content = s
		} else {
			var parts []ContentPart
			if err := json.Unmarshal(aux.RawContent, &parts); err == nil {
				m.MultiContent = parts
				// Also build plain text representation if all parts are text
				for _, p := range parts {
					if p.Type == ContentTypeText {
						m.Content += p.Text
					}
				}
			}
		}
	}
	return nil
}

// MarshalJSON formats content either as string or array depending on MultiContent.
func (m ChatMessage) MarshalJSON() ([]byte, error) {
	type Alias ChatMessage
	if len(m.MultiContent) > 0 {
		return json.Marshal(&struct {
			Content []ContentPart `json:"content"`
			Alias
		}{
			Content: m.MultiContent,
			Alias:   (Alias)(m),
		})
	}
	return json.Marshal(&struct {
		Content string `json:"content"`
		Alias
	}{
		Content: m.Content,
		Alias:   (Alias)(m),
	})
}

// ResponseFormat configures structured JSON or text output.
type ResponseFormat struct {
	Type       string          `json:"type"` // "text", "json_object", "json_schema"
	JSONSchema json.RawMessage `json:"json_schema,omitempty"`
}

// StreamOptions controls stream metadata chunks.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// CanonicalChatRequest is the unified normalized request struct.
type CanonicalChatRequest struct {
	ID                  string            `json:"id,omitempty"`
	VirtualKeyID        string            `json:"virtual_key_id,omitempty"`
	OrgID               string            `json:"org_id,omitempty"`
	TeamID              string            `json:"team_id,omitempty"`
	User                string            `json:"user,omitempty"`
	RequestedModel      string            `json:"model"`
	EffectiveModel      string            `json:"effective_model,omitempty"`
	Messages            []ChatMessage     `json:"messages"`
	Temperature         *float32          `json:"temperature,omitempty"`
	TopP                *float32          `json:"top_p,omitempty"`
	N                   *int              `json:"n,omitempty"`
	Stream              bool              `json:"stream,omitempty"`
	StreamOptions       *StreamOptions    `json:"stream_options,omitempty"`
	Stop                []string          `json:"stop,omitempty"`
	MaxTokens           *int              `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int              `json:"max_completion_tokens,omitempty"`
	PresencePenalty     *float32          `json:"presence_penalty,omitempty"`
	FrequencyPenalty    *float32          `json:"frequency_penalty,omitempty"`
	Tools               []ToolDefinition  `json:"tools,omitempty"`
	ToolChoice          any               `json:"tool_choice,omitempty"`
	ResponseFormat      *ResponseFormat   `json:"response_format,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
	ReservedBudgetMicro int64             `json:"reserved_budget_micro,omitempty"`
	ReservationID       string            `json:"reservation_id,omitempty"`
}

// GetEffectiveMaxTokens resolves max tokens considering legacy and new parameter names.
func (r *CanonicalChatRequest) GetEffectiveMaxTokens() int {
	if r.MaxCompletionTokens != nil && *r.MaxCompletionTokens > 0 {
		return *r.MaxCompletionTokens
	}
	if r.MaxTokens != nil && *r.MaxTokens > 0 {
		return *r.MaxTokens
	}
	return 0
}

// UsageDetails provides granular token breakdown.
type UsageDetails struct {
	CachedTokens     int `json:"cached_tokens,omitempty"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
	AudioInputTokens int `json:"audio_tokens,omitempty"`
}

// Usage captures token accounting.
type Usage struct {
	PromptTokens            int           `json:"prompt_tokens"`
	CompletionTokens        int           `json:"completion_tokens"`
	TotalTokens             int           `json:"total_tokens"`
	PromptTokensDetails     *UsageDetails `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *UsageDetails `json:"completion_tokens_details,omitempty"`
}

// Choice represents a completion candidate.
type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
	Logprobs     any         `json:"logprobs,omitempty"`
}

// CanonicalChatResponse is the OpenAI-compatible response envelope.
type CanonicalChatResponse struct {
	ID                string            `json:"id"`
	Object            string            `json:"object"`
	Created           int64             `json:"created"`
	Model             string            `json:"model"`
	SystemFingerprint string            `json:"system_fingerprint,omitempty"`
	Choices           []Choice          `json:"choices"`
	Usage             Usage             `json:"usage"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// NewChatResponse constructs a initialized chat response.
func NewChatResponse(id, model string) *CanonicalChatResponse {
	return &CanonicalChatResponse{
		ID:       id,
		Object:   "chat.completion",
		Created:  time.Now().Unix(),
		Model:    model,
		Choices:  make([]Choice, 0, 1),
		Metadata: make(map[string]string),
	}
}

// StreamDelta represents chunk deltas.
type StreamDelta struct {
	Role             string     `json:"role,omitempty"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}

// StreamChoice represents a streaming choice delta.
type StreamChoice struct {
	Index        int         `json:"index"`
	Delta        StreamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

// StreamChunk represents a single SSE chunk data frame.
type StreamChunk struct {
	ID                string         `json:"id"`
	Object            string         `json:"object"`
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	SystemFingerprint string         `json:"system_fingerprint,omitempty"`
	Choices           []StreamChoice `json:"choices"`
	Usage             *Usage         `json:"usage,omitempty"`
}

// APIError represents an OpenAI-compatible error payload.
type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitempty"`
	Code    string `json:"code,omitempty"`
}

// ErrorResponse wraps APIError in an outer error envelope.
type ErrorResponse struct {
	Error APIError `json:"error"`
}

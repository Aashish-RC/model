package schema

import (
	"encoding/json"
	"testing"
)

func TestChatMessage_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name        string
		jsonInput   string
		expectedMsg ChatMessage
	}{
		{
			name:      "Plain string content",
			jsonInput: `{"role":"user","content":"Hello world"}`,
			expectedMsg: ChatMessage{
				Role:    "user",
				Content: "Hello world",
			},
		},
		{
			name:      "Multi-part content blocks",
			jsonInput: `{"role":"user","content":[{"type":"text","text":"Look at this:"},{"type":"image_url","image_url":{"url":"https://example.com/cat.png"}}]}`,
			expectedMsg: ChatMessage{
				Role:    "user",
				Content: "Look at this:",
				MultiContent: []ContentPart{
					{Type: "text", Text: "Look at this:"},
					{Type: "image_url", ImageURL: &ImageURL{URL: "https://example.com/cat.png"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var msg ChatMessage
			if err := json.Unmarshal([]byte(tt.jsonInput), &msg); err != nil {
				t.Fatalf("unexpected unmarshal error: %v", err)
			}
			if msg.Role != tt.expectedMsg.Role {
				t.Errorf("expected role %s, got %s", tt.expectedMsg.Role, msg.Role)
			}
			if msg.Content != tt.expectedMsg.Content {
				t.Errorf("expected content %s, got %s", tt.expectedMsg.Content, msg.Content)
			}
			if len(msg.MultiContent) != len(tt.expectedMsg.MultiContent) {
				t.Errorf("expected multi-content len %d, got %d", len(tt.expectedMsg.MultiContent), len(msg.MultiContent))
			}
		})
	}
}

func TestCanonicalChatRequest_GetEffectiveMaxTokens(t *testing.T) {
	tok100 := 100
	tok200 := 200

	tests := []struct {
		name     string
		req      CanonicalChatRequest
		expected int
	}{
		{
			name:     "Neither set",
			req:      CanonicalChatRequest{},
			expected: 0,
		},
		{
			name: "Only MaxTokens set",
			req: CanonicalChatRequest{
				MaxTokens: &tok100,
			},
			expected: 100,
		},
		{
			name: "Only MaxCompletionTokens set",
			req: CanonicalChatRequest{
				MaxCompletionTokens: &tok200,
			},
			expected: 200,
		},
		{
			name: "Both set - MaxCompletionTokens takes precedence",
			req: CanonicalChatRequest{
				MaxTokens:           &tok100,
				MaxCompletionTokens: &tok200,
			},
			expected: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.req.GetEffectiveMaxTokens()
			if got != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, got)
			}
		})
	}
}

func TestCanonicalChatResponse_Serialization(t *testing.T) {
	resp := NewChatResponse("chatcmpl-test", "test-model")
	resp.Choices = append(resp.Choices, Choice{
		Index: 0,
		Message: ChatMessage{
			Role:    RoleAssistant,
			Content: "Hello!",
		},
		FinishReason: FinishReasonStop,
	})
	resp.Usage = Usage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
	}

	bytes, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded CanonicalChatResponse
	if err := json.Unmarshal(bytes, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.ID != "chatcmpl-test" || decoded.Object != "chat.completion" || len(decoded.Choices) != 1 {
		t.Errorf("unexpected decoded response: %+v", decoded)
	}
	if decoded.Choices[0].Message.Content != "Hello!" {
		t.Errorf("expected content Hello!, got %s", decoded.Choices[0].Message.Content)
	}
}

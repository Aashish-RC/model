# Specification: Provider Adapters

## 1. Purpose
Provider Adapters isolate the core data plane from upstream wire formats and authentication protocols. Each adapter translates the internal `CanonicalChatRequest` into the specific provider's upstream request, manages raw HTTP communication via connection-pooled HTTP clients, and translates upstream responses or SSE streams back into canonical responses.

The initial wave implements four adapters:
1. **OpenAI**: Native OpenAI Chat Completions API.
2. **Anthropic**: Claude Messages API (`/v1/messages`).
3. **Google Gemini**: Gemini GenerateContent / StreamGenerateContent API (`v1beta`).
4. **Generic OpenAI-Compatible**: For self-hosted and local endpoints (e.g. vLLM, Ollama, LocalAI, TensorRT-LLM, HuggingFace TGI).

## 2. Design Strategy & Label: BOTH
- Adopt the Go reference's interface cleanliness, explicit request context passing, and non-allocating byte-buffer parsing for streaming payloads.
- Adopt the Python reference's complete translation matrix for Anthropic message role rules (merging system prompts, combining sequential user turns, alternating message turns, converting tools to `input_schema`), and Gemini content structure (mapping `system_instruction`, `contents.parts`, `function_declarations`).
- Conflict Resolution: For tool calling formats, translate canonical JSON-Schema definitions into provider-native objects (Anthropic `tools[].input_schema`, Gemini `tools[].functionDeclarations`, OpenAI `tools[].function`).

## 3. Provider Interface Definition
```go
type ProviderAdapter interface {
    ProviderName() string
    Execute(ctx context.Context, req *CanonicalChatRequest, cred ProviderCredential) (*CanonicalChatResponse, error)
    ExecuteStream(ctx context.Context, req *CanonicalChatRequest, cred ProviderCredential) (StreamIterator, error)
    ValidateCredential(ctx context.Context, cred ProviderCredential) error
}
```

## 4. Adapter Details

### 4.1 OpenAI Adapter
- **Endpoint**: `POST https://api.openai.com/v1/chat/completions` (or custom base URL).
- **Authentication**: `Authorization: Bearer <api_key>`, optional `OpenAI-Organization`, `OpenAI-Project`.
- **Translation**: Direct mapping of canonical model, messages, tools, tool_choice, response_format.
- **Usage**: Captured from final JSON `usage` block or streaming `usage` chunk when `stream_options.include_usage=true`.

### 4.2 Anthropic Adapter
- **Endpoint**: `POST https://api.anthropic.com/v1/messages`.
- **Authentication**: `x-api-key: <api_key>`, `anthropic-version: 2023-06-01`.
- **Translation Details**:
  - System messages must not appear in `messages` array; extracted and joined into top-level `system` parameter.
  - Strict alternation: Adjacent messages of the same role are coalesced. If conversation begins with an `assistant` message, a placeholder `user` turn is prepended.
  - Tool calls: Mapped to `tool_use` content blocks with ID, name, input. Tool responses mapped to `user` turns with `tool_result` blocks.
  - Stop sequences: Mapped to `stop_sequences`.
  - Max tokens: Mandatory in Anthropic; if unset, defaulted from the model registry.
- **Streaming**: Translates Anthropic SSE events (`message_start`, `content_block_start`, `content_block_delta`, `message_delta`, `message_stop`) to canonical chunk events.

### 4.3 Google Gemini Adapter
- **Endpoint**: `POST https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent` (and `streamGenerateContent?alt=sse`).
- **Authentication**: `x-goog-api-key: <api_key>`.
- **Translation Details**:
  - System messages extracted to `system_instruction.parts[].text`.
  - Role normalization: `assistant` maps to `model`, `user` maps to `user`.
  - Message parts: Text parts map to `text`, images map to `inlineData` with base64 data and mimeType.
  - Function calling: Mapped to `tools[].functionDeclarations`. Tool responses mapped to `functionResponse`.
  - Generation config: `temperature`, `topP`, `maxOutputTokens`, `stopSequences`.
- **Streaming**: Translates Gemini SSE chunks containing `candidates[].content.parts[]` and `usageMetadata`.

### 4.4 Generic OpenAI-Compatible Adapter
- **Endpoint**: Configurable `base_url` + `/chat/completions`.
- **Authentication**: Optional Bearer token or custom header via credentials.
- **Behavior**: Permits custom headers, bypassed organization/project parameters, and accommodates subtle deviations (e.g. absent usage blocks or non-standard stop reason strings).

## 5. Lifecycle States & Context Propagation
- Context deadlines and cancellations are strictly forwarded to the underlying `http.Client`.
- If client connection drops, upstream request context cancels immediately, aborting in-flight HTTP requests.

## 6. Edge Cases
- **Anthropic Blank Assistant Turn**: Anthropic rejects empty assistant turns; adapter strips or provides minimal spacing.
- **Gemini Safety Blocks**: Gemini may return `finishReason: "SAFETY"` without choice content; adapter maps this cleanly to `finish_reason: "content_filter"`.
- **Generic Adapter without Usage**: If local inference servers do not emit usage tokens, adapter triggers a lightweight fallback token estimator.

## 7. Failure Behavior
- Upstream HTTP errors are read with a bounded reader (max 64KB) and passed to the error classifier.
- Network timeouts, DNS failures, and connection resets return structured network errors with retryable flags.

## 8. Configuration
- `providers.openai.base_url`: Default `https://api.openai.com/v1`.
- `providers.anthropic.base_url`: Default `https://api.anthropic.com`.
- `providers.gemini.base_url`: Default `https://generativelanguage.googleapis.com`.
- `http.max_idle_conns`: Pooled idle connection limit (default: 1000).
- `http.max_idle_conns_per_host`: Default 100.
- `http.dial_timeout_ms`: Default 5,000ms.

## 9. Acceptance Tests
1. **OpenAI Protocol Test**: Verify correct request envelope and header emission.
2. **Anthropic Message Flattening**: Verify multi-turn user/assistant sequences and system prompt extraction.
3. **Gemini Tool Mapping**: Verify function declarations and function response wrapping.
4. **Generic Adapter BaseURL Override**: Verify arbitrary base URLs (e.g. `http://localhost:8000/v1`) are targeted without host mutation.
5. **Context Cancel Abortion**: Cancel context while reading from mock adapter and verify immediate goroutine teardown.

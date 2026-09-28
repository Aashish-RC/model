# Specification: Canonical Request/Response Schema and OpenAI-Compatible API

## 1. Purpose
The Canonical Schema provides a unified, strongly-typed internal representation for all model interactions across the data plane. The gateway exposes a high-performance, strictly conforming OpenAI-compatible HTTP API (`/v1/chat/completions`, `/v1/models`, `/health`, `/metrics`), decoupling external callers from upstream provider dialects. Incoming HTTP payloads are parsed into canonical internal structs, routed across adapters, and translated back to OpenAI-compatible responses with zero loss of semantic fidelity.

## 2. Design Strategy & Label: BOTH
This design synthesizes the strengths of both reference paradigms:
- From the Go reference: Zero-allocation string view patterns, strongly-typed event streams, explicit pointer semantics for optional JSON fields, and low-garbage serialization.
- From the Python reference: Comprehensive coverage of parameters (e.g. tool choices, multi-part content, audio/modalities, response format constraints, structured outputs, logprobs, and reasoning tokens).
- Conflict Resolution: Where field representations conflict (e.g., representation of `content` as string vs array of content blocks), the canonical schema uses a normalized union representation (`ContentPart` slice) with fast-path string serialization when only a single text part is present.

## 3. Inputs & Outputs

### 3.1 HTTP Ingress (`POST /v1/chat/completions`)
- **Headers**:
  - `Authorization: Bearer <virtual_key>`
  - `Content-Type: application/json`
  - `X-Session-ID`: Optional tracking string for session binding.
  - `X-Execution-Profile`: Optional routing/latency profile override.
- **Request Body**:
  - `model` (string, required): Logical model identifier or alias.
  - `messages` ([]ChatMessage, required): Sequence of role-attributed messages (`system`, `user`, `assistant`, `tool`). Supports structured multi-part blocks (text, image URL, base64 image data).
  - `temperature` (*float32, optional): Sampling temperature [0.0, 2.0].
  - `top_p` (*float32, optional): Nucleus sampling parameter.
  - `n` (*int, optional): Number of completions.
  - `stream` (bool, default false): Whether to return Server-Sent Events.
  - `stream_options` (*StreamOptions, optional): Includes `include_usage` flag.
  - `stop` ([]string / string, optional): Stop sequences.
  - `max_tokens` / `max_completion_tokens` (*int, optional): Output token ceiling.
  - `presence_penalty`, `frequency_penalty` (*float32, optional).
  - `tools` ([]ToolDefinition, optional): JSON schema function definitions.
  - `tool_choice` (any, optional): `none`, `auto`, `required`, or explicit `{"type": "function", "function": {"name": ...}}`.
  - `response_format` (*ResponseFormat, optional): `text`, `json_object`, or `json_schema`.
  - `user` (string, optional): End-user identifier for attribution.

### 3.2 Canonical Internal Representation
```go
type CanonicalChatRequest struct {
    ID                  string
    VirtualKeyID        string
    OrgID               string
    TeamID              string
    RequestedModel      string
    EffectiveModel      string
    Messages            []CanonicalMessage
    Temperature         *float32
    TopP                *float32
    MaxOutputTokens     *int
    Stream              bool
    StreamOptions       StreamOptions
    Tools               []CanonicalTool
    ToolChoice          *CanonicalToolChoice
    ResponseFormat      *CanonicalResponseFormat
    User                string
    Metadata            map[string]string
    ReservedBudgetMicro int64
}
```

### 3.3 HTTP Egress (Synchronous & SSE)
- **Synchronous (`200 OK`, `application/json`)**: Standard OpenAI chat completion object with `id`, `object="chat.completion"`, `created`, `model`, `choices` (with `message`, `finish_reason`), `usage` (with `prompt_tokens`, `completion_tokens`, `total_tokens`, and optional breakdown for cached and reasoning tokens).
- **Streaming (`200 OK`, `text/event-stream`)**: Standard `data: {...}\n\n` frames ending with `data: [DONE]\n\n`. When `include_usage` is set, a final chunk delivers token accounting.

## 4. Lifecycle States
1. **Received**: HTTP body parsed and validated against schema.
2. **Normalized**: Request transformed into `CanonicalChatRequest`.
3. **Bound**: Virtual key, tenant budget hold, and routing metadata bound to context.
4. **Dispatched**: Handled by downstream provider adapter.
5. **Synthesized**: Upstream response transformed back into OpenAI-compatible payload.
6. **Emitted**: Written to client socket; metrics, audit record, and wallet settlement triggered.

## 5. Edge Cases
- **Null vs Unset vs Empty**: Empty string `""` in messages preserved; explicit `null` in optional parameters treated as unset defaults.
- **Thinking / Reasoning Content**: Normalizes reasoning tokens across providers (e.g. Anthropic `thinking` blocks, OpenAI `reasoning_content`) into standard `reasoning_content` field on assistant choices.
- **Legacy Parameter Aliases**: Maps `max_tokens` automatically to `max_completion_tokens` if only one is present; if both are present, `max_completion_tokens` takes precedence.

## 6. Failure Behavior
- **Malformed JSON**: Returns `400 Bad Request` with OpenAI error payload (`type: "invalid_request_error"`, `code: "invalid_json"`).
- **Missing Required Fields**: Returns `400 Bad Request` with clear identifier of the missing field.
- **Unsupported Option for Provider**: Gracefully dropped or rejected depending on strict compatibility mode configuration.

## 7. Configuration
- `server.max_request_body_bytes`: Maximum allowed payload size (default: 32MB).
- `server.read_timeout_ms`: Socket read timeout (default: 30,000ms).
- `server.strict_schema_validation`: Boolean (default: true).

## 8. Acceptance Tests
1. **Schema Round-Trip**: Decode standard OpenAI request JSON, convert to canonical struct, re-encode to JSON, and assert structural and value equality.
2. **Multi-Part Message Extraction**: Extract interleaved text and image parts without losing part indices or metadata.
3. **Tool Call Serialization**: Verify tool definitions and function arguments correctly survive canonical transformation.
4. **Header Compliance**: Assert `Content-Type: application/json` on non-streaming and `text/event-stream; charset=utf-8` on streaming endpoints.

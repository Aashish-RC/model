# Specification: Test Tooling (Mock Provider)

## 1. Purpose
The Mock Provider is a high-performance, deterministic upstream test server designed to validate gateway behavior under normal and pathological network and provider conditions. It allows unit, contract, and stress test suites to run hermetically without internet access, third-party credentials, or cost. It can dynamically inject artificial latencies, rate limits (429s with retry headers), server overloads (5xx, 529), socket timeouts, malformed SSE stream chunks, mid-stream disconnects, and slow streaming byte-by-byte pacing.

## 2. Design Strategy & Label: BIF Idea
Adopt the reference pattern of a controllable mock provider:
- Programmatic configuration per request via HTTP request headers or dynamic admin control endpoints (`/mock/configure`).
- Capability to emulate OpenAI, Anthropic, and Gemini wire protocols accurately.
- Fine-grained fault injection mechanisms specifically targeting streaming and fallback edge cases.

## 3. Architecture & Injection Controls

The mock provider inspects special injection headers on incoming requests (or falls back to default scripted behaviors):
- `X-Mock-Status`: Override HTTP response status code (e.g. `429`, `500`, `529`, `401`).
- `X-Mock-Error-Type`: Body error type (e.g. `"insufficient_quota"`, `"rate_limit"`, `"overloaded_error"`).
- `X-Mock-Delay-Ms`: Artificial latency before returning headers or first byte.
- `X-Mock-Retry-After`: Value for `Retry-After` header.
- `X-Mock-Stream-Chunk-Delay-Ms`: Delay injected between consecutive SSE chunks (slow streaming simulation).
- `X-Mock-Stream-Disconnect-After-Chunks`: Aborts the connection abruptly after $N$ chunks have been written.
- `X-Mock-Stream-Malformed-Chunk`: Emits invalid JSON or incomplete framing at chunk $N$.
- `X-Mock-Usage-Prompt`: Override prompt token count in usage response.
- `X-Mock-Usage-Completion`: Override completion token count in usage response.

## 4. Emulated Provider Dialects
The mock provider can respond in any of the 3 primary formats based on the requested URL:
1. `/v1/chat/completions`: Emulates OpenAI standard JSON or SSE chunks.
2. `/v1/messages`: Emulates Anthropic Claude event stream (`message_start`, `content_block_delta`, `message_delta`).
3. `/v1beta/models/...`: Emulates Google Gemini JSON or `alt=sse` candidates stream.

## 5. Streaming Fault Simulation

### 5.1 Mid-Stream Disconnect
```go
func (m *MockServer) handleMidStreamDisconnect(w http.ResponseWriter, r *http.Request, numChunks int) {
    flusher, _ := w.(http.Flusher)
    w.Header().Set("Content-Type", "text/event-stream")
    w.WriteHeader(http.StatusOK)
    flusher.Flush()
    
    for i := 0; i < numChunks; i++ {
        fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"chunk-%d \"}}]}\n\n", i)
        flusher.Flush()
    }
    
    // Abruptly hijack and close underlying TCP socket
    hijacker, _ := w.(http.Hijacker)
    conn, _, _ := hijacker.Hijack()
    conn.Close()
}
```

### 5.2 Malformed Chunk Injection
Emits truncated JSON (e.g. `data: {"choices":[{"delta":{"content":` with no closing braces) to verify gateway parsing resilience.

### 5.3 Slow Stream Simulation
Injects `time.Sleep(chunkDelay)` between individual SSE events to test gateway streaming buffer handling, client backpressure, and TTFT metrics.

## 6. Verification & Assertion Helper
The mock server records all incoming requests in an in-memory ring buffer:
```go
type MockCallRecord struct {
    Timestamp  time.Time
    Method     string
    Path       string
    Headers    http.Header
    Body       []byte
    APIKey     string
}
```
Test assertions query the mock server (`mock.GetCallHistory()`) to verify:
- Exact key rotation sequence (Key A called first, Key B called next).
- Request body transformations and header presence.
- Upstream cancellation reception when client aborts.

## 7. Configuration
- `mock.bind_address`: e.g. `127.0.0.1:0` (ephemeral port allocation for concurrent tests).
- `mock.default_ttft_ms`: Baseline latency (default: 5ms).

## 8. Acceptance Tests
1. **Status Code Injection**: Request with `X-Mock-Status: 429`; assert mock returns 429 with standard rate limit payload.
2. **Mid-Stream Disconnect**: Request stream with `X-Mock-Stream-Disconnect-After-Chunks: 3`; assert connection drops immediately after chunk 3.
3. **Slow Streaming**: Request 5 chunks with 50ms delay; assert total stream time is $\ge 250\text{ms}$.
4. **Call History Verification**: Assert mock server correctly tracks the credentials used across 5 sequential requests.

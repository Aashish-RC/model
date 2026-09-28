# Specification: Streaming and Server-Sent Events (SSE)

## 1. Purpose
The Streaming Engine manages real-time token delivery to clients using standard Server-Sent Events (`text/event-stream`). It delivers ultra-low time-to-first-token (TTFT) by passing chunks through with minimal buffering, maintains an in-memory accumulator to capture total token usage, detects abandoned streams when clients disconnect prematurely, handles mid-stream provider failures cleanly, and propagates cancellations upstream immediately to release provider compute and billing holds.

## 2. Design Strategy & Label: BIF
Adopt the Go reference's robust streaming architecture:
1. **Low-Overhead Passthrough**: Flush chunks to the client immediately upon receipt from upstream without waiting for complete frames or running heavy JSON decoders on the hot path.
2. **Streaming Usage Accumulator**: Maintain a lightweight rolling count of generated content chunks and parse usage blocks from providers that support `stream_options.include_usage`. If the provider does not emit streaming usage, estimate completion tokens via fast token counting.
3. **Abandoned Stream Detection & Cancel Propagation**: Detect client socket closures using `http.CloseNotifier` / `ctx.Done()`. When a client drops, immediately cancel the upstream HTTP request context to avoid burning provider tokens.
4. **Mid-Stream Failure Behavior**: If an upstream stream dies mid-generation (after headers and initial chunks have been flushed), emit an SSE error frame and close the stream cleanly. Do not attempt model fallback mid-stream.

## 3. Wire Protocol & Format
- **Content-Type**: `text/event-stream`
- **Cache-Control**: `no-cache`
- **Connection**: `keep-alive`
- **Chunk Format**:
  ```
  data: {"id":"chatcmpl-...","object":"chat.completion.chunk","created":1727500000,"model":"...","choices":[{"index":0,"delta":{"content":"token"},"finish_reason":null}]}\n\n
  ```
- **Terminal Event**:
  ```
  data: [DONE]\n\n
  ```

## 4. Usage Capture & Accumulator
```go
type StreamAccumulator struct {
    PromptTokens     int
    CompletionTokens int
    TotalTokens      int
    FirstTokenTime   time.Duration
    ContentBuilder   strings.Builder
    UsageCaptured    bool
    mu               sync.Mutex
}
```
- When a chunk containing a `usage` payload is received (e.g. OpenAI final chunk with `usage`), the accumulator marks `UsageCaptured = true` and records the exact numbers.
- If the stream completes without an explicit usage block from the provider, the accumulator counts the tokens from the accumulated `ContentBuilder` text using the fast tokenizer and records the estimated usage.
- Once the stream finishes, the accumulator triggers the wallet balance settlement and emits metrics.

## 5. Abandoned Streams & Cancellation Flow
```
[Client Drops Connection]
           │
           ▼
[ResponseWriter Detected Drop] (r.Context().Done() fires)
           │
           ▼
[Cancel Upstream Context] (aborts upstream http.Client transport)
           │
           ▼
[Stream Loop Breaks]
           │
           ▼
[Settle Wallet for Partial Tokens] (debit only tokens accumulated before cancel)
           │
           ▼
[Emit Metric: stream_abandoned_total++]
```

## 6. Mid-Stream Failure Handling
Once `200 OK` and initial data chunks have been sent to the client, HTTP headers and status codes cannot be changed. If the upstream provider disconnects, times out, or emits a 5xx error mid-stream:
1. The gateway does **not** attempt model fallback (doing so would result in garbled, concatenated multi-model text).
2. The gateway emits an SSE-formatted error chunk:
   ```
   data: {"error": {"message": "Upstream stream interrupted", "type": "upstream_error", "code": "stream_terminated"}}\n\n
   data: [DONE]\n\n
   ```
3. The socket is closed.
4. Accumulated tokens up to the failure point are debited from the tenant's wallet hold, and unspent reserved budget is refunded.

## 7. Edge Cases
- **Slow Clients (Backpressure)**: If a client reads too slowly, Go's TCP write buffer will fill. The streaming loop uses a non-blocking select with a bounded write timeout (e.g. 5 seconds) to detect stalled clients and abort them, preventing memory bloat from queued chunks.
- **Malformed Upstream Chunks**: If an upstream provider delivers malformed or partial SSE lines, the stream reader buffers incomplete lines until the next newline delimiter rather than crashing.

## 8. Configuration
- `streaming.flush_interval_ms`: Flush interval (default: 0, immediate flush).
- `streaming.write_timeout_seconds`: Client socket write deadline (default: 15s).
- `streaming.include_usage_by_default`: Automatically request usage chunks if client permits (default: true).

## 9. Acceptance Tests
1. **Time to First Token (TTFT)**: Measure latency from request start to first flushed chunk; verify overhead added by gateway is $\le 2\text{ms}$.
2. **Chunk Passthrough Integrity**: Stream 100 chunks through mock provider; verify client receives identical byte sequence and terminating `[DONE]`.
3. **Client Disconnect Abortion**: Disconnect client after 5 chunks; assert upstream mock provider receives request cancellation within 10ms.
4. **Mid-Stream Disconnect Settlement**: Verify wallet is settled for only the 5 delivered tokens, and remaining reservation is released.

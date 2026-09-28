# Specification: Fallback Chain

## 1. Purpose
The Fallback Chain provides automated resilience against catastrophic provider outages or total key exhaustion. When all available keys for a requested primary model are exhausted, rate-limited, or in quarantine cooldown, the gateway degrades gracefully to the next configured fallback model. To maintain transparent governance, any model downgrade is explicitly reported back to the caller in the response headers and metadata.

## 2. Design Strategy & Label: OWN
Follow the locked custom design:
1. **Exhaustion-Gated Trigger**: Never fall back to another model while healthy keys remain for the primary model. Fallback is activated **only** when all keys for the current provider/model are at their capacity limit or cooling off.
2. **Transparent Downgrade Reporting**: If a request was served by a fallback model rather than the primary model, the gateway injects response metadata:
   - Header `X-Model-Downgraded: true`
   - Header `X-Original-Model: <requested_model>`
   - Header `X-Effective-Model: <actual_model>`
   - Inside JSON response body: `model` reflects the actual model, and `system_fingerprint` or a custom extension field `model_downgrade` provides the downgrade chain audit.

## 3. Fallback Topology & Configuration
A fallback policy is defined as an ordered slice of target models:
```yaml
fallback_chains:
  "gpt-4o":
    - "claude-3-5-sonnet"
    - "gemini-1.5-pro"
  "claude-3-5-sonnet":
    - "gpt-4o"
    - "gemini-1.5-pro"
  "gpt-4o-mini":
    - "claude-3-5-haiku"
    - "gemini-1.5-flash"
```

## 4. Execution State Machine
```
[User Request (Model: gpt-4o)]
           │
           ▼
    [Select Key for gpt-4o]
           │
      Key Found?
      ├── YES ──> [Execute on Key] ──> Success ──> Return original model
      │                 │
      │              Failure (429/5xx)
      │                 │
      │                 ▼
      │           [Apply Cooldown] ──> Retry with another key for gpt-4o
      │
      └── NO (All Keys Exhausted / In Cooldown)
                │
                ▼
          [Check Fallback Chain]
                │
           Chain has next?
           ├── YES ──> Model = claude-3-5-sonnet (Mark Downgraded = true)
           │                 │
           │                 └──> Loop back to [Select Key for new model]
           │
           └── NO  ──> Return 503 Service Unavailable ("all fallback models exhausted")
```

## 5. Streaming Fallback Behavior
- **Pre-Token Failure**: If the primary model fails or is exhausted before any SSE chunk has been flushed to the client socket, the gateway silently pivots to the fallback model and establishes the new stream.
- **Mid-Stream Failure**: Once a single token/chunk has been written to the client HTTP response stream, the connection protocol cannot be rewound. In this case, fallback is **impossible**; the stream emits an error frame and terminates (see Streaming Specification).

## 6. Response Metadata Injection

### HTTP Headers:
```http
HTTP/1.1 200 OK
Content-Type: application/json
X-Original-Model: gpt-4o
X-Effective-Model: claude-3-5-sonnet
X-Model-Downgraded: true
X-Fallback-Reason: primary_provider_rate_limited
```

### JSON Response Body:
```json
{
  "id": "chatcmpl-9x...",
  "object": "chat.completion",
  "created": 1727500000,
  "model": "claude-3-5-sonnet",
  "choices": [...],
  "usage": {...},
  "metadata": {
    "requested_model": "gpt-4o",
    "effective_model": "claude-3-5-sonnet",
    "downgraded": true
  }
}
```

## 7. Edge Cases
- **Circular Fallbacks**: Fallback chains are checked for cycles at configuration load time. Runtime execution maintains a visited set (`map[string]bool`) per request to prevent infinite loops.
- **Incompatible Feature Sets**: If the requested model used a feature not supported by the fallback model (e.g. specialized response schema or temperature range), the router verifies adapter feature flags before choosing that fallback step.

## 8. Acceptance Tests
1. **Single Key Exhaustion No Fallback**: Key 1 of gpt-4o fails; Key 2 is active. Assert gpt-4o is served by Key 2 without triggering fallback.
2. **Total Exhaustion Fallback**: Mark all gpt-4o keys in cooldown; assert request automatically routes to claude-3-5-sonnet.
3. **Downgrade Metadata Verification**: Verify `X-Model-Downgraded: true` and `X-Effective-Model` headers are present on fallback execution.
4. **All Models Exhausted**: Mark keys for all models in chain in cooldown; assert immediate `503` with structured error details.

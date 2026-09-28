# Specification: Metrics and Observability

## 1. Purpose
The Metrics and Observability subsystem delivers comprehensive operational telemetry across the data plane. It exposes Prometheus metrics at `/metrics` and emits distributed OpenTelemetry spans for end-to-end tracing. Telemetry captures fine-grained latency breakdowns (internal gateway overhead vs upstream TTFT vs stream completion), token generation throughput (tokens/sec), key rotations, cooldown triggers, and fallback activations.

## 2. Design Strategy & Label: BIF
Adopt the Go reference's high-efficiency instrumentation:
- Zero allocations on the metric recording path.
- Standardized Prometheus metric naming following Cloud-Native / GenAI semantic conventions.
- Explicit span hierarchy: `gateway.request` -> `router.select_key` -> `provider.execute` -> `stream.transfer`.
- Separate measurement of:
  - **Gateway Overhead Latency**: Time spent in auth, schema translation, rate limiting, and routing before bytes hit the wire.
  - **Upstream Time to First Token (TTFT)**: Time until the first token/byte arrives from the upstream provider.
  - **Stream Generation Latency**: Time spent reading subsequent tokens until completion.

## 3. Prometheus Metrics Catalog

### 3.1 Request Counters & Gauges
- `gateway_requests_total{model, provider, status_code, org_id}`: Total HTTP requests received.
- `gateway_in_flight_requests{model, provider}`: Current active concurrent requests.
- `gateway_errors_total{model, provider, error_type, failure_class}`: Classified error count.

### 3.2 Latency Histograms
- `gateway_request_duration_seconds{model, provider, is_stream}`: End-to-end request duration. Buckets: `[0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0]`.
- `gateway_upstream_ttft_seconds{model, provider}`: Time to first token for streaming requests. Buckets: `[0.05, 0.1, 0.2, 0.5, 1.0, 2.0, 5.0]`.
- `gateway_internal_overhead_seconds{stage}`: Gateway processing overhead. Buckets: `[0.0005, 0.001, 0.002, 0.005, 0.01, 0.05]`.

### 3.3 Token & Throughput Metrics
- `gateway_tokens_total{model, provider, token_type}`: Counter for `prompt`, `completion`, `cached`, `reasoning`.
- `gateway_tokens_per_second{model, provider}`: Histogram of token emission throughput during active streaming generation.

### 3.4 Key Pool, Cooldown & Fallback Metrics
- `gateway_key_rotations_total{provider, reason}`: Number of times key was rotated due to rate limits or errors.
- `gateway_key_cooldowns_total{provider, key_id}`: Number of times keys entered quarantine.
- `gateway_key_headroom_ratio{provider, key_id}`: Gauge of remaining key headroom [0.0 - 1.0].
- `gateway_model_fallbacks_total{original_model, fallback_model, reason}`: Counter for model fallback activations.
- `gateway_streams_abandoned_total{model, provider}`: Counter for client disconnects mid-stream.

## 4. OpenTelemetry Tracing

### Span Attributes:
- `gen_ai.system`: e.g. `"openai"`, `"anthropic"`, `"gemini"`
- `gen_ai.request.model`: Requested model name
- `gen_ai.response.model`: Effective model name (after fallback)
- `gen_ai.usage.prompt_tokens`: Count
- `gen_ai.usage.completion_tokens`: Count
- `gateway.org_id`: Tenant ID
- `gateway.virtual_key_id`: Virtual key ID
- `gateway.downgraded`: Boolean flag
- `gateway.key_id`: Upstream key utilized

## 5. Endpoints
- `GET /metrics`: Standard Prometheus text exposition format (version 0.0.4).
- `GET /health` / `GET /healthz`: Liveness probe (`200 OK`, `{"status":"healthy"}`).
- `GET /ready`: Readiness probe verifying database and cache connectivity.

## 6. Edge Cases
- **Metric Cardinality Protection**: Dynamic user IDs and session IDs are attached as OpenTelemetry span attributes, **never** as high-cardinality Prometheus metric labels. Label sets are strictly bounded to model, provider, status code, and org.
- **Microsecond Precision**: High-precision monotonic timers (`time.Now()`) are used for internal overhead measurements to avoid wall-clock shifts.

## 7. Configuration
- `telemetry.prometheus.enabled`: Boolean (default: true).
- `telemetry.prometheus.path`: Default `/metrics`.
- `telemetry.otel.enabled`: Boolean (default: false).
- `telemetry.otel.endpoint`: OTLP gRPC/HTTP collector endpoint.
- `telemetry.otel.sample_ratio`: Float64 (default: 1.0).

## 8. Acceptance Tests
1. **Histogram Recording**: Execute request; scrape `/metrics`; assert `gateway_request_duration_seconds_count` and `gateway_tokens_total` increment.
2. **Fallback Metric Emission**: Trigger fallback from `gpt-4o` to `claude-3-5-sonnet`; assert `gateway_model_fallbacks_total{original_model="gpt-4o",fallback_model="claude-3-5-sonnet"}` increments by 1.
3. **TTFT Measurement**: Stream response; assert `gateway_upstream_ttft_seconds` records exact duration until first chunk.
4. **Key Rotation Count**: Simulate 429; assert `gateway_key_rotations_total` increments.

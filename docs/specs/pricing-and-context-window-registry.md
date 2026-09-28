# Specification: Pricing and Context-Window Registry

## 1. Purpose
The Pricing and Context-Window Registry provides a fast, thread-safe, in-memory catalog of token costs (input, output, cached input, reasoning tokens) and context boundaries (max input tokens, max output tokens) for every supported model. It enables accurate budget reservations prior to provider dispatch, real-time cost calculation upon completion, and context-window safety validation during request ingestion.

## 2. Design Strategy & Label: BOTH
- Adopt the comprehensive model price database structure from the reference catalog (covering prompt cost per token, completion cost per token, cache read/write cost, max tokens).
- Adopt the Go reference's high-speed in-memory lookup table (`sync.RWMutex` / atomic pointer map) with zero runtime JSON deserialization on the hot path.
- Conflict Resolution: When model aliases exist across providers (e.g. `gpt-4o` vs `openai/gpt-4o` vs `azure/gpt-4o`), normalize names to a canonical model key while allowing provider-specific price overrides.

## 3. Data Model & Pricing Entry
```go
type ModelPricing struct {
    ModelName             string   `json:"model_name"`
    Provider              string   `json:"provider"`
    MaxContextTokens      int      `json:"max_context_tokens"`
    MaxOutputTokens       int      `json:"max_output_tokens"`
    InputCostPerToken     float64  `json:"input_cost_per_token"`     // In USD
    OutputCostPerToken    float64  `json:"output_cost_per_token"`    // In USD
    CacheReadCostPerToken float64  `json:"cache_read_cost_per_token"`// In USD
    CacheWriteCostPerToken float64 `json:"cache_write_cost_per_token"`
    SupportsStreaming     bool     `json:"supports_streaming"`
    SupportsToolCalling   bool     `json:"supports_tool_calling"`
    SupportsVision        bool     `json:"supports_vision"`
}
```

## 4. Cost Computation Formula
$$\text{CostUSD} = (N_{\text{input}} \times C_{\text{input}}) + (N_{\text{output}} \times C_{\text{output}}) + (N_{\text{cached}} \times C_{\text{cache\_read}})$$
To prevent floating-point rounding issues in financial ledgers, costs are internally converted to integer micro-dollars ($\mu\text{USD}$, where $\$1.00 = 1,000,000\ \mu\text{USD}$):
$$\text{CostMicroUSD} = \text{round}(\text{CostUSD} \times 1,000,000)$$

## 5. Storage & Dynamic Update Lifecycle
1. **Bootstrapping**: Initialized from embedded JSON / config file (`models.json`) during gateway startup.
2. **In-Memory Cache**: Stored in a lock-free read map (`atomic.Pointer[map[string]ModelPricing]`) providing sub-microsecond lookups during request routing.
3. **Hot Reloading**: An admin API endpoint (`POST /v1/admin/pricing/reload`) or background polling worker can reload the registry without restarting the gateway process.

## 6. Edge Cases
- **Unknown Models**: If a client invokes an unregistered model (e.g. custom fine-tuned model or private endpoint), the registry applies configurable default pricing (or logs a warning and marks cost as 0 with an `untracked_cost` flag).
- **Wildcard / Prefix Matching**: Supports prefix matching (e.g. `ft:gpt-4o:*` matches `gpt-4o` base rate unless explicit fine-tune pricing is configured).
- **Zero Cost Models**: Local models (Ollama, vLLM) have costs explicitly recorded as 0.0, bypassing billing debit while still updating token usage metrics.

## 7. Configuration
- `pricing.catalog_path`: Path to static model pricing JSON (default: `config/models.json`).
- `pricing.allow_unknown_models`: Boolean (default: true).
- `pricing.default_input_cost_per_token`: Fallback rate if unknown (default: 0.000005).
- `pricing.default_output_cost_per_token`: Fallback rate if unknown (default: 0.000015).

## 8. Acceptance Tests
1. **Exact Lookup**: Retrieve pricing for `gpt-4o`, `claude-3-5-sonnet`, `gemini-1.5-pro`; verify input/output costs match expected baseline values.
2. **Cost Computation Accuracy**: Pass 1,000 input tokens and 500 output tokens for a known model; assert computed micro-dollar cost equals exact mathematical formula.
3. **Context Limit Exceeded Check**: Validate a request with 200,000 input tokens against a model with 128,000 limit; assert validation error is produced before dispatch.
4. **Hot Reload Consistency**: Update pricing table in-memory; assert subsequent requests immediately pick up revised rates with zero dropped requests.

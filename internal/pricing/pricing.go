package pricing

import (
	"math"
	"strings"
	"sync"
	"sync/atomic"
)

// ModelPricing defines costs and context boundaries for a model.
type ModelPricing struct {
	ModelName          string  `json:"model_name"`
	Provider           string  `json:"provider"`
	MaxContextTokens   int     `json:"max_context_tokens"`
	MaxOutputTokens    int     `json:"max_output_tokens"`
	InputCostPerToken  float64 `json:"input_cost_per_token"`
	OutputCostPerToken float64 `json:"output_cost_per_token"`
	CacheReadCostPerToken float64 `json:"cache_read_cost_per_token"`
}

// Registry manages thread-safe model pricing lookups.
type Registry struct {
	mu     sync.RWMutex
	models atomic.Pointer[map[string]ModelPricing]
}

// NewRegistry initializes a registry pre-populated with standard baseline model prices.
func NewRegistry() *Registry {
	r := &Registry{}
	initial := make(map[string]ModelPricing)

	// Pre-populate core baseline prices
	initial["gpt-4o"] = ModelPricing{
		ModelName:          "gpt-4o",
		Provider:           "openai",
		MaxContextTokens:   128000,
		MaxOutputTokens:    16384,
		InputCostPerToken:  0.000005,  // $5 / 1M tokens
		OutputCostPerToken: 0.000015, // $15 / 1M tokens
	}
	initial["gpt-4o-mini"] = ModelPricing{
		ModelName:          "gpt-4o-mini",
		Provider:           "openai",
		MaxContextTokens:   128000,
		MaxOutputTokens:    16384,
		InputCostPerToken:  0.00000015, // $0.15 / 1M tokens
		OutputCostPerToken: 0.0000006,  // $0.60 / 1M tokens
	}
	initial["claude-3-5-sonnet"] = ModelPricing{
		ModelName:          "claude-3-5-sonnet",
		Provider:           "anthropic",
		MaxContextTokens:   200000,
		MaxOutputTokens:    8192,
		InputCostPerToken:  0.000003, // $3 / 1M tokens
		OutputCostPerToken: 0.000015, // $15 / 1M tokens
	}
	initial["gemini-1.5-pro"] = ModelPricing{
		ModelName:          "gemini-1.5-pro",
		Provider:           "gemini",
		MaxContextTokens:   2000000,
		MaxOutputTokens:    8192,
		InputCostPerToken:  0.0000035,
		OutputCostPerToken: 0.0000105,
	}
	initial["gemini-1.5-flash"] = ModelPricing{
		ModelName:          "gemini-1.5-flash",
		Provider:           "gemini",
		MaxContextTokens:   1000000,
		MaxOutputTokens:    8192,
		InputCostPerToken:  0.00000035,
		OutputCostPerToken: 0.00000105,
	}

	r.models.Store(&initial)
	return r
}

// GetPricing retrieves pricing entry for a model.
func (r *Registry) GetPricing(model string) (ModelPricing, bool) {
	current := r.models.Load()
	if current == nil {
		return ModelPricing{}, false
	}

	m, exists := (*current)[model]
	if exists {
		return m, true
	}

	// Try normalized lookup (lowercase or stripping prefixes)
	lower := strings.ToLower(model)
	if m, exists := (*current)[lower]; exists {
		return m, true
	}

	// Check for local prefix -> free
	if strings.HasPrefix(lower, "local/") || strings.HasPrefix(lower, "vllm/") || strings.HasPrefix(lower, "ollama/") {
		return ModelPricing{
			ModelName:          model,
			Provider:           "generic",
			MaxContextTokens:   32768,
			MaxOutputTokens:    4096,
			InputCostPerToken:  0.0,
			OutputCostPerToken: 0.0,
		}, true
	}

	// Fallback baseline for unknown models
	return ModelPricing{
		ModelName:          model,
		Provider:           "unknown",
		MaxContextTokens:   128000,
		MaxOutputTokens:    4096,
		InputCostPerToken:  0.000005,
		OutputCostPerToken: 0.000015,
	}, false
}

// CalculateCostMicro computes integer cost in micro-dollars (1 USD = 1,000,000 micro-dollars).
func (r *Registry) CalculateCostMicro(model string, inputTokens, outputTokens, cachedTokens int) int64 {
	p, _ := r.GetPricing(model)

	costUSD := (float64(inputTokens) * p.InputCostPerToken) +
		(float64(outputTokens) * p.OutputCostPerToken) +
		(float64(cachedTokens) * p.CacheReadCostPerToken)

	return int64(math.Round(costUSD * 1000000.0))
}

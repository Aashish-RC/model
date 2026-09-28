package fallback

import (
	"errors"
	"sync"

	"github.com/ra1/model-gateway/internal/schema"
)

var (
	ErrFallbackChainExhausted = errors.New("all models in the fallback chain have been exhausted")
)

// Policy manages model fallback chains.
type Policy struct {
	mu     sync.RWMutex
	chains map[string][]string // Primary Model -> []FallbackModel
}

// NewPolicy creates an empty fallback policy.
func NewPolicy() *Policy {
	return &Policy{
		chains: make(map[string][]string),
	}
}

// SetChain registers a fallback chain for a primary model.
func (p *Policy) SetChain(primaryModel string, fallbackModels []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.chains[primaryModel] = append([]string(nil), fallbackModels...)
}

// GetNextModel returns the next eligible model in the fallback chain that has not been visited yet.
func (p *Policy) GetNextModel(primaryModel string, visited map[string]bool) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	chain, exists := p.chains[primaryModel]
	if !exists || len(chain) == 0 {
		return "", ErrFallbackChainExhausted
	}

	for _, candidate := range chain {
		if !visited[candidate] {
			return candidate, nil
		}
	}

	return "", ErrFallbackChainExhausted
}

// AnnotateDowngrade injects downgrade metadata into response headers and metadata dictionary.
func AnnotateDowngrade(resp *schema.CanonicalChatResponse, requestedModel, effectiveModel string) {
	if resp == nil {
		return
	}
	if resp.Metadata == nil {
		resp.Metadata = make(map[string]string)
	}

	resp.Model = effectiveModel
	if requestedModel != effectiveModel {
		resp.Metadata["requested_model"] = requestedModel
		resp.Metadata["effective_model"] = effectiveModel
		resp.Metadata["downgraded"] = "true"
	}
}

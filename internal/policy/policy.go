package policy

import (
	"context"
	"fmt"
	"net"
	"path"
	"strings"
	"sync"
)

// ViolationCode identifies the type of policy violation.
type ViolationCode string

const (
	CodeForbiddenModel        ViolationCode = "forbidden_model"
	CodeExcessiveMaxTokens    ViolationCode = "excessive_max_tokens"
	CodeExcessiveInputTokens  ViolationCode = "excessive_input_tokens"
	CodeIPForbidden           ViolationCode = "ip_forbidden"
	CodeMissingRequiredHeader ViolationCode = "missing_required_header"
)

// PolicyViolationError represents an enforcement failure.
type PolicyViolationError struct {
	Message string        `json:"message"`
	Type    string        `json:"type"`
	Code    ViolationCode `json:"code"`
}

func (e *PolicyViolationError) Error() string {
	return fmt.Sprintf("policy violation (%s): %s", e.Code, e.Message)
}

// Rules defines governance constraints on incoming requests.
type Rules struct {
	AllowedModels   []string `json:"allowed_models,omitempty"`
	MaxTokensLimit  int      `json:"max_tokens_limit,omitempty"`
	MaxInputTokens  int      `json:"max_input_tokens,omitempty"`
	IPAllowlist     []string `json:"ip_allowlist,omitempty"`
	RequiredHeaders []string `json:"required_headers,omitempty"`
}

// Policy is an organizational rule set targeting a specific scope.
type Policy struct {
	ID    string `json:"id"`
	Scope string `json:"scope"` // e.g. "team:interns", "org:marketing", "key:sk-prod-*", or "*" for global
	Rules Rules  `json:"rules"`
}

// RequestContext encapsulates request attributes evaluated against active policies.
type RequestContext struct {
	ClientIP             string            `json:"client_ip"`
	Team                 string            `json:"team,omitempty"`
	Org                  string            `json:"org,omitempty"`
	KeyPrefix            string            `json:"key_prefix,omitempty"`
	Model                string            `json:"model"`
	MaxTokens            int               `json:"max_tokens"`
	EstimatedInputTokens int               `json:"estimated_input_tokens"`
	Headers              map[string]string `json:"headers,omitempty"`
}

// Engine evaluates requests against active organizational policies.
type Engine struct {
	mu       sync.RWMutex
	policies map[string]Policy
}

// NewEngine creates a new policy evaluation engine.
func NewEngine() *Engine {
	return &Engine{
		policies: make(map[string]Policy),
	}
}

// RegisterPolicy adds or updates a policy in the engine.
func (e *Engine) RegisterPolicy(p Policy) error {
	if p.ID == "" {
		return fmt.Errorf("policy id cannot be empty")
	}

	// Validate IP CIDRs if provided
	for _, cidr := range p.Rules.IPAllowlist {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			// Also check if it's a bare IP, which can be /32 or /128
			if ip := net.ParseIP(cidr); ip == nil {
				return fmt.Errorf("invalid CIDR or IP in allowlist: %s: %w", cidr, err)
			}
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.policies[p.ID] = p
	return nil
}

// RemovePolicy removes a policy by ID.
func (e *Engine) RemovePolicy(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.policies, id)
}

// GetPolicy retrieves a policy by ID.
func (e *Engine) GetPolicy(id string) (Policy, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	p, ok := e.policies[id]
	return p, ok
}

// ListPolicies returns all configured policies.
func (e *Engine) ListPolicies() []Policy {
	e.mu.RLock()
	defer e.mu.RUnlock()
	list := make([]Policy, 0, len(e.policies))
	for _, p := range e.policies {
		list = append(list, p)
	}
	return list
}

// Evaluate tests a request context against all matching policies.
// If any matching policy rejects the request, an error is returned.
func (e *Engine) Evaluate(ctx context.Context, req RequestContext) error {
	e.mu.RLock()
	policies := make([]Policy, 0, len(e.policies))
	for _, p := range e.policies {
		policies = append(policies, p)
	}
	e.mu.RUnlock()

	for _, p := range policies {
		if !e.scopeMatches(p.Scope, req) {
			continue
		}

		if err := e.enforceRules(p, req); err != nil {
			return err
		}
	}

	return nil
}

func (e *Engine) scopeMatches(scope string, req RequestContext) bool {
	if scope == "" || scope == "*" {
		return true
	}

	parts := strings.SplitN(scope, ":", 2)
	if len(parts) == 1 {
		// Bare match against team or org
		return req.Team == scope || req.Org == scope
	}

	scopeType := strings.ToLower(parts[0])
	target := parts[1]

	switch scopeType {
	case "team":
		matched, _ := path.Match(target, req.Team)
		return matched || req.Team == target
	case "org":
		matched, _ := path.Match(target, req.Org)
		return matched || req.Org == target
	case "key":
		matched, _ := path.Match(target, req.KeyPrefix)
		return matched || strings.HasPrefix(req.KeyPrefix, target)
	default:
		return false
	}
}

func (e *Engine) enforceRules(p Policy, req RequestContext) error {
	rules := p.Rules

	// 1. Model Allowlist Check
	if len(rules.AllowedModels) > 0 {
		allowed := false
		for _, pattern := range rules.AllowedModels {
			if matched, _ := path.Match(pattern, req.Model); matched || req.Model == pattern {
				allowed = true
				break
			}
		}
		if !allowed {
			return &PolicyViolationError{
				Message: fmt.Sprintf("requested model '%s' not allowed for scope '%s'", req.Model, p.Scope),
				Type:    "policy_violation",
				Code:    CodeForbiddenModel,
			}
		}
	}

	// 2. Max Tokens Cap Check
	if rules.MaxTokensLimit > 0 && req.MaxTokens > rules.MaxTokensLimit {
		return &PolicyViolationError{
			Message: fmt.Sprintf("requested max_tokens %d exceeds policy limit of %d", req.MaxTokens, rules.MaxTokensLimit),
			Type:    "policy_violation",
			Code:    CodeExcessiveMaxTokens,
		}
	}

	// 3. Max Input Tokens Cap Check
	if rules.MaxInputTokens > 0 && req.EstimatedInputTokens > rules.MaxInputTokens {
		return &PolicyViolationError{
			Message: fmt.Sprintf("estimated input tokens %d exceeds policy limit of %d", req.EstimatedInputTokens, rules.MaxInputTokens),
			Type:    "policy_violation",
			Code:    CodeExcessiveInputTokens,
		}
	}

	// 4. IP Allowlist Check
	if len(rules.IPAllowlist) > 0 && req.ClientIP != "" {
		clientIPStr := req.ClientIP
		if host, _, err := net.SplitHostPort(clientIPStr); err == nil {
			clientIPStr = host
		}
		clientIP := net.ParseIP(clientIPStr)
		if clientIP == nil {
			return &PolicyViolationError{
				Message: fmt.Sprintf("invalid client IP address: %s", req.ClientIP),
				Type:    "policy_violation",
				Code:    CodeIPForbidden,
			}
		}

		inAllowlist := false
		for _, cidr := range rules.IPAllowlist {
			if strings.Contains(cidr, "/") {
				_, ipNet, err := net.ParseCIDR(cidr)
				if err == nil && ipNet.Contains(clientIP) {
					inAllowlist = true
					break
				}
			} else {
				if parsed := net.ParseIP(cidr); parsed != nil && parsed.Equal(clientIP) {
					inAllowlist = true
					break
				}
			}
		}

		if !inAllowlist {
			return &PolicyViolationError{
				Message: fmt.Sprintf("client IP '%s' is not authorized in scope '%s'", req.ClientIP, p.Scope),
				Type:    "policy_violation",
				Code:    CodeIPForbidden,
			}
		}
	}

	// 5. Required Headers Check
	if len(rules.RequiredHeaders) > 0 {
		for _, reqHdr := range rules.RequiredHeaders {
			found := false
			for hName, hVal := range req.Headers {
				if strings.EqualFold(hName, reqHdr) && strings.TrimSpace(hVal) != "" {
					found = true
					break
				}
			}
			if !found {
				return &PolicyViolationError{
					Message: fmt.Sprintf("missing required header '%s'", reqHdr),
					Type:    "policy_violation",
					Code:    CodeMissingRequiredHeader,
				}
			}
		}
	}

	return nil
}

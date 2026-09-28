package rules

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"cel.dev/cel-go/cel"
)

// Actions defines what modifications are applied when a rule matches.
type Actions struct {
	SetModel       string            `json:"set_model,omitempty"`
	ForceProvider  string            `json:"force_provider,omitempty"`
	SetHeaders     map[string]string `json:"set_headers,omitempty"`
	Priority       int               `json:"priority,omitempty"`
	Terminal       bool              `json:"terminal,omitempty"`
}

// Rule defines an individual policy or routing rule.
type Rule struct {
	ID          string  `json:"id"`
	Description string  `json:"description,omitempty"`
	Condition   string  `json:"condition"`
	Actions     Actions `json:"actions"`
}

// CompiledRule contains the pre-compiled executable CEL program.
type CompiledRule struct {
	Rule    *Rule
	Program cel.Program
}

// EvaluationContext contains the data visible to CEL expressions.
type EvaluationContext struct {
	Model           string            `json:"model"`
	Stream          bool              `json:"stream"`
	EstimatedTokens int               `json:"estimated_tokens"`
	Headers         map[string]string `json:"headers"`
	User            string            `json:"user"`
	KeyID           string            `json:"key_id"`
	OrgID           string            `json:"org_id"`
	TeamID          string            `json:"team_id"`
}

// Result holds the aggregated outcome after evaluating rules.
type Result struct {
	MatchedRules    []string
	RewrittenModel  string
	ForcedProvider  string
	InjectedHeaders map[string]string
}

// Engine compiles and evaluates CEL rules against incoming requests.
type Engine struct {
	mu            sync.RWMutex
	compiledRules []*CompiledRule
	env           *cel.Env
}

// NewEngine initializes and pre-compiles the set of CEL rules.
func NewEngine(rules []*Rule) (*Engine, error) {
	// Setup CEL environment with typed declarations
	env, err := cel.NewEnv(
		cel.Variable("request", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("key", cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL env: %w", err)
	}

	e := &Engine{
		env:           env,
		compiledRules: make([]*CompiledRule, 0, len(rules)),
	}

	if err := e.compileRules(rules); err != nil {
		return nil, err
	}

	return e, nil
}

func (e *Engine) compileRules(rules []*Rule) error {
	// Sort rules by Priority descending
	sorted := make([]*Rule, len(rules))
	copy(sorted, rules)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Actions.Priority > sorted[j].Actions.Priority
	})

	compiled := make([]*CompiledRule, 0, len(sorted))
	for _, r := range sorted {
		ast, issues := e.env.Compile(r.Condition)
		if issues != nil && issues.Err() != nil {
			return fmt.Errorf("failed to compile rule '%s' condition '%s': %w", r.ID, r.Condition, issues.Err())
		}

		prg, err := e.env.Program(ast)
		if err != nil {
			return fmt.Errorf("failed to build CEL program for rule '%s': %w", r.ID, err)
		}

		compiled = append(compiled, &CompiledRule{
			Rule:    r,
			Program: prg,
		})
	}

	e.compiledRules = compiled
	return nil
}

// UpdateRules hot-reloads the rule set atomically.
func (e *Engine) UpdateRules(rules []*Rule) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.compileRules(rules)
}

// Evaluate runs the compiled rules against the evaluation context.
func (e *Engine) Evaluate(ctx context.Context, evalCtx *EvaluationContext) (*Result, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	headers := evalCtx.Headers
	if headers == nil {
		headers = make(map[string]string)
	}

	reqMap := map[string]any{
		"model":            evalCtx.Model,
		"stream":           evalCtx.Stream,
		"estimated_tokens": int64(evalCtx.EstimatedTokens),
		"headers":          headers,
		"user":             evalCtx.User,
	}

	keyMap := map[string]any{
		"id":      evalCtx.KeyID,
		"org_id":  evalCtx.OrgID,
		"team_id": evalCtx.TeamID,
	}

	input := map[string]any{
		"request": reqMap,
		"key":     keyMap,
	}

	res := &Result{
		MatchedRules:    make([]string, 0),
		InjectedHeaders: make(map[string]string),
	}

	for _, cr := range e.compiledRules {
		out, _, err := cr.Program.Eval(input)
		if err != nil {
			// Skip rules that error during eval due to missing fields
			continue
		}

		matched, _ := out.Value().(bool)

		if matched {
			res.MatchedRules = append(res.MatchedRules, cr.Rule.ID)

			if cr.Rule.Actions.SetModel != "" {
				res.RewrittenModel = cr.Rule.Actions.SetModel
			}
			if cr.Rule.Actions.ForceProvider != "" {
				res.ForcedProvider = cr.Rule.Actions.ForceProvider
			}
			for k, v := range cr.Rule.Actions.SetHeaders {
				res.InjectedHeaders[k] = v
			}

			if cr.Rule.Actions.Terminal {
				break
			}
		}
	}

	return res, nil
}

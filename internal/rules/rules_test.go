package rules

import (
	"context"
	"testing"
)

func TestCEL_HeaderCondition_MatchesAndRewritesModel(t *testing.T) {
	ruleList := []*Rule{
		{
			ID:          "finance-override",
			Description: "Rewrite gpt-4o queries to gpt-4o-mini for finance department",
			Condition:   `request.headers["x-department"] == "finance" && request.model == "gpt-4o"`,
			Actions: Actions{
				SetModel:      "gpt-4o-mini",
				ForceProvider: "openai",
				SetHeaders: map[string]string{
					"X-Routed-By": "finance-cel-rule",
				},
				Priority: 10,
				Terminal: true,
			},
		},
	}

	engine, err := NewEngine(ruleList)
	if err != nil {
		t.Fatalf("failed to build engine: %v", err)
	}

	evalCtx := &EvaluationContext{
		Model: "gpt-4o",
		Headers: map[string]string{
			"x-department": "finance",
		},
		OrgID: "org-finance",
	}

	res, err := engine.Evaluate(context.Background(), evalCtx)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if len(res.MatchedRules) != 1 || res.MatchedRules[0] != "finance-override" {
		t.Errorf("expected finance-override to match, got %v", res.MatchedRules)
	}
	if res.RewrittenModel != "gpt-4o-mini" {
		t.Errorf("expected rewritten model gpt-4o-mini, got %s", res.RewrittenModel)
	}
	if res.ForcedProvider != "openai" {
		t.Errorf("expected forced provider openai, got %s", res.ForcedProvider)
	}
	if res.InjectedHeaders["X-Routed-By"] != "finance-cel-rule" {
		t.Errorf("expected injected header, got %v", res.InjectedHeaders)
	}
}

func TestCEL_TokenThresholdCondition_AppliesRoutingAction(t *testing.T) {
	ruleList := []*Rule{
		{
			ID:        "large-context-redirect",
			Condition: `request.estimated_tokens > 10000`,
			Actions: Actions{
				SetModel: "gemini-1.5-pro",
				Priority: 5,
			},
		},
	}

	engine, err := NewEngine(ruleList)
	if err != nil {
		t.Fatalf("failed to build engine: %v", err)
	}

	evalCtx := &EvaluationContext{
		Model:           "gpt-4o",
		EstimatedTokens: 15000,
	}

	res, err := engine.Evaluate(context.Background(), evalCtx)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if len(res.MatchedRules) != 1 || res.RewrittenModel != "gemini-1.5-pro" {
		t.Errorf("expected large-context-redirect match, got %v", res)
	}
}

func TestCEL_SafeAttributeAccess_MissingHeaderDoesNotPanic(t *testing.T) {
	ruleList := []*Rule{
		{
			ID:        "check-header",
			Condition: `request.headers["non-existent"] == "value"`,
			Actions: Actions{
				SetModel: "override",
			},
		},
	}

	engine, err := NewEngine(ruleList)
	if err != nil {
		t.Fatalf("failed to build engine: %v", err)
	}

	evalCtx := &EvaluationContext{
		Model:   "gpt-4o",
		Headers: map[string]string{},
	}

	res, err := engine.Evaluate(context.Background(), evalCtx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if len(res.MatchedRules) != 0 {
		t.Errorf("expected no match, got %v", res.MatchedRules)
	}
}

func TestCEL_InvalidRule_RejectedAtCompileTime(t *testing.T) {
	ruleList := []*Rule{
		{
			ID:        "bad-syntax",
			Condition: `request.model == == "broken"`,
		},
	}

	_, err := NewEngine(ruleList)
	if err == nil {
		t.Fatalf("expected compilation error for broken CEL syntax")
	}
}

func TestCEL_PriorityOrdering(t *testing.T) {
	ruleList := []*Rule{
		{
			ID:        "low-prio",
			Condition: `request.model == "gpt-4o"`,
			Actions: Actions{
				SetModel: "low-model",
				Priority: 1,
			},
		},
		{
			ID:        "high-prio",
			Condition: `request.model == "gpt-4o"`,
			Actions: Actions{
				SetModel: "high-model",
				Priority: 100,
				Terminal: true,
			},
		},
	}

	engine, err := NewEngine(ruleList)
	if err != nil {
		t.Fatalf("failed to build engine: %v", err)
	}

	evalCtx := &EvaluationContext{
		Model: "gpt-4o",
	}

	res, err := engine.Evaluate(context.Background(), evalCtx)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	// High priority terminal rule should run first and terminate
	if len(res.MatchedRules) != 1 || res.MatchedRules[0] != "high-prio" {
		t.Errorf("expected high-prio to execute first and terminate, got %v", res.MatchedRules)
	}
	if res.RewrittenModel != "high-model" {
		t.Errorf("expected high-model, got %s", res.RewrittenModel)
	}
}

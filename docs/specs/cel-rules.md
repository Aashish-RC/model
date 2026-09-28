# Specification: CEL (Common Expression Language) Rules Engine

**Status:** Draft (Wave 2)  
**Layer:** Request Policy & Routing Pre-Processor (`internal/rules`)  
**Design Reference Mode:** `OWN` (Clean-Room standard Google CEL specification)  

---

## 1. Purpose & Overview

The CEL Rules Engine allows administrators to express declarative, high-performance routing, tagging, model rewriting, and policy enforcement rules using Google's Common Expression Language (CEL).

Instead of hardcoding enterprise routing logic into Go code, administrators can define dynamic rules that inspect:
- Request headers (e.g. `request.headers["x-department"] == "finance"`)
- Virtual key metadata (e.g. `key.org_id == "org-enterprise"`)
- Model name requested (e.g. `request.model.startsWith("claude")`)
- Prompt attributes (e.g. `request.prompt_tokens > 8000`)
- Caller user identity (e.g. `request.user.matches("^prod-.*")`)

---

## 2. Rule Structure & Execution Flow

### 2.1 Rule Definition
```yaml
id: "finance-deepseek-override"
description: "Route internal finance queries with high tokens to cost-effective models"
condition: 'request.headers["x-department"] == "finance" && request.estimated_tokens > 4000'
actions:
  set_model: "gpt-4o-mini"
  set_header:
    "x-routed-by": "finance-cel-rule"
  force_provider: "openai"
  priority: 100
```

### 2.2 Variables Exposed to CEL Environment
- `request.model`: string
- `request.stream`: bool
- `request.estimated_tokens`: int
- `request.headers`: map[string]string
- `request.user`: string
- `key.id`: string
- `key.org_id`: string
- `key.team_id`: string
- `key.spend_micro`: int

### 2.3 Evaluation Pipeline
1. Incoming canonical request arrives at the server middleware.
2. CEL expressions are compiled at startup/config load (never re-parsed on the hot path).
3. Expressions are evaluated in order of descending `priority`.
4. If a condition evaluates to `true`:
   - Configured actions are applied (model rewrite, tag injection, target constraint).
   - If `terminal: true` is set, evaluation stops; otherwise continues to subsequent rules.

---

## 3. Edge Cases & Safety Constraints

1. **Compilation Guard:** Syntax errors in CEL rules are caught at configuration parse time; the gateway rejects invalid rules before deployment.
2. **Execution Timeout / Resource Limit:** CEL evaluation is strictly deterministic, non-Turing complete, and side-effect free. Maximum execution cost is capped at 10,000 instructions to prevent DoS.
3. **Missing Variable / Key:** Safe property access semantics (`request.headers[?key]`) ensure absent headers evaluate to empty/false rather than throwing runtime panics.

---

## 4. Acceptance Tests

1. `TestCEL_HeaderCondition_MatchesAndRewritesModel`
2. `TestCEL_TokenThresholdCondition_AppliesRoutingAction`
3. `TestCEL_SafeAttributeAccess_MissingHeaderDoesNotPanic`
4. `TestCEL_InvalidRule_RejectedAtCompileTime`
5. `TestCEL_PriorityOrdering`

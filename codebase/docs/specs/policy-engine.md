# Specification: Fine-Grained Policy Engine

**Status:** Draft (Wave 3)  
**Layer:** Request Authorization & Governance (`internal/policy`)  
**Design Reference Mode:** `BOTH`  

---

## 1. Purpose & Overview

The Policy Engine enforces organizational governance constraints across virtual keys, user identities, teams, and IP ranges prior to model execution.

Key capabilities:
- Enforcing maximum prompt and completion token boundaries.
- Restricting specific teams or roles to designated cost-tiered models (e.g. intern keys can only call mini models).
- IP CIDR allowlisting/denylisting.
- Rate limit throttling per user or client IP.
- Enforcing mandatory metadata (e.g. requiring `client_app_id` or `cost_center` in headers).

---

## 2. Policy Model & Evaluation

A Policy consists of:
- `ID`: Unique string identifier.
- `Scope`: Target organization, team, or virtual key prefix.
- `Rules`:
  - `AllowedModels`: Whitelist of glob patterns (e.g. `["gpt-4o-mini", "claude-3-haiku*"]`).
  - `MaxTokensLimit`: Upper cap on `max_tokens` or `max_completion_tokens`.
  - `MaxInputTokens`: Upper cap on estimated input prompt size.
  - `IPAllowlist`: List of valid CIDR blocks (e.g. `["10.0.0.0/8", "192.168.1.0/24"]`).
  - `RequiredHeaders`: Headers that must accompany every request.

---

## 3. Enforcement & Errors

- **Evaluation Timing:** Evaluated before wallet reservation.
- **Error Codes:**
  - `ErrPolicyViolation`: HTTP 403 Forbidden with details:
    ```json
    {
      "error": {
        "message": "Policy violation: requested model 'gpt-4o' not allowed for team 'interns'",
        "type": "policy_violation",
        "code": "forbidden_model"
      }
    }
    ```

---

## 4. Acceptance Tests

1. `TestPolicy_ModelAllowlist_BlocksDisallowedModel`
2. `TestPolicy_TokenCap_RejectsExcessiveMaxTokens`
3. `TestPolicy_IPAllowlist_AllowsInternalCIDR`
4. `TestPolicy_IPAllowlist_BlocksExternalIP`
5. `TestPolicy_RequiredHeadersEnforcement`

# Specification: Guardrail Framework & Layered Detection

**Status:** Draft (Wave 3)  
**Layer:** Content Safety & Moderation Pipeline (`internal/guardrails`)  
**Design Reference Mode:** `BOTH`  

---

## 1. Purpose & Overview

The Guardrail Framework intercepts incoming user prompts and outgoing model completions to enforce enterprise safety policies, protect against prompt injection/jailbreaking, redact Personally Identifiable Information (PII), mask internal secrets, and filter harmful/toxic content.

The architecture operates as a composable, layered pipeline supporting both **pre-call validation** (before dispatching to upstream providers) and **post-call inspection** (before streaming or returning completions to the client).

---

## 2. Guardrail Types & Detectors

1. **PII Redaction & Masking:**
   - Detects Social Security Numbers (SSN), Credit Card numbers (Luhn algorithm verified), email addresses, phone numbers, and IPv4/IPv6 addresses.
   - Modes: `Redact` (replaces with `[REDACTED_SSN]`, etc.) or `Block` (rejects with HTTP 400).
2. **Secret & Key Leakage Scanner:**
   - Detects API keys, private keys (`-----BEGIN PRIVATE KEY-----`), AWS access keys (`AKIA...`), and generic bearer tokens.
3. **Prompt Injection & Jailbreak Heuristics:**
   - Evaluates suspicious directive overriding phrases (e.g. `ignore previous instructions`, `DAN mode`, `developer mode override`).
4. **Toxicity & Profanity Filtering:**
   - Regex and keyword based moderation filter.

---

## 3. Pipeline Execution Flow

```
Inbound Request -> Pre-Call Guardrails -> (Pass?) -> Provider Execution -> Post-Call Guardrails -> (Pass?) -> Client
                          |                                                       |
                          v (Block)                                               v (Block/Redact)
                   HTTP 400 Refusal                                        Sanitized or Blocked
```

- **Short-Circuit on Block:** If any guardrail detector configured with `Action: "block"` flags a violation, the request is immediately aborted before incurring upstream API costs or wallet hold.
- **Header Injection:** Injects diagnostic headers:
  - `X-Gateway-Guardrail-Checked: true`
  - `X-Gateway-Guardrail-Violations: pii,prompt_injection`

---

## 4. Configuration Schema

```json
{
  "guardrails": {
    "enabled": true,
    "detectors": [
      {
        "name": "pii_scanner",
        "action": "mask",
        "types": ["email", "credit_card", "ssn"]
      },
      {
        "name": "prompt_injection_filter",
        "action": "block",
        "sensitivity": "medium"
      }
    ]
  }
}
```

---

## 5. Acceptance Tests

1. `TestGuardrail_PII_MasksCreditCardAndEmail`
2. `TestGuardrail_PromptInjection_BlocksMaliciousPrompt`
3. `TestGuardrail_SecretScanning_DetectsPrivateKey`
4. `TestGuardrail_PostCallInspection_SanitizesResponse`
5. `TestGuardrail_PassThrough_CleanPrompt`

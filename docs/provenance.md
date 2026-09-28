# Provenance Log

This document records the exact reference repository paths and commit hashes consulted during the clean-room specification design for Wave 1. In accordance with clean-room engineering guidelines, this log records paths and commit hashes only; no code, comments, or excerpts are reproduced.

## Reference Commit Hashes
- **LiteLLM**: `6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`
- **Bifrost**: `51172dbe37df018384d0f15f94940e01c40a7fff`

## Consulted References by Specification Area

### 1. Canonical Request/Response Schema and OpenAI-Compatible API
- `clones/bifrost/core/schemas/chatcompletions.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/bifrost/core/schemas/responses.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/bifrost/core/schemas/mux.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/litellm/litellm/types/llms/openai.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/litellm/litellm/types/utils.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)

### 2. Provider Adapters
- `clones/bifrost/core/providers/openai/` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/bifrost/core/providers/anthropic/` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/bifrost/core/providers/gemini/` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/litellm/litellm/llms/openai/` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/litellm/litellm/llms/anthropic/` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/litellm/litellm/llms/vertex_ai/` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)

### 3. Error Classification
- `clones/bifrost/core/failureclass.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/bifrost/core/schemas/failureclass.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/litellm/litellm/exceptions.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)

### 4. Retry and Backoff Policy
- `clones/bifrost/core/bifrost.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/bifrost/core/utils.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/litellm/litellm/router.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)

### 5. Key Selection and Cooldown
- `clones/bifrost/core/keyselectors/` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/litellm/litellm/router.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/litellm/litellm/router_strategy/` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)

### 6. Fallback Chain
- `clones/litellm/litellm/router.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/bifrost/core/streamfallback_test.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)

### 7. Streaming
- `clones/bifrost/core/abandonedstream_test.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/bifrost/core/azurestreamretry_test.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/bifrost/core/streamoverhead.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/litellm/litellm/main.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)

### 8. Pricing and Context-Window Registry
- `clones/litellm/model_prices_and_context_window.json` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/litellm/litellm/cost_calculator.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/bifrost/core/schemas/providercost_test.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)

### 9. Virtual Keys
- `clones/litellm/schema.prisma` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/litellm/litellm/proxy/proxy_server.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/bifrost/core/schemas/vault.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)

### 10. Budgets and Prepaid Wallet
- `clones/litellm/litellm/budget_manager.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/litellm/litellm/proxy/spend_tracking/` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)

### 11. Cost Attribution
- `clones/litellm/schema.prisma` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/litellm/litellm/proxy/spend_tracking/spend_tracking.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)

### 12. Metrics
- `clones/bifrost/core/schemas/trace.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/bifrost/core/schemas/tracer.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/bifrost/core/schemas/spanattrs.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/litellm/litellm/integrations/prometheus.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/litellm/litellm/integrations/opentelemetry.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)

### 13. Test Tooling (Mock Provider)
- `clones/bifrost/core/overhead_e2e_test.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/bifrost/core/abandonedstream_test.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/litellm/litellm/integrations/mock_client_factory.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)

## Wave 2 Provenance Logs

### 14. Routing Strategies (Latency, Cost, Least-Busy, TPM/RPM-Aware)
- `clones/litellm/litellm/router_strategy/lowest_latency.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/litellm/litellm/router_strategy/lowest_cost.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/litellm/litellm/router_strategy/least_busy.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/litellm/litellm/router_strategy/lowest_tpm_rpm_v2.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/bifrost/core/keyselectors/weightedrandom.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)

### 15. Adaptive Routing
- `clones/litellm/litellm/router_strategy/base_routing_strategy.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/bifrost/core/decisionemulation.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)

### 16. CEL Rules Engine
- Clean-room specification based on Google Common Expression Language (CEL-Go) standard specification without reference repo derivation.

### 17. Session Affinity
- `clones/bifrost/core/sessionaffinity.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/litellm/litellm/caching/affinity_cache.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)

### 18. Health Probing & Circuit Breaking
- `clones/litellm/litellm/router.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/bifrost/core/failureclass.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)

### 19. Exact & Semantic Response Cache
- `clones/litellm/litellm/caching/caching.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/litellm/litellm/caching/in_memory_cache.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/litellm/litellm/caching/_internal_lru_cache.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)
- `clones/bifrost/core/promptcache_test.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)

### 20. Runtime Configuration Changes
- `clones/litellm/litellm/proxy/proxy_server.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)

## Wave 3 Provenance Logs

### 21. Guardrail Framework & Layered Detection
- `clones/bifrost/core/schemas/guardrailmetadata.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/bifrost/core/schemas/redaction.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)

### 22. Policy Engine
- `clones/bifrost/core/schemas/grant.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)

### 23. Roles, SSO & SCIM Directory Synchronization
- Clean-room specification according to RFC 7643 and RFC 7644 (SCIM 2.0 Core Schema and Protocol).

### 24. Cryptographic Hash-Chained Audit Log
- Clean-room specification of append-only tamper-evident SHA-256 hash-chained block ledger.

### 25. Secrets & KMS Integration
- `clones/bifrost/core/schemas/vault.go` (`51172dbe37df018384d0f15f94940e01c40a7fff`)
- `clones/litellm/litellm/secret_managers/base_secret_manager.py` (`6f5ad78a1f019a340ec5d2e6f6d72ff1bf32e6c2`)

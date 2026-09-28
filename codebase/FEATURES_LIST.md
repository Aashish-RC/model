# LiteLLM: Complete Features List, Decisions & Actions

This document tracks all architectural features and capabilities of LiteLLM in the [codebase](file:///d:/model/codebase). It captures the decisions made across evaluation prompts (`KEEP` vs `DUMP`), flags items that are `Not Yet Considered`, and records the specific action taken for each feature in the repository.

---

## 1. Native Core (`litellm-rust`)

| Feature | Decision | Action Taken | Note |
|---|---|---|---|
| **Rust core engine** (gateway inference, router, SSE/HTTP stream framer) | **KEEP** | Retained in codebase (`litellm-rust/crates`) | Reference for high-performance hot path; eliminates GIL bottleneck |
| **Fast token counters** (`token-counter-fast`, `tiktoken`, `huggingface`) | **KEEP** | Retained in codebase | Needed for budget reservation, cost estimates, and rate-limiting |
| **Python bridge and compat layers** (`python-bridge`, `python-compat`, `callbacks-legacy-python`, `host-python`) | **DUMP** | **Deleted from codebase** (`litellm-rust/crates/python-*`, `callbacks-*`, `host-*`) | Irrelevant to a custom standalone gateway build |

---

## 2. Providers, Modalities and Translation

| Feature | Decision | Action Taken | Note |
|---|---|---|---|
| **OpenAI-format API translation** (chat, responses, embeddings, images, audio, batches) | **KEEP** | Retained in codebase | Core of the adapter contract |
| **140+ LLM provider integrations** | **KEEP** | Retained in codebase | Reference target for ecosystem breadth |
| **Pass-through routes for raw provider endpoints** | **KEEP** | Retained in codebase | Escape hatch for provider-specific capabilities |
| **Exception mapping to OpenAI-compatible errors** | **KEEP** | Retained in codebase | Feeds custom gateway error classes |
| **Python SDK as an embedded library** | **DUMP** | Marked for reference (not exposed as client library) | Customers call the gateway service over HTTP, not an embedded library |
| **Anthropic Messages API reverse translation** (`/v1/messages`) | **KEEP** | Retained in codebase (`litellm/anthropic_interface`) | Lets Anthropic-format clients reach any model |
| **Realtime WebSocket API** | **KEEP** | Retained in codebase (`litellm/realtime_api`) | Voice and streaming bidirectional agents |
| **Rerank API** (Cohere, Voyage, Jina) | **KEEP** | Retained in codebase (`litellm/rerank_api`) | Retrieval baseline |
| **Unified web search API** (`asearch` across Tavily, Google, Perplexity, Exa, Brave) | **KEEP** | Retained in codebase (`litellm/search`) | Matches search baseline |
| **OCR API** (Mistral OCR, etc.) | **KEEP** | Retained in codebase (`litellm/ocr`) | Document ingestion and vision pipelines |
| **Video generation endpoints** | **KEEP** | Retained in codebase (`litellm/videos`) | Media generation in scope |
| **Files API emulation** | **KEEP** | Retained in codebase (`litellm/files`) | Managed file attachments |
| **Managed vector stores API** | **KEEP** | Retained in codebase (`litellm/vector_stores`) | Retrieval baseline |
| **Assistants / Threads / Runs emulation** | **DUMP** | **Deleted from codebase** (`litellm/assistants`) | Custom build uses proprietary agent and memory design |
| **Fine-tuning API emulation** | **DUMP** | **Deleted from codebase** (`litellm/fine_tuning`, `litellm/proxy/fine_tuning_endpoints`) | Local/fine-tuned model training outside v1 scope |
| **OpenAI Evals API emulation** | **DUMP** | **Deleted from codebase** (`litellm/evals`, `litellm/proxy/openai_evals_endpoints`) | Proprietary stress-test harness covers evaluations |

---

## 3. Routing and Reliability

| Feature | Decision | Action Taken | Note |
|---|---|---|---|
| **Retry and fallback across deployments** | **KEEP** | Retained in codebase | Locked fallback chain; likely SWAP with proprietary design later |
| **Load balancing across providers, regions and keys** | **KEEP** | Retained in codebase | Compare against headroom-plus-growing-cooldown design |
| **Latency-based routing** | **KEEP** | Retained in codebase | Automatic routing to lowest-latency endpoints |
| **Lowest-cost routing** | **KEEP** | Retained in codebase | Feeds cost-aware model selection |
| **Auto routing** (simple prompts to cheaper models, complex to stronger) | **KEEP** | Retained in codebase | Overlaps intent classifier; likely MERGE or SWAP |
| **Budget fallbacks** (moving to cheaper model when budget threshold exceeded) | **KEEP** | Retained in codebase | Ties directly into wallet behavior |
| **TPM/RPM-aware routing** (steers away from near-limit deployments) | **KEEP** | Retained in codebase (`lowest_tpm_rpm_v2.py`) | Likely MERGE with headroom-based key selection |
| **Least-busy routing** (in-flight request concurrency balancing) | **KEEP** | Retained in codebase (`least_busy.py`) | Alternative load-balancing strategy |
| **Adaptive routing from rolling error and latency drift** | **KEEP** | Retained in codebase (`LiteLLM_AdaptiveRouterState`) | Pairs with growing cooldown mechanisms |
| **Shadow traffic evaluation** (async traffic copies to candidate models) | **KEEP** | Retained in codebase (`shadow_eval_logger.py`) | Useful for model benchmarking and stress testing |
| **Auto-router baseline cost and quality comparison** | **KEEP** | Retained in codebase (`LiteLLM_AutoRouterBaselineComparison`) | Feeds post-execution metrics vs baseline (e.g. GPT-4o) |
| **Tag-based routing** | **KEEP** | Retained in codebase (`tag_based_routing.py`) | Routing based on request tags/metadata |

---

## 4. Keys, Budgets, Spend and FinOps

| Feature | Decision | Action Taken | Note |
|---|---|---|---|
| **Virtual keys with limits, allowlists, expiry & attribution** | **KEEP** | Retained in codebase | Basis for org, team, and agent API keys |
| **Hard budgets per key, team, org, model** (daily/monthly resets) | **KEEP** | Retained in codebase | Reconciled with wallet-first billing |
| **Per-tag budgets** | **KEEP** | Retained in codebase | Project-level and department cost allocation |
| **Rate limits and leaked-key protection** | **KEEP** | Retained in codebase | Pairs with vault security work |
| **Request-level cost attribution** (key, team, user, model, tag) | **KEEP** | Retained in codebase | Feeds "every penny accounted for" ledger |
| **Two-phase budget reservation** (pre-call budget hold) | **KEEP** | Retained in codebase (`budget_reservation.py`) | Closest to wallet-first; stops concurrent overspend races |
| **PTU / provisioned-capacity cost tracking** | **KEEP** | Retained in codebase (`ptu_flat_cost_rollup.py`) | Flat-cost accounting for Azure PTUs and Bedrock Provisioned Throughput |
| **FinOps exporters** (CloudZero, Vantage, OpenMeter, Lago) & **FOCUS** | **KEEP** | Retained in codebase (`mavvrik_focus`) | Finance integration and FinOps Open Cost and Usage Specification |
| **Daily spend rollups** (per org, team, user, end-user, agent, tag) | **KEEP** | Retained in codebase | Pre-aggregated database tables for fast analytics |
| **Savings calculator** (routing, caching, compression vs baseline) | **KEEP** | Retained in codebase (`savings.py`) | Proves net dollar savings delivered to customers |

---

## 5. Caching and Performance

| Feature | Decision | Action Taken | Note |
|---|---|---|---|
| **Exact-match and semantic caching** (in-memory, Redis, S3, Qdrant) | **KEEP** | Retained in codebase | Baseline for caching; proprietary 0.95 threshold goes beyond |
| **L1 in-memory + L2 Redis dual cache** | **KEEP** | Retained in codebase (`dual_cache.py`) | Ultra-low-latency fast path |
| **Local disk cache** | **KEEP** | Retained in codebase (`disk_cache.py`) | Air-gapped and local SSD caching |
| **GCS and Azure Blob cache backends** | **KEEP** | Retained in codebase | Multi-cloud object storage caching alongside S3 |
| **Valkey and Redis cluster semantic cache** | **KEEP** | Retained in codebase | High-scale distributed cache options |
| **Affinity (sticky-session) cache** | **KEEP** | Retained in codebase (`affinity_cache.py`) | Multi-turn conversational caching efficiency |
| **Auto-injected Anthropic cache-control breakpoints** | **KEEP** | Retained in codebase (`anthropic_cache_control_hook.py`) | Automatically injects breakpoints to unlock 90% prompt caching discounts |

---

## 6. Guardrails, Security and Policies

| Feature | Decision | Action Taken | Note |
|---|---|---|---|
| **Core hook integrations** (Aporia, Lakera, Presidio) + pre/post callbacks | **KEEP** | Retained in codebase | Maps to before-and-after checkpoints |
| **PII masking, prompt-injection and secret-detection policies** | **KEEP** | Retained in codebase | Gateway-level safety and content sanitization |
| **Access groups** | **KEEP** | Retained in codebase | Role-based model access control |
| **Hyperscaler connectors** (Bedrock, Azure Content Safety, IBM, OpenAI) | **KEEP** | Retained in codebase | Cloud-native detection tier |
| **Security-vendor connector framework** (Palo Alto, CrowdStrike, Cisco, Zscaler, Cato, Purview) | **KEEP** | Retained in codebase | Framework for enterprise security and SASE integrations |
| **Long tail of ~25 specialist vendor connectors** (Akto, DeepKeep, DynamoAI, EnkryptAI, GraySwan, Lasso, Model Armor, etc.) | **DUMP** | **Deleted from codebase** (Removed 33 vendor directories from `litellm/proxy/guardrails/guardrail_hooks`) | Niche connectors pruned; framework enables on-demand addition |
| **Code-execution block and LLM-as-judge** | **KEEP** | Retained in codebase | Third detection tier and spot checking |
| **Declarative policy engine with wildcard scoping & inheritance** | **KEEP** | Retained in codebase (`litellm/proxy/policy_engine`) | Baseline plus org/team rules with wildcard matching |
| **Ephemeral code sandbox** (E2B, Modal, Docker) | **KEEP** | Retained in codebase (`litellm/sandbox`) | Safe code execution environment for agents |
| **Container file system CRUD API** | **DUMP** | **Deleted from codebase** (`litellm/containers`, `litellm/proxy/container_endpoints`) | Proprietary storage utilized instead |

---

## 7. Agents, Workflows and MCP

| Feature | Decision | Action Taken | Note |
|---|---|---|---|
| **Durable workflow runs with append-only event store & replay** | **KEEP** | Retained in codebase (`litellm/proxy/workflows`) | Fits DAG/cyclic execution, auditability, and crash recovery |
| **Session-to-spend binding** (`x-litellm-session-id`) | **KEEP** | Retained in codebase | Attributes tool calls and LLM invocations to specific workflow runs |
| **Agent-to-Agent (A2A) protocol and agent cards** | **KEEP** | Retained in codebase (`litellm/a2a_protocol`) | Interoperability across independent agent systems |
| **Agent entities with kill switch, headers & access groups** | **KEEP** | Retained in codebase (`LiteLLM_AgentsTable`) | Kill switch adds emergency governance over rogue agents |
| **Per-agent RPM/TPM, session limits and spend** | **KEEP** | Retained in codebase | Treats agents as first-class authenticated users |
| **MCP server registry and toolsets** | **KEEP** | Retained in codebase (`LiteLLM_MCPServerTable`, `LiteLLM_MCPToolsetTable`) | Central gateway for Model Context Protocol tools |
| **Per-user MCP credentials and OAuth handshakes** | **KEEP** | Retained in codebase | Required for real user tool authentication |
| **Per-user tool permissions and allow/deny policies** | **KEEP** | Retained in codebase (`tool_permission.py`) | Enforces least-privilege tool execution |
| **JWT signing of tool-call tokens** | **KEEP** | Retained in codebase | Cryptographic tamper-proofing of tool calls |

---

## 8. Tenancy, Access Control and Admin

| Feature | Decision | Action Taken | Note |
|---|---|---|---|
| **Stateless proxy tier with PostgreSQL and Redis** | **KEEP** | Retained in codebase | Reference architecture for scalable gateway |
| **Self-hosting anywhere, including air-gapped** | **KEEP** | Retained in codebase | Complies with signed-license offline requirements |
| **Projects layer between teams and keys** | **KEEP** | Retained in codebase (`LiteLLM_ProjectTable`) | Isolates specific use-cases/environments under teams |
| **End-user tracking with soft and hard limits** | **KEEP** | Retained in codebase (`LiteLLM_EndUserTable`) | Tracks and throttles individual end-users of client apps |
| **Per-member models, roles and budgets** | **KEEP** | Retained in codebase (`LiteLLM_TeamMembership`) | Scoped member permissions inside organizations/teams |
| **Self-service invitation links** | **KEEP** | Retained in codebase (`LiteLLM_InvitationLink`) | Team and organization onboarding |
| **Deleted-entity audit trail** | **KEEP** | Retained in codebase (`LiteLLM_DeletedTeamTable`) | Preserves historical spend and audit integrity after deletions |
| **Dynamic JWT claim mapping to teams, budgets & keys** | **KEEP** | Retained in codebase (`handle_jwt.py`) | Dynamic auth via Okta, Keycloak, Auth0 without pre-provisioning keys |
| **IP allowlists, CIDR filtering, trusted proxy headers** | **KEEP** | Retained in codebase (`ip_address_utils.py`) | Enterprise network perimeter controls |
| **Login rate throttling** | **KEEP** | Retained in codebase (`login_throttle.py`) | Protection against brute-force attacks |
| **Password complexity policy** | **DUMP** | **Deleted from codebase** (`litellm/proxy/auth/password_policy.py`) | Proprietary stack uses magic links and SSO |
| **Worker heartbeats and distributed cron** | **KEEP** | Retained in codebase (`LiteLLM_ProxyWorkerHeartbeat`, `LiteLLM_CronJob`) | Multi-replica high availability coordination |
| **Claude Code plugin marketplace table** | **DUMP** | **Deleted from codebase** (Removed `LiteLLM_ClaudeCodePluginTable` from `schema.prisma`) | Outside the core gateway scope |
| **Observability logging** (Langfuse, Arize Phoenix, LangSmith, OTEL) | **KEEP** | Retained in codebase | OpenTelemetry and LLM observability exports |
| **Admin dashboard UI** | **KEEP** | Retained in codebase (`ui/`) | Separate admin panel product baseline |
| **Audit log on every request** | **KEEP** | Retained in codebase (`LiteLLM_AuditLog`) | Compliance ledger; proprietary build adds hash chaining |
| **Hooks for auth and logging** | **KEEP** | Retained in codebase | Core extensibility points |
| **SSO/SAML and multi-team management** | **KEEP** | Retained in codebase | Enterprise team hierarchy |

---

## 9. Secrets, Prompts and Memory

| Feature | Decision | Action Taken | Note |
|---|---|---|---|
| **Runtime secret resolution** (AWS, Azure, Google, HashiCorp, CyberArk) | **KEEP** | Retained in codebase (`litellm/secret_managers`) | Dynamic secret retrieval; directly relevant to vault design |
| **RDS IAM passwordless DB auth** | **DUMP** | **Deleted from codebase** (`litellm/proxy/auth/rds_iam_token.py`) | AWS-specific; add on demand if required |
| **Prompt registry with versioning and variables** | **KEEP** | Retained in codebase (`litellm/proxy/prompts`) | Centralized prompt template management |
| **Memory API with user- and team-scoped memories** | **KEEP** | Retained in codebase (`/v1/memory`) | Entity and conversation memory endpoints |
| **Prompt compression and context pruning** | **KEEP** | Retained in codebase (`litellm/compression`) | Reduces upstream token count and latency |

---

## 10. Operations, Proactive Health & Platform Extensions

The following 20 features have been evaluated and actioned as per Section 10 decisions:

| Feature | Decision | Action Taken | Note |
|---|---|---|---|
| **Model pricing and context-window registry** | **KEEP** | Retained in codebase (`model_prices_and_context_window.json`, `cost_calculator.py`) | Needed for wallet cost math and context-aware routing |
| **Proactive alerting (Slack, Teams, email)** | **KEEP** | Retained in codebase (`litellm/integrations/SlackAlerting`, `email_alerting.py`) | Budget breaches, hanging requests and outages. Build the channel framework, not every channel. |
| **Active health probing and circuit breaker** | **KEEP** | Retained in codebase (`health_check.py`, `LiteLLM_HealthCheckTable`) | Overlaps your growing cooldown, so a likely MERGE in the later pass |
| **Credential decoupling and independent key rotation** | **KEEP** | Retained in codebase (`LiteLLM_CredentialsTable`, `/credentials`) | Key rotation without touching routes fits BYOK and the vault |
| **Batch completion and async polling** | **KEEP** | Retained in codebase (`batch_completion/`, `response_polling/`) | Batches are in scope, and long jobs need polling and webhooks |
| **Context compaction for complexity routing** | **KEEP** | Retained in codebase (`native_compaction.py`) | Overlaps prompt compression, so a likely MERGE |
| **Runtime config overrides without restart** | **KEEP** | Retained in codebase (`LiteLLM_ConfigOverrides`, `LiteLLM_UISettings`) | Supports your near-zero-downtime target |
| **Managed provider ID rewriter** | **KEEP** | Retained in codebase (`managed_id_rewriter.py`) | Needed for files and batches across providers |
| **Pluggable hooks framework** | **KEEP** | Retained in codebase (`custom_hooks/`, `custom_validate.py`) | Keep the pre-call, post-call and auth checkpoints. The "external Python scripts" mechanism conflicts with sealed delivery and with your dumped Python bridge, so redesign it. |
| **KMS envelope encryption for keys and secrets** | **KEEP** | Retained in codebase (`google_kms.py`, AWS KMS hooks) | Matches your org-KMS application-layer encryption |
| **Reasoning and thinking-budget normalization** | **KEEP** | Retained in codebase | One syntax across providers |
| **Anthropic beta-header normalization** | **KEEP** | Retained in codebase (`anthropic_beta_headers_manager.py`) | Generalize it into provider feature flags |
| **Google Interactions and deep-research agent API** | **DUMP** | **Deleted from codebase** (`litellm/interactions`) | One vendor's native agent API. Your agents are task-scoped and orchestrated by you. |
| **Native RAG query pipeline** | **KEEP** | Retained in codebase (`litellm/rag`) | Retrieval is a locked baseline. Its chunking and splitters aren't adopted, since document ingestion is still open. |
| **Agent skills management API** | **KEEP** | Retained in codebase (`litellm/skills`, `discovery_endpoints`) | Reusable agent capabilities fit conversational and form-built agents. Low priority. |
| **DataDog APM span tagging** | **DUMP** | **Deleted from codebase** (`litellm/proxy/dd_span_tagger.py`) | Vendor-specific. Your kept OTEL export covers span attributes. |
| **Spend-log cold storage archival** | **KEEP** | Retained in codebase (`cold_storage_handler.py`) | Fits your write-once object storage and keeps the main database lean |
| **Budget throttling and soft degradation** | **KEEP** | Retained in codebase (`budget_throttle.py`) | A gentle ramp before the wallet's read-only state at zero |
| **Auth object prefetch and caching** | **KEEP** | Retained in codebase (`auth_object_prefetch.py`) | Hot-path speed |
| **Compliance and data-residency boundary checks** | **KEEP** | Retained in codebase (`compliance_checks.py`) | Regional and regulatory enforcement for enterprise |

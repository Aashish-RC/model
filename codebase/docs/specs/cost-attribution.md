# Specification: Request-Level Cost Attribution

## 1. Purpose
The Cost Attribution Engine tracks and attributes every LLM invocation, token consumed, and dollar fraction spent to the responsible entity. It captures full multi-dimensional metadata (virtual key, team, organization, user, requested model, effective model, and custom tags) on every request, providing comprehensive visibility into AI consumption for billing, chargebacks, and FinOps audits.

## 2. Design Strategy & Label: LITE
Adopt the reference spend-tracking and request attribution pattern:
- Every request generates a detailed audit record capturing input tokens, output tokens, cached tokens, duration, calculated cost, and hierarchical identifiers (org, team, user, key).
- Ingested tags (`tags` array or `metadata` dictionary in the request) are persisted alongside system attributes.
- Audit records are written asynchronously via a buffered ring buffer / channel to avoid adding latency to the response hot path.

## 3. Data Model & Spend Record Schema
```sql
CREATE TABLE spend_logs (
    request_id VARCHAR(64) PRIMARY KEY,
    org_id VARCHAR(64) NOT NULL,
    team_id VARCHAR(64),
    user_id VARCHAR(64),
    virtual_key_id VARCHAR(64) NOT NULL,
    requested_model VARCHAR(128) NOT NULL,
    effective_model VARCHAR(128) NOT NULL,
    provider VARCHAR(64) NOT NULL,
    prompt_tokens INT NOT NULL DEFAULT 0,
    completion_tokens INT NOT NULL DEFAULT 0,
    cached_tokens INT NOT NULL DEFAULT 0,
    total_tokens INT NOT NULL DEFAULT 0,
    cost_micro BIGINT NOT NULL DEFAULT 0,      -- Micro-dollars (1 USD = 1,000,000)
    latency_ms INT NOT NULL,
    time_to_first_token_ms INT,
    status_code INT NOT NULL,
    error_code VARCHAR(64),
    is_stream BOOLEAN DEFAULT FALSE,
    is_downgraded BOOLEAN DEFAULT FALSE,
    custom_tags JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_spend_logs_org_date ON spend_logs(org_id, created_at);
CREATE INDEX idx_spend_logs_team_date ON spend_logs(team_id, created_at);
CREATE INDEX idx_spend_logs_key_date ON spend_logs(virtual_key_id, created_at);
CREATE INDEX idx_spend_logs_model ON spend_logs(effective_model);
```

## 4. Ingestion & Emission Architecture

```
[Request Ingress]
       │ (Extract KeyID, OrgID, TeamID, User, Custom Tags)
       ▼
[Execution Complete]
       │ (Extract Latency, TTFT, Token Counts, CostMicro, Status)
       ▼
[CostAttributor.RecordAsync(entry)]
       │
       ▼
[Buffered Channel (capacity: 10,000)]
       │
       ▼
[Worker Pool (Batch Writer)]
       │ (Groups up to 500 records or 100ms interval)
       ▼
[Batch INSERT into spend_logs (PostgreSQL)]
```

## 5. Aggregations & Rollups
For fast dashboarding and chargeback reports, background workers compute pre-aggregated daily summaries:
- `daily_org_spend` (`org_id`, `date`, `total_cost_micro`, `total_tokens`, `request_count`)
- `daily_team_spend` (`team_id`, `date`, `total_cost_micro`, `total_tokens`)
- `daily_model_spend` (`model`, `date`, `total_cost_micro`, `total_tokens`)

## 6. Edge Cases
- **Buffer Overflow Under Extreme Spike**: If the buffered channel fills due to database lag, workers shed non-essential debug metadata while guaranteeing that financial totals in the wallet ledger remain 100% consistent.
- **Failed Requests**: Requests failing before provider contact (e.g. 401 auth error, 429 rate limit) record 0 tokens and 0 cost, but record status code and error code to attribute rejection rates by key.

## 7. Configuration
- `attribution.buffer_size`: Ring buffer capacity (default: 10000).
- `attribution.batch_size`: Batch insert size (default: 500).
- `attribution.flush_interval_ms`: Max delay before flushing partial batch (default: 100).

## 8. Acceptance Tests
1. **Full Metadata Capture**: Submit request with `user="dev-123"` and `metadata={"env":"prod","project":"agent-alpha"}`; assert `spend_logs` contains matching user and custom tag entries.
2. **Asynchronous Non-Blocking Execution**: Inject a 500ms delay into the database batch writer; assert client request latency remains unaffected.
3. **Rollup Calculation Accuracy**: Write 1,000 spend records across 3 teams; run aggregation worker; assert sum of team totals exactly matches the grand total.

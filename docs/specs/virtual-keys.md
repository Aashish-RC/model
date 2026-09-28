# Specification: Virtual Keys (Minimal)

## 1. Purpose
Virtual Keys provide client-facing authentication and authorization tokens that decouple external API consumers from upstream cloud credentials. Each virtual key enforces multi-tenant boundaries, applies rate limits (requests per minute and tokens per minute), restricts model access through allowlists, enforces expiration deadlines, and attributes all token usage and spend to specific organizations, teams, and users.

## 2. Design Strategy & Label: BOTH
- Adopt the comprehensive tenant-hierarchy data model (org -> team -> virtual key -> end user).
- Adopt the Go reference's high-throughput caching and cryptographic token hashing design:
  - Virtual keys are generated with a high-entropy random prefix and secret (`sk-gw-...`).
  - Raw key values are shown to callers **only once upon creation**; the database stores only the SHA-256 hash.
  - Active keys are cached in memory / Redis to avoid database queries on every HTTP request.

## 3. Data Model & Schema
```sql
CREATE TABLE virtual_keys (
    id VARCHAR(64) PRIMARY KEY,
    key_hash VARCHAR(64) NOT NULL UNIQUE,
    key_prefix VARCHAR(16) NOT NULL,
    name VARCHAR(128) NOT NULL,
    org_id VARCHAR(64) NOT NULL,
    team_id VARCHAR(64),
    user_id VARCHAR(64),
    models_allowlist TEXT[],           -- NULL or empty means all models permitted
    max_budget_micro BIGINT,          -- Lifetime spending ceiling in micro-dollars
    spend_micro BIGINT DEFAULT 0,     -- Accumulated spend in micro-dollars
    rpm_limit INT DEFAULT 0,          -- 0 = unlimited
    tpm_limit INT DEFAULT 0,          -- 0 = unlimited
    is_active BOOLEAN DEFAULT TRUE,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_virtual_keys_hash ON virtual_keys(key_hash);
CREATE INDEX idx_virtual_keys_org ON virtual_keys(org_id);
```

## 4. Key Lifecycle Operations

### 4.1 Create Key (`POST /v1/admin/keys`)
- **Inputs**: `name`, `org_id`, `team_id`, `models_allowlist`, `rpm_limit`, `tpm_limit`, `max_budget_micro`, `expires_at`.
- **Generation**: Generates 32 bytes of cryptographically secure random bytes: `sk-gw-<prefix>-<random_hex>`.
- **Outputs**: Returns the raw key string once, alongside the key metadata object.

### 4.2 Validate Key (Request Hot Path)
1. Read `Authorization: Bearer sk-gw-...`.
2. Compute SHA-256 hash of the bearer token.
3. Check fast local in-memory L1 cache (`sync.Map` or LRU).
4. If cache miss, fetch from PostgreSQL and populate L1 cache with TTL (e.g. 5 minutes).
5. Check `is_active == true`.
6. Check `now < expires_at`.
7. Check `models_allowlist`: If configured, assert requested model is explicitly contained in the slice.
8. Evaluate key-level sliding-window rate limits (RPM and TPM).
9. Bind key metadata (`OrgID`, `TeamID`, `KeyID`) to the request context.

### 4.3 Revoke Key (`DELETE /v1/admin/keys/:id` or `POST /v1/admin/keys/:id/revoke`)
- Sets `is_active = false` in database.
- Immediately evicts key from memory/Redis cache across all gateway nodes.
- Subsequent calls using this key return `401 Unauthorized`.

## 5. Rate Limiting Implementation
- Rate limits use a distributed token bucket or sliding window algorithm in Redis (or in-memory fallback for standalone mode).
- Keys exceeding RPM return `429 Too Many Requests` with header `Retry-After: <seconds>` and OpenAI-format error: `type: "rate_limit_error"`, `code: "virtual_key_rpm_limit_exceeded"`.
- Keys exceeding TPM return `429 Too Many Requests` with code `"virtual_key_tpm_limit_exceeded"`.

## 6. Edge Cases
- **Revocation Propagation**: When a key is revoked, in-flight requests that have already passed authentication are permitted to finish; subsequent requests are rejected within $\le 1\text{ms}$ across the cluster.
- **Clock Drift on Expiration**: Expiration timestamps use UTC with a 5-second grace window to absorb minor clock skews between cluster nodes.
- **Budget Exceeded**: If `spend_micro >= max_budget_micro`, key is blocked with `429 / 402` ("Virtual key budget exceeded").

## 7. Configuration
- `auth.cache_ttl_seconds`: Duration key validation result is cached in memory (default: 300).
- `auth.key_prefix`: Prefix for issued virtual keys (default: `sk-gw-`).

## 8. Acceptance Tests
1. **Creation and Hashing**: Create a virtual key; assert raw key is returned; assert database stores only the SHA-256 hash.
2. **Model Allowlist Enforcement**: Configure key with allowlist `["gpt-4o"]`; attempt calling `claude-3-5-sonnet`; assert `403 Forbidden` with `"model_not_allowed"`.
3. **Expiration Rejection**: Create a key with past `expires_at`; assert immediate `401 Unauthorized` with `"key_expired"`.
4. **Immediate Revocation**: Validate key succeeds; call revoke endpoint; assert subsequent call fails with `401 Unauthorized` immediately.
5. **RPM Limiting**: Configure key with `rpm_limit = 5`; fire 6 rapid requests; assert 6th request receives `429 Too Many Requests`.

-- Migration: 001_init_virtual_keys_and_wallets.sql

CREATE TABLE IF NOT EXISTS virtual_keys (
    id VARCHAR(64) PRIMARY KEY,
    key_hash VARCHAR(64) NOT NULL UNIQUE,
    key_prefix VARCHAR(16) NOT NULL,
    name VARCHAR(128) NOT NULL,
    org_id VARCHAR(64) NOT NULL,
    team_id VARCHAR(64),
    user_id VARCHAR(64),
    models_allowlist TEXT[],
    max_budget_micro BIGINT,
    spend_micro BIGINT DEFAULT 0,
    rpm_limit INT DEFAULT 0,
    tpm_limit INT DEFAULT 0,
    is_active BOOLEAN DEFAULT TRUE,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_virtual_keys_hash ON virtual_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_virtual_keys_org ON virtual_keys(org_id);

CREATE TABLE IF NOT EXISTS wallets (
    org_id VARCHAR(64) PRIMARY KEY,
    balance_micro BIGINT NOT NULL CHECK (balance_micro >= 0),
    reserved_micro BIGINT NOT NULL DEFAULT 0 CHECK (reserved_micro >= 0),
    currency VARCHAR(8) DEFAULT 'USD',
    is_frozen BOOLEAN DEFAULT FALSE,
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    CONSTRAINT balance_sufficient CHECK (balance_micro >= reserved_micro)
);

CREATE TABLE IF NOT EXISTS wallet_ledger (
    id VARCHAR(64) PRIMARY KEY,
    org_id VARCHAR(64) NOT NULL,
    request_id VARCHAR(64),
    virtual_key_id VARCHAR(64),
    transaction_type VARCHAR(32) NOT NULL, -- 'CREDIT', 'RESERVATION', 'SETTLEMENT', 'RELEASE'
    amount_micro BIGINT NOT NULL,
    balance_after_micro BIGINT NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_wallet_ledger_org ON wallet_ledger(org_id, created_at);

CREATE TABLE IF NOT EXISTS spend_logs (
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
    cost_micro BIGINT NOT NULL DEFAULT 0,
    latency_ms INT NOT NULL,
    time_to_first_token_ms INT,
    status_code INT NOT NULL,
    error_code VARCHAR(64),
    is_stream BOOLEAN DEFAULT FALSE,
    is_downgraded BOOLEAN DEFAULT FALSE,
    custom_tags JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_spend_logs_org_date ON spend_logs(org_id, created_at);
CREATE INDEX IF NOT EXISTS idx_spend_logs_team_date ON spend_logs(team_id, created_at);
CREATE INDEX IF NOT EXISTS idx_spend_logs_key_date ON spend_logs(virtual_key_id, created_at);
CREATE INDEX IF NOT EXISTS idx_spend_logs_model ON spend_logs(effective_model);

# Specification: Budgets and Prepaid Wallet

## 1. Purpose
The Prepaid Wallet and Budget Engine enforces strictly non-negative balance accounting for organizations. To guarantee that high-concurrency request spikes cannot cause balance overdrafts, the engine implements a **two-phase budget reservation protocol**: funds are reserved (held) before upstream dispatch and settled (committed or released) after call completion. When an organization's balance reaches zero, the organization automatically drops into a read-only state where no new generation calls are permitted. Every transaction is immutably recorded as an itemized ledger entry.

## 2. Design Strategy & Label: OWN + LITE
- Adopt the reference concept of hierarchical budget tracking (org, team, key limits).
- Follow the locked custom design:
  1. **Two-Phase Reservation**:
     - Phase 1 (Pre-Call): Estimate maximum possible call cost based on input tokens + max requested completion tokens; place a temporary hold against the organization's wallet balance. If balance is insufficient, reject call immediately with `402 Payment Required`.
     - Phase 2 (Post-Call): Compute actual token cost from the provider response; debit exact cost from the wallet and release the unspent hold back to available balance.
  2. **Zero-Balance Read-Only Boundary**:
     - At zero available balance, new generation requests are blocked (`402 Payment Required`).
     - Non-generative endpoints (e.g. read operations, audit queries, model list `/v1/models`) remain accessible.
     - Overdrafts and negative balances are strictly impossible.
  3. **Itemized Immutable Ledger**: Every debit, refund, and credit is stored as a row in an append-only ledger table.

## 3. Data Model & Schema
```sql
CREATE TABLE wallets (
    org_id VARCHAR(64) PRIMARY KEY,
    balance_micro BIGINT NOT NULL CHECK (balance_micro >= 0),
    reserved_micro BIGINT NOT NULL DEFAULT 0 CHECK (reserved_micro >= 0),
    currency VARCHAR(8) DEFAULT 'USD',
    is_frozen BOOLEAN DEFAULT FALSE,
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    CONSTRAINT balance_sufficient CHECK (balance_micro >= reserved_micro)
);

CREATE TABLE wallet_ledger (
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

CREATE INDEX idx_wallet_ledger_org ON wallet_ledger(org_id, created_at);
```

## 4. Two-Phase Reservation Protocol

### Phase 1: Pre-Call Reservation
1. Calculate estimated max cost:
   $$\text{InputTokens} = \text{CountTokens}(\text{request.messages})$$
   $$\text{MaxOutputTokens} = \text{coalesce}(\text{request.max\_tokens}, \text{model.default\_max\_output})$$
   $$\text{HoldAmountMicro} = (\text{InputTokens} \times C_{\text{input}}) + (\text{MaxOutputTokens} \times C_{\text{output}})$$
2. Execute atomic reservation query (PostgreSQL with Row-Level Locking or Redis Lua script):
   ```sql
   UPDATE wallets
   SET reserved_micro = reserved_micro + $HoldAmountMicro
   WHERE org_id = $OrgID 
     AND (balance_micro - reserved_micro) >= $HoldAmountMicro
     AND is_frozen = FALSE;
   ```
3. If no rows updated:
   - Available balance is insufficient.
   - Return `402 Payment Required` (`type: "insufficient_balance"`, `message: "Organization prepaid wallet balance is exhausted"`).
4. Store `ReservationID` and `HoldAmountMicro` in request context.

### Phase 2: Post-Call Settlement
Upon upstream response (or stream conclusion):
1. Extract actual usage: `ActualInputTokens`, `ActualOutputTokens`.
2. Compute actual cost:
   $$\text{ActualCostMicro} = (\text{ActualInputTokens} \times C_{\text{input}}) + (\text{ActualOutputTokens} \times C_{\text{output}})$$
3. Execute atomic settlement transaction:
   ```sql
   BEGIN;
   -- Release the entire hold and deduct actual cost
   UPDATE wallets
   SET reserved_micro = reserved_micro - $HoldAmountMicro,
       balance_micro = balance_micro - $ActualCostMicro
   WHERE org_id = $OrgID;

   -- Write immutable itemized ledger row
   INSERT INTO wallet_ledger (id, org_id, request_id, virtual_key_id, transaction_type, amount_micro, balance_after_micro, description)
   VALUES ($LedgerID, $OrgID, $RequestID, $VirtualKeyID, 'SETTLEMENT', $ActualCostMicro, (SELECT balance_micro FROM wallets WHERE org_id = $OrgID), $AuditDescription);
   COMMIT;
   ```
4. If the call failed prior to token generation (e.g. upstream 5xx or network abort):
   - `ActualCostMicro = 0`.
   - The entire `HoldAmountMicro` is released back to the wallet (`reserved_micro = reserved_micro - HoldAmountMicro`).

## 5. Concurrency Invariants
- **No Overdraft**: Because `(balance_micro - reserved_micro) >= HoldAmountMicro` is enforced atomically before any work is dispatched, $N$ concurrent requests can never commit more spend than available funds.
- **Lost Reservations (Orphan Cleanup)**: If a gateway node crashes while a request is in-flight, a background sweeper reconciles reservations older than 10 minutes against completed request records and releases orphaned holds.

## 6. Edge Cases
- **Actual Cost Exceeds Hold**: If provider output tokens exceed the estimated `MaxOutputTokens` ceiling, the settlement debits the full actual cost down to 0, but never allows balance to go negative (the difference is absorbed by a configurable reserve buffer or capped at balance).
- **Zero Balance Transitions**: As soon as `balance_micro - reserved_micro == 0`, all subsequent incoming calls are rejected immediately at the ingress gateway without contacting any upstream LLM.

## 7. Configuration
- `wallet.orphan_reservation_timeout_seconds`: Sweeper expiration window (default: 600).
- `wallet.min_reservation_micro`: Minimum hold per call (default: 1,000 $\mu\text{USD}$ / $0.001).

## 8. Acceptance Tests
1. **Concurrency Protection Under 1,000 Requests**: Initialize wallet with $\$10.00$ ($10,000,000\ \mu\text{USD}$). Fire 1,000 concurrent requests each needing $\$0.02$ hold ($\$20.00$ total demand). Assert that exactly 500 requests succeed, remaining 500 receive `402 Payment Required`, and final balance is strictly non-negative ($\ge \$0.00$).
2. **Reservation Release on Failure**: Simulate upstream 500 error; verify hold is 100% released and balance remains unchanged.
3. **Itemized Ledger Integrity**: Execute 10 requests; assert ledger table contains exactly 10 matching settlement entries with accurate token counts and descending balances.
4. **Read-Only Mode at Zero**: Drain wallet to 0; assert `/v1/chat/completions` returns 402 while `/v1/models` succeeds with 200 OK.

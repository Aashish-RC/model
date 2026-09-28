# Specification: Cryptographic Hash-Chained Audit Log

**Status:** Draft (Wave 3)  
**Layer:** Audit & Compliance Ledger (`internal/audit`)  
**Design Reference Mode:** `OWN` (Clean-Room tamper-evident blockchain ledger)  

---

## 1. Purpose & Overview

Compliance regulations (SOC 2 Type II, ISO 27001, HIPAA, FedRAMP) require non-repudiable, tamper-evident audit trails of every administrative action, virtual key generation, budget adjustment, and model execution event.

The Cryptographic Audit Log organizes entries as an append-only, sequentially linked SHA-256 hash chain (similar to a Git commit tree or blockchain ledger). Any retroactive deletion, insertion, or byte alteration in the historical ledger invalidates all subsequent block hashes, enabling cryptographic proof of tampering.

---

## 2. Hash Chain Data Structure

Each entry $i$ contains:
- `SequenceID`: Monotonically increasing 64-bit integer ($1, 2, 3, \dots$).
- `Timestamp`: UTC nanosecond timestamp.
- `Actor`: Identity that triggered the action (`user_id`, `key_id`, or `system`).
- `OrgID`: Tenant organization identifier.
- `Action`: Standard verb (`key.created`, `wallet.debited`, `policy.updated`, `model.called`).
- `Resource`: Target resource URI (`keys/vk-123`, `models/gpt-4o`).
- `PayloadHash`: SHA-256 of the event payload metadata.
- `PreviousHash`: Hex-encoded SHA-256 hash of block $i-1$. (Block 0 uses genesis hash `0000000000000000000000000000000000000000000000000000000000000000`).
- `EntryHash`:
  $$\text{EntryHash}_i = \text{SHA256}(\text{SequenceID} + \text{Timestamp} + \text{Actor} + \text{Action} + \text{Resource} + \text{PayloadHash} + \text{PreviousHash})$$

---

## 3. Verification & Tamper Detection

The system provides a verification tool:
- `VerifyChain(entries []*AuditEntry) (bool, error)`:
  - Iterates sequentially through the ledger.
  - Recomputes $\text{EntryHash}_i$ and asserts equality.
  - Asserts $\text{PreviousHash}_i == \text{EntryHash}_{i-1}$.
  - Detects missing sequence gaps or modified payload contents with exact index identification.

---

## 4. Acceptance Tests

1. `TestAuditLog_SequentialAppend_MaintainsValidHashChain`
2. `TestAuditLog_TamperPayload_FailsVerification`
3. `TestAuditLog_DeletedEntry_DetectsSequenceGap`
4. `TestAuditLog_ConcurrentAppend_ThreadSafe`
5. `TestAuditLog_GenesisBlock_Validates`

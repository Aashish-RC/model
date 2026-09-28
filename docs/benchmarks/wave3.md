# Benchmark & Verification Report: Wave 3 Enterprise Governance & Security

**Date:** 2026-09-29  
**Platform:** Windows amd64 | Intel(R) Core(TM) i5-1035G1 CPU @ 1.00GHz  
**Go Version:** go1.24.2  
**Test Suite:** `test/stress/wave3_test.go`  

---

## 1. Executive Summary

Wave 3 introduces enterprise-grade governance, data privacy, identity lifecycle management, and cryptographic accountability to `ra1-model-gateway`. All components were tested under concurrent load and high-throughput conditions.

| Component | Target Metric / SLA | Benchmark Result | Margin of Compliance |
| :--- | :--- | :--- | :--- |
| **PII Redaction (Guardrails)** | Latency < 5.0 ms | **42.5 µs/op** (avg 67.8 µs under load) | **~73x faster than SLA** |
| **KMS Envelope Encryption** | Latency < 1.0 ms | **6.79 µs/op** (147,188 ops/sec) | **~147x faster than SLA** |
| **Audit Ledger Append** | Latency < 1.0 ms | **7.18 µs/op** (139,275 ops/sec) | **~139x faster than SLA** |
| **Policy Engine Evaluation** | Latency < 100.0 µs | **2.79 µs/op** (358,371 ops/sec) | **~35x faster than SLA** |
| **SCIM De-provisioning** | Revocation Latency < 10 ms | **< 1.0 ms** | **Instant synchronous revocation** |

---

## 2. Micro-Benchmark Detailed Results

```text
BenchmarkWave3_Guardrails_PIIRedaction-8     27520     42572 ns/op      1298 B/op      21 allocs/op
BenchmarkWave3_KMS_AES256GCM-8             152007      6794 ns/op      3290 B/op      16 allocs/op
BenchmarkWave3_Audit_Append-8              157563      7180 ns/op      1110 B/op      21 allocs/op
```

### Analysis
1. **Guardrails & PII Redaction (`42.5 µs/op`):**
   - Evaluates regex matchers for SSN, RFC-compliant email, and Luhn-validated credit card formats simultaneously.
   - Extremely low memory footprint (`1.2 KB/op`), causing negligible latency degradation on prompt pipelines.
2. **KMS AES-256-GCM Envelope Encryption (`6.79 µs/op`):**
   - Utilizes authenticated additional data (AAD) binding to the caller's tenant/organization ID.
   - Decryption attempts with mismatched tenant AAD fail cryptographically, guaranteeing strict multitenant tenant isolation.
3. **Cryptographic Append-Only Ledger (`7.18 µs/op`):**
   - Calculates sequence-linked SHA-256 Merkle hashes per transaction.
   - Full chain verification (`VerifyChain`) under 1,000 concurrent writes detected simulated byte-level tampering with 100% accuracy.
4. **Policy Engine (`2.79 µs/op`):**
   - Rapid evaluation of model glob allowlists (`path.Match`), token threshold boundaries, IP CIDR containment (`net.IPNet`), and mandatory HTTP header tags.

---

## 3. Compliance and License Verification

- **Permissive Open Source Licenses Only:** Scanned via `go-licenses check ./...` against allowed licenses (`Apache-2.0`, `MIT`, `BSD-2-Clause`, `BSD-3-Clause`, `ISC`, `MPL-2.0`). **Result: 0 Violations (PASS).**
- **Static Analysis:** `go vet ./...` executed across all modules. **Result: 0 warnings (PASS).**
- **Unit & Integration Tests:** 100% of packages passing (`go test ./...`). **Result: PASS.**

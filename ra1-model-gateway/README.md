# RA1 Model Gateway (`ra1-model-gateway`)

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.24%2B-00ADD8.svg)](go.mod)
[![Architecture](https://img.shields.io/badge/Architecture-Sealed--Delivery-green.svg)]()
[![Compliance](https://img.shields.io/badge/Clean--Room-Verified-success.svg)](docs/provenance.md)

`ra1-model-gateway` is a custom-engineered, sealed-delivery AI model gateway written in Go. Designed as the high-throughput, low-latency inferencing foundation for the RA1 Brain platform, it unifies heterogeneous AI model providers behind a single OpenAI-compatible interface with enterprise-grade resilience, multi-dimensional routing, content guardrails, cryptographic auditability, and directory synchronization.

---

## Architecture Overview

```
                      +---------------------------------------+
                      |         Client / Application          |
                      +---------------------------------------+
                                          |
                         (OpenAI API / RFC 7644 SCIM 2.0)
                                          v
+-----------------------------------------------------------------------------------+
|                            RA1 MODEL GATEWAY                                      |
|                                                                                   |
|  +--------------------+   +--------------------+   +---------------------------+  |
|  |  Virtual Key Auth  |-->|   Policy Engine    |-->|    Guardrail Moderation   |  |
|  | (RBAC & Key Quota) |   | (CIDRs, Glob, Caps)|   | (PII Masking, Injection)  |  |
|  +--------------------+   +--------------------+   +---------------------------+  |
|                                                                   |               |
|                                                                   v               |
|  +--------------------+   +--------------------+   +---------------------------+  |
|  |   Exact Response   |<--|     CEL Rules      |<--|     Session Affinity      |  |
|  |       Cache        |   | (Dynamic Rewrites) |   |    (Thread/User State)    |  |
|  +--------------------+   +--------------------+   +---------------------------+  |
|                                                                   |               |
|                                                                   v               |
|  +--------------------+   +--------------------+   +---------------------------+  |
|  |  Circuit Breakers  |<--|  Routing Strategy  |<--|     Prepaid Wallet        |  |
|  |  (Fast Failover)   |   | (Latency/Cost/Ada) |   |   (Two-Phase Hold/Settle) |  |
|  +--------------------+   +--------------------+   +---------------------------+  |
|                                     |                                             |
|                                     v                                             |
|  +-----------------------------------------------------------------------------+  |
|  |                   Provider Adapters & Resilient Key Pool                    |  |
|  |            (OpenAI, Anthropic Claude, Google Gemini, Generic REST)          |  |
|  +-----------------------------------------------------------------------------+  |
|                                     |                                             |
|  +----------------------------------v------------------------------------------+  |
|  |     Tamper-Evident SHA-256 Merkle Audit Log & Sealed Secrets KMS Vault      |  |
|  +-----------------------------------------------------------------------------+  |
+-----------------------------------------------------------------------------------+
                                      |
         +----------------------------+----------------------------+
         |                            |                            |
         v                            v                            v
  [OpenAI APIs]               [Anthropic Claude]            [Google Gemini]
```

---

## Core Capabilities by Wave

### Wave 1: Core Foundation & Provider Adapters
- **OpenAI-Compatible Ingress:** Full support for `/v1/chat/completions` (JSON & SSE streaming) and `/v1/models`.
- **Unified Provider Adapters:** Clean-room native implementations for OpenAI, Anthropic, Google Gemini, and Generic HTTP providers.
- **Resilient Key Pool & Rotation:** Automatic exponential backoff, rate-limit cooldown, and error-classified rotation across API key pools.
- **Two-Phase Prepaid Org Wallets:** High-precision atomic micro-dollar (`1/1,000,000 USD`) reservation and settlement.
- **Accurate Pricing Registry:** Dynamic token cost calculation for major frontier and efficient models.
- **Prometheus Observability:** Native `/metrics` exposing latency histograms, TTFT (time-to-first-token), request counts, and token throughput.

### Wave 2: Advanced Routing, Resiliency & Dynamic Config
- **5 High-Performance Routing Strategies:**
  - `lowest-latency`: Historical EWMA latency routing.
  - `lowest-cost`: Dynamic token price optimization.
  - `least-busy`: Concurrency-weighted load dispatching.
  - `headroom`: TPM/RPM quota-aware dispatching.
  - `adaptive`: Multi-metric weighted composite scoring.
- **Google CEL Rule Engine:** Dynamic model rewriting and header injection based on request context.
- **Circuit Breakers & Instant Failover:** Sub-millisecond open-circuit short-circuiting to prevent cascading upstream outages.
- **Exact-Match In-Memory Cache:** SHA-256 hashed prompt response cache delivering sub-millisecond cache hits.
- **Session Affinity:** Sticky sessions bound to session, thread, or user IDs with automatic unbinding on provider failure.
- **Hot Runtime Configuration:** Dynamic atomic reload via `/admin/v1/reload` without downtime.

### Wave 3: Enterprise Governance, Security & Compliance
- **Content Safety Guardrails:**
  - Real-time PII detection and redaction (SSN, RFC email, Luhn-verified credit card numbers).
  - Secret scanning (AWS keys, private keys, bearer tokens).
  - Prompt injection and jailbreak defense.
- **Cryptographic Hash-Chained Audit Ledger:** Append-only SHA-256 sequence-linked Merkle log with cryptographic tamper verification via `/admin/v1/audit/verify`.
- **Fine-Grained Policy Engine:** Scope-based model glob allowlists, token boundary caps, IP CIDR boundaries, and mandatory auditing headers.
- **Enterprise RBAC:** 4-tier permission matrix (`Owner`, `Admin`, `Engineer`, `Auditor`/`ReadOnly`).
- **RFC 7644 SCIM 2.0 Directory Sync:** REST endpoints at `/scim/v2/Users` and `/scim/v2/Groups` with automatic user de-provisioning and virtual key revocation.
- **Sealed Secrets KMS Vault:** AES-256-GCM envelope encryption with authenticated additional data (AAD) tenant isolation.

---

## Benchmarks & Performance SLA

All micro-benchmarks and concurrent stress tests were verified under Go 1.24 on Windows amd64:

| Feature / Subsystem | Target SLA | Benchmark Performance | Multiplier vs SLA |
| :--- | :--- | :--- | :--- |
| **PII Redaction Latency** | < 5.0 ms | **42.5 µs/op** (avg 67.8 µs under load) | **~73x faster** |
| **KMS Envelope Encryption** | < 1.0 ms | **6.79 µs/op** (147,188 ops/sec) | **~147x faster** |
| **Audit Ledger Append** | < 1.0 ms | **7.18 µs/op** (139,275 ops/sec) | **~139x faster** |
| **Policy Engine Evaluation** | < 100 µs | **2.79 µs/op** (358,371 ops/sec) | **~35x faster** |
| **Exact Cache Hit Overhead**| < 1.0 ms | **0.02 ms** | **~50x faster** |
| **Circuit Breaker Failover** | < 50.0 ms | **< 1.0 ms** | **~50x faster** |

Full benchmark documentation is available in [`docs/benchmarks/`](docs/benchmarks/).

---

## Clean-Room Engineering & Licensing Policy

This repository adheres to strict clean-room engineering principles:
1. **Zero Copied Code:** All behavioral specifications were drafted clean-room from reference behavior.
2. **Permissive Open Source Licenses Only:** Unconditional compliance verified via automated license audit (`go-licenses`).
   - Allowed licenses: `Apache-2.0`, `MIT`, `BSD-2-Clause`, `BSD-3-Clause`, `ISC`, `MPL-2.0`.
   - Strictly prohibited: `GPL`, `LGPL`, `AGPL`, `SSPL`, `BUSL`, and commercial-restriction licenses.
3. Complete provenance audit trail is maintained in [`docs/provenance.md`](docs/provenance.md).

---

## Quickstart

### Prerequisites
- Go 1.24+ installed
- Git

### Build & Run Locally
```bash
# Clone the repository
git clone https://github.com/ra1/model-gateway.git
cd model-gateway

# Run full test suite across all modules
go test ./...

# Run the gateway service (starts on :8080 by default)
go run ./cmd/gateway
```

### Making a Test Inference Request
```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-gw-default-key-1234567890abcdef" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [
      {"role": "user", "content": "Hello, RA1 Brain Gateway!"}
    ]
  }'
```

### Verifying Audit Chain Cryptographic Integrity
```bash
curl http://localhost:8080/admin/v1/audit/verify
# Response: {"valid": true}
```

---

## License

Licensed under the [Apache License, Version 2.0](LICENSE).  
Third-party dependency notices are available in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

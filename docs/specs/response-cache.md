# Specification: Exact & Semantic Response Cache

**Status:** Draft (Wave 2)  
**Layer:** Request Acceleration & Caching (`internal/cache`)  
**Design Reference Mode:** `BOTH`  

---

## 1. Purpose & Overview

LLM queries frequently contain repetitive requests (e.g. system prompt identical classifications, customer support FAQs, batch evaluation runs). Caching identical or semantically equivalent responses eliminates upstream API costs, frees rate limits, and lowers response latency from hundreds of milliseconds to under **0.5 ms**.

The gateway provides a dual caching engine:
1. **Exact-Match Cache:** Cryptographically hashed normalized request lookup.
2. **Semantic Cache:** Vector similarity matching using cosine similarity with a strict default threshold of **0.95**.

---

## 2. Architecture & Algorithms

### 2.1 Exact-Match Cache
- Normalizes the request payload:
  - Canonical JSON serialization of `(model, messages, temperature, top_p, tools)`.
  - Whitespace-trimmed text contents.
- Computes SHA-256 hash:
  $$\text{CacheKey} = \text{SHA256}(\text{TenantID} + ":" + \text{NormalizedPayload})$$
- Returns cached response immediately with header:
  - `X-Gateway-Cache: HIT`
  - `X-Gateway-Cache-Type: exact`

### 2.2 Semantic Cache (Threshold 0.95)
- For natural language prompts where exact characters differ slightly but semantics match:
  1. Computes embedding vector $\vec{v}_q$ for the user's latest prompt.
  2. Queries in-memory vector index for top nearest neighbor $\vec{v}_k$ within the same model and system prompt scope.
  3. Computes cosine similarity:
     $$\cos(\theta) = \frac{\vec{v}_q \cdot \vec{v}_k}{\|\vec{v}_q\| \|\vec{v}_k\|}$$
  4. If $\cos(\theta) \ge 0.95$, returns cached response with:
     - `X-Gateway-Cache: HIT`
     - `X-Gateway-Cache-Type: semantic`
     - `X-Gateway-Cache-Similarity: 0.962`
  5. If $\cos(\theta) < 0.95$, proceeds to live upstream call and indexes the resulting pair.

### 2.3 Eviction & Memory-Linked Expiry
- Fixed-capacity Least Recently Used (LRU) cache.
- Memory-pressure eviction: monitors `runtime.MemStats.Alloc`. If heap memory exceeds `max_cache_bytes`, the oldest 10% of entries are proactively evicted.
- Per-entry TTL (default: 1 hour).

---

## 3. Cache Invalidation & Controls

- Callers can bypass cache by sending:
  - Header `Cache-Control: no-cache` or `X-Gateway-Cache-Bypass: true`.
- Zero-cost billing: Cached hits bypass upstream wallet cost deduction or deduct only a nominal cache-serving fee.

---

## 4. Acceptance Tests

1. `TestCache_ExactMatch_ReturnsCachedResponse`
2. `TestCache_SemanticMatch_ExceedsThreshold095`
3. `TestCache_SemanticMatch_RejectsBelowThreshold095`
4. `TestCache_BypassHeader_ForcesLiveCall`
5. `TestCache_LRUEvictionUnderMemoryPressure`

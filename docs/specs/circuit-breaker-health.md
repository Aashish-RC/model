# Specification: Health Probing & Circuit Breaking

**Status:** Draft (Wave 2)  
**Layer:** Provider Health & Resiliency (`internal/circuitbreaker`)  
**Design Reference Mode:** `BOTH`  

---

## 1. Purpose & Overview

When upstream LLM provider endpoints experience severe degradation, DNS resolution failure, network partitions, or full outages, continuing to send requests increases caller latency, exhausts local connection pools, and degrades overall gateway throughput.

The Circuit Breaker subsystem monitors upstream provider health using a combination of **passive failure tracking** and **active background health probing**. Degraded providers are isolated early, protecting the system and enabling fast failover to backup models.

---

## 2. Tri-State Finite State Machine

```
      +---------+  Consecutive Failures >= Threshold  +------+
      | CLOSED  | ----------------------------------> | OPEN |
      +---------+                                     +------+
           ^                                             |
           | Success >= RecoveryCount                    | Sleep Window Expired
           |                                             v
      +-----------+                               +-----------+
      | HALF-OPEN | <---------------------------- | HALF-OPEN |
      +-----------+     Canary Probe Allowed      +-----------+
```

1. **Closed (Healthy):**
   - Normal operation. All requests pass through to the provider.
   - Failures (5xx, timeouts, connection refused) increment a failure counter.
   - Transient 400 (caller error) and 401 (bad credentials) do NOT trip the provider circuit (they are handled at key level).
   - If consecutive failures reach `FailureThreshold` (default: 5), circuit transitions to **Open**.

2. **Open (Tripped):**
   - All inbound requests immediately fail fast or skip to fallback models without making network calls.
   - Persists for `CooloffPeriod` (default: 30 seconds).
   - Metrics emit `circuit_breaker_tripped_total{provider="..."}`.

3. **Half-Open (Canary Probing):**
   - After `CooloffPeriod` expires, the circuit enters Half-Open state.
   - Allows a controlled trickle of traffic (e.g. 1 request or 1 background probe) through as a canary.
   - If the canary succeeds, circuit resets to **Closed** and resets failure counters.
   - If the canary fails, circuit transitions back to **Open** with doubled cooloff period (exponential backoff).

---

## 3. Active Background Health Prober

In addition to passive request-driven checks, an active health prober periodically executes lightweight health checks:
- Probes `/v1/models` or lightweight ping endpoint every `ProbeInterval` (default: 60s).
- When a circuit is Open, prober frequency increases to detect provider recovery proactively without requiring real user requests to act as canaries.

---

## 4. Configuration Schema

```json
{
  "circuit_breaker": {
    "failure_threshold": 5,
    "success_threshold": 2,
    "cooloff_duration_ms": 30000,
    "max_cooloff_ms": 300000,
    "active_probe_interval_ms": 60000
  }
}
```

---

## 5. Acceptance Tests

1. `TestCircuitBreaker_TripsAfterThresholdFailures`
2. `TestCircuitBreaker_FastFailsInOpenState`
3. `TestCircuitBreaker_TransitionsToHalfOpenAfterCooloff`
4. `TestCircuitBreaker_RecoversToClosedOnCanarySuccess`
5. `TestCircuitBreaker_Caller4xxDoesNotTripCircuit`

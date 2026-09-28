# Specification: Routing Strategies (Latency, Cost, Least-Busy, TPM/RPM-Aware)

**Status:** Draft (Wave 2)  
**Layer:** Data Plane Router & Dispatcher (`internal/routing`)  
**Design Reference Mode:** `BOTH`  

---

## 1. Purpose & Overview

The routing strategies engine determines which upstream provider, deployment target, or credential key receives an incoming request when multiple candidate targets are capable of serving the model.

Rather than relying purely on random choice or static ordering, the system provides four specialized, pluggable selection strategies:
1. **Lowest Latency:** Routes to the target displaying the lowest Exponential Moving Average (EMA) latency (TTFT for streaming, total duration for synchronous).
2. **Lowest Cost:** Routes to the target with the minimum micro-dollar cost per token derived from the pricing registry.
3. **Least Busy:** Routes to the target with the minimum number of concurrent in-flight requests.
4. **TPM/RPM Headroom:** Routes to the target with the highest available capacity headroom before hitting rate limits.

---

## 2. Architecture & Algorithms

### 2.1 Lowest Latency Strategy
- Maintains a thread-safe sliding window or EMA of response latencies:
  $$\text{EMA}_{t} = \alpha \cdot \text{latency} + (1 - \alpha) \cdot \text{EMA}_{t-1}$$
  Default smoothing factor $\alpha = 0.25$.
- For streaming completions, the tracked sample is **Time-to-First-Token (TTFT)**.
- For non-streaming completions, the sample is the total request elapsed duration.
- Outlier damping: latencies resulting from network errors (timeouts, 5xx) are excluded from the latency EMA and instead reported to the circuit breaker.

### 2.2 Lowest Cost Strategy
- Queries the pricing registry for each candidate target's active model rates:
  $$\text{EstimatedCost} = (\text{EstPromptTokens} \times \text{InputPriceMicro}) + (\text{EstCompletionTokens} \times \text{OutputPriceMicro})$$
- Ties are broken by least-busy or headroom.

### 2.3 Least Busy Strategy
- An atomic integer (`atomic.Int64`) tracks `ActiveRequests` per target.
- Incremented immediately before dispatch; decremented in a deferred block upon response body closure or stream termination.
- Lowest `ActiveRequests` wins.

### 2.4 TPM/RPM Headroom Strategy
- Computes available headroom ratio:
  $$H = \min\left(1.0 - \frac{\text{CurrentRPM}}{\text{LimitRPM}}, 1.0 - \frac{\text{CurrentTPM}}{\text{LimitTPM}}\right)$$
- Selects the target with maximum $H > 0$. If all targets have $H \le 0$, initiates fallback chain.

---

## 3. Inputs & Outputs

- **Input:**
  - Candidate targets list: `[]*Target`
  - Context & canonical chat request
  - Strategy selector: `StrategyType` (`"lowest_latency"`, `"lowest_cost"`, `"least_busy"`, `"headroom"`)
- **Output:**
  - Selected target: `*Target`
  - Error: `ErrNoEligibleTarget` if all targets are cooling off, tripped by circuit breaker, or at zero headroom.

---

## 4. Edge Cases & Failure Behavior

1. **Cold Start / Zero Samples:** If a target has no latency samples yet, it receives an optimistic median latency bonus or is selected via round-robin to warm up baseline metrics.
2. **Ties:** When multiple targets have identical metrics, tie-breaking uses pseudo-random jitter to prevent herd effects.
3. **Dynamic Re-Weighting:** Targets marked in cooldown or half-open circuit states are deprioritized or excluded.

---

## 5. Acceptance Tests

1. `TestRouting_LowestLatency_SelectsFastestTarget`
2. `TestRouting_LowestCost_SelectsCheapestProvider`
3. `TestRouting_LeastBusy_TracksInFlightConcurrently`
4. `TestRouting_Headroom_AvoidsSaturatedKey`
5. `TestRouting_ColdStartTargetExploration`

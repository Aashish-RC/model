# Specification: Adaptive Dynamic Routing

**Status:** Draft (Wave 2)  
**Layer:** Data Plane Router (`internal/routing`)  
**Design Reference Mode:** `BOTH`  

---

## 1. Purpose & Overview

Adaptive Routing dynamically balances multiple conflicting optimization objectives—latency, cost, error frequency, and rate limit headroom—into a single normalized composite health score per target.

In production environments, routing solely by cost risks routing into degraded/slow endpoints; routing solely by latency may incur astronomical provider bills. Adaptive routing continuously updates target weights to optimize quality-of-service while respecting organizational cost constraints.

---

## 2. Mathematical Scoring Model

For each eligible target $i$, the adaptive score $S_i \in [0.0, 1.0]$ is computed as:

$$S_i = w_L \cdot (1 - \hat{L}_i) + w_C \cdot (1 - \hat{C}_i) + w_E \cdot (1 - E_i) + w_H \cdot H_i$$

Where:
- $\hat{L}_i \in [0, 1]$: Min-max normalized latency across candidate pool.
- $\hat{C}_i \in [0, 1]$: Min-max normalized cost across candidate pool.
- $E_i \in [0, 1]$: Recent error rate over the last sliding window (e.g. 100 requests).
- $H_i \in [0, 1]$: Available rate-limit headroom ratio.
- $w_L, w_C, w_E, w_H$: Configurable weights summing to $1.0$ (Default: $w_L=0.35, w_C=0.25, w_E=0.25, w_H=0.15$).

The target with the highest composite score $S_i$ is chosen for dispatch.

---

## 3. Dynamic Adaptation & Exploration

1. **Exploitation vs Exploration (Epsilon-Greedy):**
   - With probability $1 - \epsilon$ (default $\epsilon = 0.05$), the router exploits the highest-scoring target.
   - With probability $\epsilon$, the router randomly samples another healthy target to continually discover latency/cost shifts.
2. **Error Penalty Ramp:**
   - Any 5xx or timeout exponentially increases $E_i$, temporarily degrading $S_i$ to divert traffic without waiting for full circuit trip.
3. **Decay Window:**
   - Error penalties decay exponentially over time if no further errors occur.

---

## 4. Configuration Schema

```json
{
  "adaptive_routing": {
    "enabled": true,
    "weights": {
      "latency": 0.35,
      "cost": 0.25,
      "error_rate": 0.25,
      "headroom": 0.15
    },
    "exploration_epsilon": 0.05,
    "sliding_window_size": 100
  }
}
```

---

## 5. Acceptance Tests

1. `TestAdaptiveRouting_CompositeScoreCalculation`
2. `TestAdaptiveRouting_HighErrorTargetDivertsTraffic`
3. `TestAdaptiveRouting_EpsilonGreedyExploration`
4. `TestAdaptiveRouting_WeightsNormalization`

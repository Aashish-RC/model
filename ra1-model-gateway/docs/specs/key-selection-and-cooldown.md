# Specification: Key Selection and Cooldown

## 1. Purpose
The Key Pool Manager ensures optimal utilization and high availability across pools of upstream provider API keys. It directs traffic to the credential with the highest remaining capacity (headroom-based routing), isolates enterprise dedicated keys from the shared public pool, and quarantines failing keys with an expanding (growing) cooldown penalty that automatically heals as keys recover.

## 2. Design Strategy & Label: OWN
Follow the locked custom design:
1. **Headroom-Aware Selection**: Requests are dispatched to the key that currently possesses the highest headroom relative to its RPM (requests per minute) and TPM (tokens per minute) ceilings.
2. **Growing Cooldown**: When a key triggers a rate limit or credential failure, it enters a quarantine state. Repeated failures multiply the cooldown duration exponentially.
3. **Automatic Rejoin**: Once cooldown expires, the key rejoins the eligible pool automatically without requiring operator intervention or manual probes.
4. **Pool Separation**: Enterprise tenants configured with dedicated credential pools never share keys with the multi-tenant shared pool.

## 3. Architecture & Data Structures

```go
type KeyPoolType string

const (
    PoolShared     KeyPoolType = "shared"
    PoolEnterprise KeyPoolType = "enterprise_dedicated"
)

type UpstreamKey struct {
    ID               string
    Provider         string
    APIKey           string
    PoolType         KeyPoolType
    EnterpriseOrgID  string          // Bound org for dedicated pools
    LimitRPM         int
    LimitTPM         int
    CurrentRPM       atomic.Int64
    CurrentTPM       atomic.Int64
    State            KeyState        // Active, CoolingDown, Disabled
    ConsecutiveFails atomic.Int32
    CooldownUntil    atomic.Int64    // Unix timestamp (nanoseconds)
    LastSuccess      time.Time
}
```

## 4. Headroom Scoring Formula
The headroom score $H$ for an active key is computed as:
$$H_{RPM} = 1.0 - \frac{\text{CurrentRPM}}{\text{LimitRPM}}$$
$$H_{TPM} = 1.0 - \frac{\text{CurrentTPM}}{\text{LimitTPM}}$$
$$\text{HeadroomScore} = \min(H_{RPM}, H_{TPM})$$

- If a key has no explicit limits configured, it is assigned a baseline score equivalent to $1.0 - (\text{InFlight} \times 0.05)$.
- The key selector iterates through eligible keys in the target pool (matching provider and org scope) and picks $\arg\max(\text{HeadroomScore})$.

## 5. Growing Cooldown Lifecycle

When a key encounters a rate limit (429) or transient provider failure:
1. `ConsecutiveFails` is incremented atomically.
2. Base cooldown duration: $T_0 = 10 \text{ seconds}$ (or provider `Retry-After` if larger).
3. Cooldown formula:
   $$\text{CooldownDuration} = \min(T_{\max}, T_0 \times 2^{(\text{ConsecutiveFails} - 1)})$$
   where $T_{\max} = 30 \text{ minutes}$.
4. Key state transitions to `KeyStateCoolingDown`.
5. `CooldownUntil` is set to `now + CooldownDuration`.

### Automatic Rejoin Mechanism:
When `SelectKey()` checks keys:
- If `now >= CooldownUntil`, the key is marked tentatively eligible.
- When a request sent to this key succeeds:
  - `ConsecutiveFails` is reset to 0.
  - State transitions back to `KeyStateActive`.
- If the first request after cooldown fails again:
  - `ConsecutiveFails` increments further, doubling the next cooldown.

## 6. Enterprise Isolation
- When a request originates from an Enterprise organization with dedicated keys:
  - The router filters solely keys where `PoolType == PoolEnterprise && EnterpriseOrgID == req.OrgID`.
  - Under no circumstances does an enterprise request fall back into the shared pool, nor does a shared tenant ever touch enterprise keys.
- If all dedicated keys for an enterprise are cooling down, the fallback chain moves to the next configured fallback model within the enterprise's dedicated pool.

## 7. Edge Cases & Concurrency
- **All Keys Cooling Down**: If every key in the pool is in cooldown, the selector identifies the key with the nearest `CooldownUntil` timestamp. If that timestamp is within a small threshold (e.g. $\le 200\text{ms}$), the selector may wait; otherwise, it immediately signals exhaustion to trigger model fallback.
- **Concurrent Selection**: Atomically tracks rolling usage buckets (1-second sliding windows) to prevent multiple concurrent requests from picking the same key simultaneously.

## 8. Configuration
- `keypool.base_cooldown_seconds`: Initial cooldown (default: 10).
- `keypool.max_cooldown_seconds`: Cooldown ceiling (default: 1800).
- `keypool.backoff_multiplier`: Exponential base (default: 2.0).

## 9. Acceptance Tests
1. **Highest Headroom Selection**: Populate pool with Key A (90% capacity left) and Key B (30% capacity left); assert Key A is selected.
2. **Growing Cooldown Multiplication**: Simulate 3 consecutive 429 errors on a key; assert cooldown durations follow $10s \to 20s \to 40s$.
3. **Automatic Rejoin Verification**: Fast-forward time past `CooldownUntil`; assert key is re-admitted to the candidate pool and resets fail counter upon success.
4. **Enterprise Pool Segregation**: Send requests with Org 1 (enterprise) and Org 2 (shared); verify that Org 1 never accesses shared keys and Org 2 never accesses Org 1's dedicated keys.

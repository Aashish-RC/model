# Specification: Retry and Backoff Policy

## 1. Purpose
The Retry and Backoff Engine governs transient error recovery across network hiccups and upstream server overloads. It applies decorrelated exponential jitter to prevent thundering herd problems, strictly honors upstream `Retry-After` headers, and ensures that retry delays respect client request deadlines.

## 2. Design Strategy & Label: BIF
Adopt the Go reference's robust backoff policy:
- Parse and honor standard `Retry-After` (in seconds or HTTP date) and provider-specific headers (`retry-after-ms`, `x-ratelimit-reset-requests`, `x-ratelimit-reset-tokens`).
- Use Full Jitter exponential backoff for unguided retries.
- Bound total retry attempts per request and overall retry time window.
- Distinguish between **same-key retries** (for transient 5xx/529) and **different-key retries** (for rotated 429s).

## 3. Backoff Formula & Timing Parameters
For unguided retries (when no provider hint exists):
$$\text{delay} = \text{random}(0, \min(\text{max\_backoff}, \text{base\_backoff} \times 2^{\text{attempt}}))$$

### Default Parameters:
- `base_backoff`: 250 milliseconds.
- `max_backoff`: 10,000 milliseconds (10 seconds).
- `max_retries_per_key`: 3 attempts.
- `max_total_retries`: 5 attempts across all keys and models.
- `jitter_factor`: Full Jitter (uniform random between 0 and calculated exponential delay).

## 4. Provider Retry Hint Parsing

The engine checks headers in the following priority order:
1. `retry-after-ms`: Integer milliseconds (e.g. `"500"` -> 500ms).
2. `retry-after`: Integer seconds (e.g. `"2"` -> 2000ms) or RFC1123 / RFC850 date string.
3. `x-ratelimit-reset-requests`: Duration or timestamp until quota reset.
4. `x-ratelimit-reset-tokens`: Duration until token bucket replenish.

### Safety Guards on Hints:
- **Minimum Enforced Delay**: 50ms (prevents tight loops on `retry-after: 0`).
- **Maximum Allowed Delay**: 30 seconds. If a provider hint exceeds 30 seconds, the request does not sleep; instead, the key is immediately placed into cooldown and the call rotates to another key.
- **Context Deadline Check**: If `now + delay >= ctx.Deadline()`, abort immediately with `context.DeadlineExceeded` rather than sleeping uselessly.

## 5. Lifecycle Flow
```
[Execution Failure]
        │
        ▼
[Error Classifier]
        │
        ├──> TransientServer (5xx / 529)
        │         │
        │         ▼
        │    Check same-key retry count < max_retries_per_key
        │         ├── Yes: Sleep(backoff) -> Retry on SAME key
        │         └── No: Escalate to Key Rotation or Fallback
        │
        └──> RateLimited (429)
                  │
                  ▼
             Parse Retry-After Hint
                  │
             Put failing key into Cooldown(hint or exponential)
                  │
             Select NEXT key with headroom
                  │
             Execute immediately on new key (zero delay)
```

## 6. Edge Cases
- **Client Cancellation During Backoff**: All sleeps are executed via `select { case <-time.After(delay): case <-ctx.Done(): return ctx.Err() }`. A cancelled client aborts the backoff instantaneously.
- **Clock Skew on Date Headers**: If upstream server clock skew produces a negative delay, default to `base_backoff`.

## 7. Configuration
- `retry.max_retries`: Global retry limit (default: 3).
- `retry.initial_delay_ms`: Initial backoff delay (default: 250).
- `retry.max_delay_ms`: Ceiling for exponential backoff (default: 10000).
- `retry.respect_retry_after`: Boolean flag (default: true).

## 8. Acceptance Tests
1. **Exponential Jitter Distribution**: Execute 1,000 backoff calculations; assert delay values are strictly bounded by `max_backoff` and exhibit expected variance.
2. **Retry-After Seconds Parsing**: Feed `Retry-After: 3` header; verify sleep interval is 3,000ms ± 50ms.
3. **Retry-After Milliseconds Parsing**: Feed `retry-after-ms: 450`; verify sleep interval is 450ms.
4. **Deadline Abortion**: Provide a context with 500ms remaining and a 1,000ms retry hint; assert immediate exit with deadline error without waiting.

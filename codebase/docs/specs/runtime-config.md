# Specification: Dynamic Runtime Configuration

**Status:** Draft (Wave 2)  
**Layer:** Management Plane & Live Config (`internal/config`)  
**Design Reference Mode:** `BOTH`  

---

## 1. Purpose & Overview

In a sealed, mission-critical model gateway, updating model pricing, adding/rotating provider credentials, changing fallback order, or adjusting rate limit parameters must never require restarting the running daemon or dropping active in-flight TCP connections.

The Dynamic Runtime Configuration subsystem enables hot-reloading gateway configurations via:
1. OS signal handling (`SIGHUP`)
2. Local administrative control endpoint (`POST /admin/v1/reload`)
3. File watcher / Polling loop on configuration file (`config.yaml` / `config.json`)

---

## 2. Hot-Reload Architecture

### 2.1 Atomic Pointer Swapping
- Configuration state is stored in an atomic pointer (`atomic.Pointer[Config]`).
- Incoming requests load the active snapshot pointer at request start and hold it for the duration of the request:
  - Long-running streaming requests finish cleanly against the snapshot active when they began.
  - New incoming requests immediately pick up the newly swapped configuration pointer.
- Zero locks on the hot path for reading configuration.

### 2.2 Reconfigurable Subsystems
The hot reload updates:
1. **Model Fallback Chains:** Dynamic target lists and retry policies.
2. **Pricing Registry:** Model cost tables and context limits.
3. **Provider Credentials:** Adding, updating, or disabling provider API keys in the key pool without discarding active cooldown states.
4. **CEL Routing Rules:** Recompiled rule trees.
5. **Circuit Breaker Thresholds:** Failure and cooloff settings.

---

## 3. Validation & Safe Rollback

1. **Two-Phase Commit Validation:**
   - The new configuration is fully parsed, type-checked, and validated (CEL expressions compiled, regexes verified, endpoints checked) before touching active state.
   - If any syntax or structural error exists, the reload is aborted with an error log, and the running configuration continues operating without disruption.
2. **Audit & Event Emission:**
   - Emits log message: `Config hot-reloaded successfully (revision: <hash>)`.
   - Increments Prometheus counter `gateway_config_reloads_total{status="success|error"}`.

---

## 4. Acceptance Tests

1. `TestRuntimeConfig_AtomicSwapNoDowntime`
2. `TestRuntimeConfig_InvalidConfigRollback`
3. `TestRuntimeConfig_SIGHUPTrigger`
4. `TestRuntimeConfig_PreservesActiveCooldownStates`

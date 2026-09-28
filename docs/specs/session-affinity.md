# Specification: Session Affinity (Sticky Routing)

**Status:** Draft (Wave 2)  
**Layer:** Data Plane Router (`internal/routing`)  
**Design Reference Mode:** `BOTH`  

---

## 1. Purpose & Overview

Stateful AI agent workflows, multi-turn chat applications, and prompt-caching providers (such as Anthropic and OpenAI prompt caching) benefit significantly when sequential requests in the same conversation thread hit the exact same backend provider, endpoint, and credential key.

Session Affinity establishes a deterministic binding between an incoming `session_id` (or `thread_id` / `user_id`) and a specific target provider/key.

---

## 2. Key Architecture & Behavior

### 2.1 Session Identifier Resolution
The gateway resolves the session identifier from:
1. Explicit HTTP header: `X-Session-ID` or `X-Thread-ID`
2. Canonical request field: `request.user` or metadata `session_id`
3. If absent, session affinity is bypassed and standard dynamic routing applies.

### 2.2 Namespace Scoping & Isolation
Session IDs can collide between different organizations or virtual keys. All session bindings are namespaced under the virtual key's tenant:
$$\text{StorageKey} = \text{fmt.Sprintf}("session:\%s:\%s", \text{orgID}, \text{sessionID})$$

### 2.3 Binding Lifecycle
1. **Binding Creation:** When a new session arrives, standard routing selects a target. Once the request succeeds, the binding `(TargetID, KeyID, Timestamp)` is persisted with a configurable Time-To-Live (TTL, default: 30 minutes).
2. **Sticky Resolution:** On subsequent requests with the same session ID:
   - If the bound target and key are active, eligible, and not in cooldown &rarr; route directly to the bound target.
   - If the bound key is in cooldown or the target circuit is open &rarr; affinity is immediately broken, standard fallback routing selects a new healthy target, and the session is re-bound to the new target.
3. **Failure Unbinding:** A request that fails with a 5xx or 429 immediately unbinds the session so the caller is never trapped on a broken target.

---

## 3. Storage & Concurrency

- **In-Memory Store:** Thread-safe concurrent map with background TTL sweeps.
- **Distributed Ready:** Interface `SessionAffinityStore` supports Redis / KV store implementations for multi-node deployments.

---

## 4. Acceptance Tests

1. `TestSessionAffinity_SequentialCallsRouteToSameKey`
2. `TestSessionAffinity_TenantIsolationPreventsCrossTalk`
3. `TestSessionAffinity_CoolingDownKeyTriggersRebind`
4. `TestSessionAffinity_TTLExpiration`
5. `TestSessionAffinity_FailureImmediatelyUnbinds`

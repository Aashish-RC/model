# Specification: Error Classification

## 1. Purpose
The Error Classifier categorizes all upstream errors into distinct failure classes to determine whether an error is transient (infrastructure-related), credential-specific (key/account-related), or client-specific (bad request). This prevents burning healthy API keys on flaky server errors and avoids repeating doomed requests on exhausted or invalid credentials.

## 2. Design Strategy & Label: BIF
Follow the Go reference's robust, exact failure classification model:
- Transient upstream server errors (500, 502, 503, 504, 529 overloaded) retry on the **SAME key** (switching keys will not fix an overloaded server).
- Credential-specific errors (authentication failure, invalid API key, insufficient quota/balance, account restriction, rate limits) trigger key **rotation or exclusion**.
- Caller fault errors (400, 422, unparseable payload, bad tool schema) fail immediately with no retry and no key penalty.

## 3. Failure Classes
```go
type FailureClass int

const (
    // Client faults: do not retry, do not rotate key
    FailureClassCallerFault FailureClass = iota
    
    // Server flakiness: retry on SAME key
    FailureClassTransientServer
    
    // Key-specific issues: ROTATE to next key in pool
    FailureClassRateLimited
    FailureClassQuotaExceeded
    FailureClassInvalidCredential
    FailureClassRegionBlocked
    
    // Model availability: trigger fallback chain
    FailureClassModelNotFound
    FailureClassModelRetired
)
```

## 4. Classification Rules & Parsing Matrix

### 4.1 Transient Server Errors (Same Key Retry)
- **Status Codes**: `500 Internal Server Error`, `502 Bad Gateway`, `503 Service Unavailable`, `504 Gateway Timeout`.
- **Special Status**: `529 Overloaded` (Anthropic / Bedrock Mantle overloaded).
- **Network Errors**: Connection reset by peer, TLS handshake timeout, temporary DNS resolution failure.
- **Action**: Backoff and retry using the identical credential.

### 4.2 Rate Limits (Rotate Key)
- **Status Codes**: `429 Too Many Requests`.
- **Substring Patterns (case-insensitive in body/message)**:
  `"rate limit"`, `"ratelimit"`, `"too many requests"`, `"throttled"`, `"throttling"`, `"rpm exceeded"`, `"tpm exceeded"`, `"tokens per minute"`, `"requests per minute"`, `"burst_rate"`.
- **Action**: Place the key in cooldown; immediately rotate to the next key with headroom in the same pool.

### 4.3 Quota Exceeded (Exclude Key)
- **Status Codes**: `402 Payment Required`, `429` with quota message, or `400` (Anthropic credit balance).
- **Codes & Substrings**:
  `"insufficient_quota"`, `"blocked_api_access"`, `"credit balance"`, `"spend limit"`, `"prepayment credits"`, `"credits are depleted"`, `"spending cap"`, `"requires billing to be enabled"`.
- **Action**: Exclude key from pool with extended cooldown (or mark permanently disabled until admin re-enables).

### 4.4 Invalid Credential (Exclude Key)
- **Status Codes**: `401 Unauthorized`, `403 Forbidden` (when credential rejected).
- **Codes & Substrings**:
  `"invalid_api_key"`, `"UNAUTHENTICATED"`, `"authentication_error"`, `"api key not valid"`, `"api_key_invalid"`, `"api key expired"`, `"organization_restricted"`.
- **Action**: Mark key as invalid; raise alert; rotate to next available key.

### 4.5 Model Not Found / Model Deprecated (Fallback Trigger)
- **Status Codes**: `404 Not Found` (when referencing model id).
- **Codes & Substrings**:
  `"model_not_found"`, `"DeploymentNotFound"`, `"unknown_model"`, `"model_decommissioned"`, `"deprecated"`.
- **Action**: Immediately abort key rotation for this model; transition directly to next model in fallback chain.

### 4.6 Caller Fault (Fail Immediately)
- **Status Codes**: `400 Bad Request` (without quota markers), `413 Payload Too Large`, `415 Unsupported Media Type`, `422 Unprocessable Entity`.
- **Action**: Forward error directly to client without modifying key state or retrying.

## 5. Failure Behavior & Edge Cases
- **Ambiguous Status 400**: Anthropic returns 400 when credit balance is too low. The classifier checks message bodies for `"credit balance"` before defaulting a 400 to CallerFault.
- **Provider Retry Hints**: If an error header contains `retry-after` or `retry-after-ms`, this hint is attached to the error classification metadata.

## 6. Acceptance Tests
1. **529 Classification**: Assert status 529 maps to `FailureClassTransientServer` with `SameKeyRetry=true`.
2. **OpenAI Quota Check**: Assert 429 with `"insufficient_quota"` maps to `FailureClassQuotaExceeded` rather than standard rate limit.
3. **Invalid Key Rotation**: Assert 401 maps to `FailureClassInvalidCredential` with `RotateKey=true`.
4. **Caller Fault Passthrough**: Assert 422 schema validation error maps to `FailureClassCallerFault` with zero retries.

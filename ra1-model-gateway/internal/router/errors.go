package router

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ra1/model-gateway/internal/provider/openai"
)

type FailureClass int

const (
	FailureClassCallerFault FailureClass = iota
	FailureClassTransientServer
	FailureClassRateLimited
	FailureClassQuotaExceeded
	FailureClassInvalidCredential
	FailureClassModelNotFound
)

// Decision defines the recovery action to take.
type Decision struct {
	Class        FailureClass
	RetrySameKey bool
	RotateKey    bool
	TriggerFallback bool
	RetryAfter   time.Duration
}

var transientServerStatusCodes = map[int]bool{
	http.StatusInternalServerError: true,
	http.StatusBadGateway:          true,
	http.StatusServiceUnavailable:  true,
	http.StatusGatewayTimeout:       true,
	529:                            true, // Anthropic overloaded_error
}

var rateLimitPatterns = []string{
	"rate limit", "rate_limit", "ratelimit", "too many requests",
	"throttled", "throttling", "rpm exceeded", "tpm exceeded",
	"tokens per minute", "requests per minute", "burst_rate",
}

var quotaPatterns = []string{
	"insufficient_quota", "blocked_api_access", "credit balance",
	"spend limit", "prepayment credits", "credits are depleted",
	"spending cap", "requires billing to be enabled", "current quota",
	"exceeded your current quota",
}

var credentialPatterns = []string{
	"invalid_api_key", "unauthenticated", "authentication_error",
	"api key not valid", "api_key_invalid", "api key expired",
	"organization_restricted",
}

var modelNotFoundPatterns = []string{
	"model_not_found", "deploymentnotfound", "unknown_model",
	"model_decommissioned", "deprecated", "does not exist",
	"not found", "is not available",
}

// ParseRetryAfter extracts retry delay hints from headers.
func ParseRetryAfter(headers http.Header) time.Duration {
	if headers == nil {
		return 0
	}

	// 1. Check retry-after-ms
	if msStr := headers.Get("retry-after-ms"); msStr != "" {
		if ms, err := strconv.Atoi(msStr); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}

	// 2. Check standard Retry-After (seconds or date)
	if sStr := headers.Get("Retry-After"); sStr != "" {
		if s, err := strconv.Atoi(sStr); err == nil && s > 0 {
			return time.Duration(s) * time.Second
		}
		if t, err := http.ParseTime(sStr); err == nil {
			d := time.Until(t)
			if d > 0 {
				return d
			}
		}
	}

	return 0
}

// ClassifyError inspects an upstream error and determines the recovery strategy.
func ClassifyError(err error) Decision {
	if err == nil {
		return Decision{Class: FailureClassCallerFault}
	}

	upErr, ok := err.(*openai.UpstreamError)
	if !ok {
		// Generic network/timeout error -> transient server
		return Decision{
			Class:        FailureClassTransientServer,
			RetrySameKey: true,
		}
	}

	status := upErr.StatusCode
	msg := strings.ToLower(upErr.Message)
	retryHint := ParseRetryAfter(upErr.Headers)

	// 1. Check Model Not Found (Status 404 with model patterns, or message mentioning model)
	if status == http.StatusNotFound {
		for _, pat := range modelNotFoundPatterns {
			if strings.Contains(msg, pat) {
				return Decision{
					Class:           FailureClassModelNotFound,
					TriggerFallback: true,
				}
			}
		}
	}

	// 2. Check Quota Exceeded (evaluated BEFORE general 429 check because quota is reported as 429, 402, or 400)
	errType := strings.ToLower(upErr.Type)
	errCode := strings.ToLower(upErr.Code)
	if errType == "insufficient_quota" || errCode == "insufficient_quota" {
		return Decision{
			Class:     FailureClassQuotaExceeded,
			RotateKey: true,
		}
	}
	for _, pat := range quotaPatterns {
		if strings.Contains(msg, pat) {
			return Decision{
				Class:     FailureClassQuotaExceeded,
				RotateKey: true,
			}
		}
	}

	// 3. Check Credential / Auth Invalid
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		for _, pat := range credentialPatterns {
			if strings.Contains(msg, pat) {
				return Decision{
					Class:     FailureClassInvalidCredential,
					RotateKey: true,
				}
			}
		}
		if status == http.StatusUnauthorized {
			return Decision{
				Class:     FailureClassInvalidCredential,
				RotateKey: true,
			}
		}
	}

	// 4. Check Rate Limited
	if status == http.StatusTooManyRequests {
		return Decision{
			Class:      FailureClassRateLimited,
			RotateKey:  true,
			RetryAfter: retryHint,
		}
	}
	for _, pat := range rateLimitPatterns {
		if strings.Contains(msg, pat) {
			return Decision{
				Class:      FailureClassRateLimited,
				RotateKey:  true,
				RetryAfter: retryHint,
			}
		}
	}

	// Check Transient Server
	if transientServerStatusCodes[status] {
		return Decision{
			Class:        FailureClassTransientServer,
			RetrySameKey: true,
			RetryAfter:   retryHint,
		}
	}

	// Default to Caller Fault (400, 422, etc.)
	return Decision{
		Class: FailureClassCallerFault,
	}
}

package router

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ra1/model-gateway/internal/provider/openai"
)

func TestClassifyError_Scenarios(t *testing.T) {
	tests := []struct {
		name                 string
		err                  error
		expectedClass        FailureClass
		expectedSameKeyRetry bool
		expectedRotateKey    bool
		expectedFallback     bool
	}{
		{
			name: "Status 529 Overloaded",
			err: &openai.UpstreamError{
				StatusCode: 529,
				Message:    "Overloaded",
			},
			expectedClass:        FailureClassTransientServer,
			expectedSameKeyRetry: true,
			expectedRotateKey:    false,
		},
		{
			name: "Status 500 Internal Server Error",
			err: &openai.UpstreamError{
				StatusCode: 500,
				Message:    "Internal error",
			},
			expectedClass:        FailureClassTransientServer,
			expectedSameKeyRetry: true,
			expectedRotateKey:    false,
		},
		{
			name: "Status 429 Standard Rate Limit",
			err: &openai.UpstreamError{
				StatusCode: 429,
				Message:    "Rate limit exceeded",
			},
			expectedClass:        FailureClassRateLimited,
			expectedSameKeyRetry: false,
			expectedRotateKey:    true,
		},
		{
			name: "Status 429 with Insufficient Quota",
			err: &openai.UpstreamError{
				StatusCode: 429,
				Message:    "You exceeded your current quota, please check your plan and billing details.",
			},
			expectedClass:        FailureClassQuotaExceeded,
			expectedSameKeyRetry: false,
			expectedRotateKey:    true,
		},
		{
			name: "Status 401 Invalid Key",
			err: &openai.UpstreamError{
				StatusCode: 401,
				Message:    "Incorrect API key provided",
			},
			expectedClass:        FailureClassInvalidCredential,
			expectedSameKeyRetry: false,
			expectedRotateKey:    true,
		},
		{
			name: "Status 404 Model Not Found",
			err: &openai.UpstreamError{
				StatusCode: 404,
				Message:    "The model `gpt-fake` does not exist or you do not have access to it.",
			},
			expectedClass:    FailureClassModelNotFound,
			expectedFallback: true,
		},
		{
			name: "Status 400 Caller Fault",
			err: &openai.UpstreamError{
				StatusCode: 400,
				Message:    "Invalid temperature parameter",
			},
			expectedClass:        FailureClassCallerFault,
			expectedSameKeyRetry: false,
			expectedRotateKey:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := ClassifyError(tt.err)
			if decision.Class != tt.expectedClass {
				t.Errorf("expected class %v, got %v", tt.expectedClass, decision.Class)
			}
			if decision.RetrySameKey != tt.expectedSameKeyRetry {
				t.Errorf("expected RetrySameKey %v, got %v", tt.expectedSameKeyRetry, decision.RetrySameKey)
			}
			if decision.RotateKey != tt.expectedRotateKey {
				t.Errorf("expected RotateKey %v, got %v", tt.expectedRotateKey, decision.RotateKey)
			}
			if decision.TriggerFallback != tt.expectedFallback {
				t.Errorf("expected TriggerFallback %v, got %v", tt.expectedFallback, decision.TriggerFallback)
			}
		})
	}
}

func TestParseRetryAfter_Headers(t *testing.T) {
	// 1. Seconds
	h1 := make(http.Header)
	h1.Set("Retry-After", "5")
	if d := ParseRetryAfter(h1); d != 5*time.Second {
		t.Errorf("expected 5s, got %v", d)
	}

	// 2. Milliseconds
	h2 := make(http.Header)
	h2.Set("retry-after-ms", "350")
	if d := ParseRetryAfter(h2); d != 350*time.Millisecond {
		t.Errorf("expected 350ms, got %v", d)
	}
}

func TestRetryPolicy_ComputeBackoffAndCancel(t *testing.T) {
	policy := DefaultRetryPolicy()

	// Hint takes precedence
	b1 := policy.ComputeBackoff(1, 400*time.Millisecond)
	if b1 != 400*time.Millisecond {
		t.Errorf("expected 400ms from hint, got %v", b1)
	}

	// Unhinted backoff is bounded
	b2 := policy.ComputeBackoff(10, 0)
	if b2 > policy.MaxBackoff {
		t.Errorf("expected <= max backoff %v, got %v", policy.MaxBackoff, b2)
	}

	// Context cancellation aborts SleepContext
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := SleepContext(ctx, 1*time.Second)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
}

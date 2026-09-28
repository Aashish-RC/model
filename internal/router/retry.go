package router

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// RetryPolicy defines backoff timing parameters.
type RetryPolicy struct {
	BaseBackoff       time.Duration
	MaxBackoff        time.Duration
	MaxSameKeyRetries int
	MaxTotalRetries   int
}

// DefaultRetryPolicy returns production-standard retry configuration.
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		BaseBackoff:       100 * time.Millisecond,
		MaxBackoff:        5 * time.Second,
		MaxSameKeyRetries: 3,
		MaxTotalRetries:   5,
	}
}

// ComputeBackoff calculates full-jitter exponential backoff.
func (p *RetryPolicy) ComputeBackoff(attempt int, hint time.Duration) time.Duration {
	if hint > 0 {
		if hint > p.MaxBackoff {
			return p.MaxBackoff
		}
		if hint < 50*time.Millisecond {
			return 50 * time.Millisecond
		}
		return hint
	}

	maxDelay := float64(p.BaseBackoff) * math.Pow(2, float64(attempt))
	if maxDelay > float64(p.MaxBackoff) {
		maxDelay = float64(p.MaxBackoff)
	}

	// Full jitter: uniformly distributed in [0, maxDelay]
	jittered := rand.Float64() * maxDelay
	if jittered < float64(50*time.Millisecond) {
		jittered = float64(50 * time.Millisecond)
	}

	return time.Duration(jittered)
}

// SleepContext sleeps for the specified duration or aborts if the context is cancelled.
func SleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

package routing

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrNoEligibleTarget = errors.New("no eligible routing target available")
)

// StrategyType identifies a routing strategy.
type StrategyType string

const (
	StrategyLowestLatency StrategyType = "lowest_latency"
	StrategyLowestCost    StrategyType = "lowest_cost"
	StrategyLeastBusy     StrategyType = "least_busy"
	StrategyHeadroom      StrategyType = "headroom"
	StrategyAdaptive      StrategyType = "adaptive"
)

// Target represents a candidate provider/key target with live tracking metrics.
type Target struct {
	ID             string
	Provider       string
	Model          string
	KeyID          string
	CostPer1kInput int64 // Micro-dollars
	CostPer1kOut   int64 // Micro-dollars

	ActiveRequests atomic.Int64
	latencyEMA     atomic.Int64 // nanoseconds
	samplesCount   atomic.Int64

	mu         sync.RWMutex
	errors     int
	successes  int
	lastError  time.Time
	IsEligible func() bool
	HeadroomFn func() float64
}

// NewTarget initializes a candidate target.
func NewTarget(id, provider, model, keyID string, costIn, costOut int64) *Target {
	return &Target{
		ID:             id,
		Provider:       provider,
		Model:          model,
		KeyID:          keyID,
		CostPer1kInput: costIn,
		CostPer1kOut:   costOut,
		IsEligible:     func() bool { return true },
		HeadroomFn:     func() float64 { return 1.0 },
	}
}

// RecordResult records a call's duration and whether it succeeded.
func (t *Target) RecordResult(d time.Duration, success bool) {
	if success {
		// Update EMA latency with smoothing factor alpha = 0.25
		const alpha = 0.25
		prev := t.latencyEMA.Load()
		if prev == 0 {
			t.latencyEMA.Store(d.Nanoseconds())
		} else {
			newEMA := int64(alpha*float64(d.Nanoseconds()) + (1-alpha)*float64(prev))
			t.latencyEMA.Store(newEMA)
		}
		t.samplesCount.Add(1)

		t.mu.Lock()
		t.successes++
		t.mu.Unlock()
	} else {
		t.mu.Lock()
		t.errors++
		t.lastError = time.Now()
		t.mu.Unlock()
	}
}

// LatencyEMA returns the current latency EMA.
func (t *Target) LatencyEMA() time.Duration {
	return time.Duration(t.latencyEMA.Load())
}

// ErrorRate calculates recent error rate [0.0, 1.0].
func (t *Target) ErrorRate() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	total := t.successes + t.errors
	if total == 0 {
		return 0.0
	}
	return float64(t.errors) / float64(total)
}

// Strategy selects a target from candidate targets.
type Strategy interface {
	Name() StrategyType
	Select(ctx context.Context, targets []*Target) (*Target, error)
}

// LowestLatencyStrategy picks the target with lowest EMA latency.
type LowestLatencyStrategy struct {
	DefaultWarmupLatency time.Duration
}

func (s *LowestLatencyStrategy) Name() StrategyType {
	return StrategyLowestLatency
}

func (s *LowestLatencyStrategy) Select(ctx context.Context, targets []*Target) (*Target, error) {
	eligible := filterEligible(targets)
	if len(eligible) == 0 {
		return nil, ErrNoEligibleTarget
	}

	warmup := s.DefaultWarmupLatency
	if warmup <= 0 {
		warmup = 10 * time.Millisecond
	}

	var best *Target
	var bestLat time.Duration = time.Duration(math.MaxInt64)

	for _, t := range eligible {
		lat := t.LatencyEMA()
		if lat == 0 {
			lat = warmup // Give cold targets an optimistic baseline
		}
		if lat < bestLat {
			bestLat = lat
			best = t
		}
	}

	return best, nil
}

// LowestCostStrategy picks the target with lowest token cost.
type LowestCostStrategy struct{}

func (s *LowestCostStrategy) Name() StrategyType {
	return StrategyLowestCost
}

func (s *LowestCostStrategy) Select(ctx context.Context, targets []*Target) (*Target, error) {
	eligible := filterEligible(targets)
	if len(eligible) == 0 {
		return nil, ErrNoEligibleTarget
	}

	var best *Target
	var lowestCost int64 = math.MaxInt64

	for _, t := range eligible {
		avgCost := (t.CostPer1kInput + t.CostPer1kOut) / 2
		if avgCost < lowestCost {
			lowestCost = avgCost
			best = t
		}
	}

	return best, nil
}

// LeastBusyStrategy routes to the target with fewest active requests.
type LeastBusyStrategy struct{}

func (s *LeastBusyStrategy) Name() StrategyType {
	return StrategyLeastBusy
}

func (s *LeastBusyStrategy) Select(ctx context.Context, targets []*Target) (*Target, error) {
	eligible := filterEligible(targets)
	if len(eligible) == 0 {
		return nil, ErrNoEligibleTarget
	}

	var best *Target
	var minActive int64 = math.MaxInt64

	for _, t := range eligible {
		active := t.ActiveRequests.Load()
		if active < minActive {
			minActive = active
			best = t
		}
	}

	return best, nil
}

// HeadroomStrategy routes to the target with the highest available capacity headroom.
type HeadroomStrategy struct{}

func (s *HeadroomStrategy) Name() StrategyType {
	return StrategyHeadroom
}

func (s *HeadroomStrategy) Select(ctx context.Context, targets []*Target) (*Target, error) {
	eligible := filterEligible(targets)
	if len(eligible) == 0 {
		return nil, ErrNoEligibleTarget
	}

	var best *Target
	var maxHeadroom float64 = -1.0

	for _, t := range eligible {
		hr := t.HeadroomFn()
		if hr > maxHeadroom {
			maxHeadroom = hr
			best = t
		}
	}

	if best == nil || maxHeadroom <= 0 {
		return nil, ErrNoEligibleTarget
	}

	return best, nil
}

func filterEligible(targets []*Target) []*Target {
	out := make([]*Target, 0, len(targets))
	for _, t := range targets {
		if t != nil && (t.IsEligible == nil || t.IsEligible()) {
			out = append(out, t)
		}
	}
	return out
}

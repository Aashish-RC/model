package routing

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// AdaptiveWeights configures the weighting factors for multi-objective optimization.
type AdaptiveWeights struct {
	Latency  float64 // Default: 0.35
	Cost     float64 // Default: 0.25
	Error    float64 // Default: 0.25
	Headroom float64 // Default: 0.15
}

// DefaultWeights returns calibrated production weights.
func DefaultWeights() AdaptiveWeights {
	return AdaptiveWeights{
		Latency:  0.35,
		Cost:     0.25,
		Error:    0.25,
		Headroom: 0.15,
	}
}

// AdaptiveStrategy balances latency, cost, reliability, and headroom.
type AdaptiveStrategy struct {
	Weights AdaptiveWeights
	Epsilon float64 // Probability of exploring a random healthy target (default: 0.05)
	rng     *rand.Rand
}

// NewAdaptiveStrategy initializes the adaptive strategy.
func NewAdaptiveStrategy(w AdaptiveWeights, epsilon float64) *AdaptiveStrategy {
	if w.Latency+w.Cost+w.Error+w.Headroom == 0 {
		w = DefaultWeights()
	}
	if epsilon < 0 || epsilon > 1.0 {
		epsilon = 0.05
	}
	return &AdaptiveStrategy{
		Weights: w,
		Epsilon: epsilon,
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (s *AdaptiveStrategy) Name() StrategyType {
	return StrategyAdaptive
}

func (s *AdaptiveStrategy) Select(ctx context.Context, targets []*Target) (*Target, error) {
	eligible := filterEligible(targets)
	if len(eligible) == 0 {
		return nil, ErrNoEligibleTarget
	}

	if len(eligible) == 1 {
		return eligible[0], nil
	}

	// Epsilon-greedy exploration
	if s.Epsilon > 0 && s.rng.Float64() < s.Epsilon {
		idx := s.rng.Intn(len(eligible))
		return eligible[idx], nil
	}

	// Find min/max ranges for normalization
	var minLat, maxLat float64 = math.MaxFloat64, 0
	var minCost, maxCost float64 = math.MaxFloat64, 0

	for _, t := range eligible {
		lat := float64(t.LatencyEMA().Milliseconds())
		if lat < minLat {
			minLat = lat
		}
		if lat > maxLat {
			maxLat = lat
		}

		cost := float64(t.CostPer1kInput + t.CostPer1kOut)
		if cost < minCost {
			minCost = cost
		}
		if cost > maxCost {
			maxCost = cost
		}
	}

	var bestTarget *Target
	var maxScore float64 = -1.0

	for _, t := range eligible {
		score := s.computeScore(t, minLat, maxLat, minCost, maxCost)
		if score > maxScore {
			maxScore = score
			bestTarget = t
		}
	}

	if bestTarget == nil {
		return eligible[0], nil
	}
	return bestTarget, nil
}

func (s *AdaptiveStrategy) computeScore(t *Target, minLat, maxLat, minCost, maxCost float64) float64 {
	// Normalized latency in [0, 1] (0 = fastest, 1 = slowest)
	var normLat float64 = 0.5
	if maxLat > minLat {
		lat := float64(t.LatencyEMA().Milliseconds())
		normLat = (lat - minLat) / (maxLat - minLat)
	}

	// Normalized cost in [0, 1] (0 = cheapest, 1 = most expensive)
	var normCost float64 = 0.5
	if maxCost > minCost {
		cost := float64(t.CostPer1kInput + t.CostPer1kOut)
		normCost = (cost - minCost) / (maxCost - minCost)
	}

	// Error rate in [0, 1]
	errRate := t.ErrorRate()

	// Headroom in [0, 1] (1 = full capacity)
	headroom := t.HeadroomFn()

	// Composite Score: Higher is better
	score := s.Weights.Latency*(1.0-normLat) +
		s.Weights.Cost*(1.0-normCost) +
		s.Weights.Error*(1.0-errRate) +
		s.Weights.Headroom*headroom

	return score
}

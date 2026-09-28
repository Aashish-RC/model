package routing

import (
	"context"
	"testing"
	"time"
)

func TestRouting_LowestLatency_SelectsFastestTarget(t *testing.T) {
	ctx := context.Background()
	t1 := NewTarget("t1", "p1", "m1", "k1", 1000, 2000)
	t2 := NewTarget("t2", "p2", "m1", "k2", 1000, 2000)
	t3 := NewTarget("t3", "p3", "m1", "k3", 1000, 2000)

	t1.RecordResult(100*time.Millisecond, true)
	t2.RecordResult(20*time.Millisecond, true)
	t3.RecordResult(250*time.Millisecond, true)

	strat := &LowestLatencyStrategy{}
	selected, err := strat.Select(ctx, []*Target{t1, t2, t3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected.ID != "t2" {
		t.Errorf("expected t2, got %s", selected.ID)
	}
}

func TestRouting_LowestCost_SelectsCheapestProvider(t *testing.T) {
	ctx := context.Background()
	t1 := NewTarget("t1", "p1", "m1", "k1", 3000, 5000) // $8.00 avg
	t2 := NewTarget("t2", "p2", "m1", "k2", 500, 1500)   // $1.00 avg
	t3 := NewTarget("t3", "p3", "m1", "k3", 2000, 2000) // $2.00 avg

	strat := &LowestCostStrategy{}
	selected, err := strat.Select(ctx, []*Target{t1, t2, t3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected.ID != "t2" {
		t.Errorf("expected t2 (cheapest), got %s", selected.ID)
	}
}

func TestRouting_LeastBusy_TracksInFlightConcurrently(t *testing.T) {
	ctx := context.Background()
	t1 := NewTarget("t1", "p1", "m1", "k1", 1000, 1000)
	t2 := NewTarget("t2", "p2", "m1", "k2", 1000, 1000)

	t1.ActiveRequests.Store(5)
	t2.ActiveRequests.Store(1)

	strat := &LeastBusyStrategy{}
	selected, err := strat.Select(ctx, []*Target{t1, t2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected.ID != "t2" {
		t.Errorf("expected t2 (least busy), got %s", selected.ID)
	}
}

func TestRouting_Headroom_AvoidsSaturatedKey(t *testing.T) {
	ctx := context.Background()
	t1 := NewTarget("t1", "p1", "m1", "k1", 1000, 1000)
	t2 := NewTarget("t2", "p2", "m1", "k2", 1000, 1000)

	t1.HeadroomFn = func() float64 { return 0.05 } // 5% headroom left
	t2.HeadroomFn = func() float64 { return 0.85 } // 85% headroom left

	strat := &HeadroomStrategy{}
	selected, err := strat.Select(ctx, []*Target{t1, t2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected.ID != "t2" {
		t.Errorf("expected t2 (most headroom), got %s", selected.ID)
	}
}

func TestAdaptiveRouting_CompositeScoreCalculation(t *testing.T) {
	ctx := context.Background()
	// t1: Fast (10ms), Moderate cost (1000), 0 errors, 90% headroom
	t1 := NewTarget("t1", "p1", "m1", "k1", 1000, 1000)
	t1.RecordResult(10*time.Millisecond, true)
	t1.HeadroomFn = func() float64 { return 0.9 }

	// t2: Slow (300ms), Cheap (100), 50% errors, 10% headroom
	t2 := NewTarget("t2", "p2", "m1", "k2", 100, 100)
	t2.RecordResult(300*time.Millisecond, true)
	t2.RecordResult(300*time.Millisecond, false) // 1 success, 1 error = 50%
	t2.HeadroomFn = func() float64 { return 0.1 }

	strat := NewAdaptiveStrategy(DefaultWeights(), 0.0) // Epsilon=0 for deterministic test
	selected, err := strat.Select(ctx, []*Target{t1, t2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected.ID != "t1" {
		t.Errorf("expected t1 to win composite score, got %s", selected.ID)
	}
}

func TestAdaptiveRouting_HighErrorTargetDivertsTraffic(t *testing.T) {
	ctx := context.Background()
	t1 := NewTarget("t1", "p1", "m1", "k1", 1000, 1000)
	t2 := NewTarget("t2", "p2", "m1", "k2", 1000, 1000)

	// Both same latency and cost, but t1 has 90% errors
	for i := 0; i < 9; i++ {
		t1.RecordResult(20*time.Millisecond, false)
	}
	t1.RecordResult(20*time.Millisecond, true)

	// t2 has 100% successes
	for i := 0; i < 10; i++ {
		t2.RecordResult(20*time.Millisecond, true)
	}

	strat := NewAdaptiveStrategy(DefaultWeights(), 0.0)
	selected, err := strat.Select(ctx, []*Target{t1, t2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected.ID != "t2" {
		t.Errorf("expected t2 to win due to low error rate, got %s", selected.ID)
	}
}

func TestRouting_FilterEligible_ExcludesCoolingDown(t *testing.T) {
	ctx := context.Background()
	t1 := NewTarget("t1", "p1", "m1", "k1", 1000, 1000)
	t2 := NewTarget("t2", "p2", "m1", "k2", 1000, 1000)

	t1.IsEligible = func() bool { return false } // Cooling down
	t2.IsEligible = func() bool { return true }

	strat := &LowestLatencyStrategy{}
	selected, err := strat.Select(ctx, []*Target{t1, t2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected.ID != "t2" {
		t.Errorf("expected t2, got %s", selected.ID)
	}
}

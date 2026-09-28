package wallet

import (
	"fmt"
	"sync"
	"testing"
)

func TestWallet_TwoPhaseReservationAndSettlement(t *testing.T) {
	wm := NewManager()
	orgID := "org-test"
	// $10.00 = 10,000,000 micro-dollars
	wm.SetBalance(orgID, 10000000)

	// 1. Reserve $1.00 (1,000,000 micro-dollars)
	hold := int64(1000000)
	if err := wm.Reserve(orgID, hold); err != nil {
		t.Fatalf("unexpected reserve error: %v", err)
	}

	w, _ := wm.GetWallet(orgID)
	if w.ReservedMicro != hold {
		t.Errorf("expected reserved %d, got %d", hold, w.ReservedMicro)
	}
	if w.AvailableMicro() != 9000000 {
		t.Errorf("expected available 9,000,000, got %d", w.AvailableMicro())
	}

	// 2. Settle actual cost $0.40 (400,000 micro-dollars)
	actual := int64(400000)
	if err := wm.Settle(orgID, hold, actual, "req-1", "vk-1", "chat completion call"); err != nil {
		t.Fatalf("unexpected settle error: %v", err)
	}

	wAfter, _ := wm.GetWallet(orgID)
	if wAfter.ReservedMicro != 0 {
		t.Errorf("expected reserved released to 0, got %d", wAfter.ReservedMicro)
	}
	if wAfter.BalanceMicro != 9600000 {
		t.Errorf("expected balance 9,600,000, got %d", wAfter.BalanceMicro)
	}

	// 3. Verify ledger entry
	ledger := wm.GetLedgerHistory(orgID)
	if len(ledger) != 2 { // 1 credit + 1 settlement
		t.Fatalf("expected 2 ledger entries, got %d", len(ledger))
	}
	if ledger[1].AmountMicro != actual || ledger[1].BalanceAfterMicro != 9600000 {
		t.Errorf("unexpected ledger row: %+v", ledger[1])
	}
}

func TestWallet_ReleaseOnFailure(t *testing.T) {
	wm := NewManager()
	orgID := "org-fail"
	wm.SetBalance(orgID, 5000000)

	hold := int64(1000000)
	_ = wm.Reserve(orgID, hold)

	// Failure releases hold completely
	_ = wm.Release(orgID, hold)

	w, _ := wm.GetWallet(orgID)
	if w.ReservedMicro != 0 {
		t.Errorf("expected reserved to be 0 after release, got %d", w.ReservedMicro)
	}
	if w.BalanceMicro != 5000000 {
		t.Errorf("expected balance to remain intact, got %d", w.BalanceMicro)
	}
}

func TestWallet_ConcurrencyUnder1000RequestsNoOverdraft(t *testing.T) {
	wm := NewManager()
	orgID := "org-concurrency"

	// Balance: $1.00 = 1,000,000 micro-dollars
	// Each request tries to hold $0.01 = 10,000 micro-dollars
	// Exactly 100 requests can be served concurrently without overspend
	wm.SetBalance(orgID, 1000000)

	numRequests := 1000
	holdPerReq := int64(10000)
	actualCostPerReq := int64(5000)

	var wg sync.WaitGroup
	var successCount int64
	var rejectedCount int64
	var mu sync.Mutex

	wg.Add(numRequests)
	for i := 0; i < numRequests; i++ {
		go func(id int) {
			defer wg.Done()
			reqID := fmt.Sprintf("req-%d", id)

			err := wm.Reserve(orgID, holdPerReq)
			if err != nil {
				mu.Lock()
				rejectedCount++
				mu.Unlock()
				return
			}

			// Simulating successful generation & settlement
			_ = wm.Settle(orgID, holdPerReq, actualCostPerReq, reqID, "vk-conc", "concurrent call")
			mu.Lock()
			successCount++
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	w, _ := wm.GetWallet(orgID)

	// Invariant: Balance must never be negative
	if w.BalanceMicro < 0 {
		t.Fatalf("CRITICAL: Balance went negative: %d", w.BalanceMicro)
	}
	if w.ReservedMicro < 0 {
		t.Fatalf("CRITICAL: Reserved went negative: %d", w.ReservedMicro)
	}

	// Verify that sum of spent + remaining equals original balance
	totalSpent := successCount * actualCostPerReq
	if w.BalanceMicro != (1000000 - totalSpent) {
		t.Errorf("expected balance %d, got %d", 1000000-totalSpent, w.BalanceMicro)
	}

	if successCount+rejectedCount != int64(numRequests) {
		t.Errorf("expected %d total results, got %d", numRequests, successCount+rejectedCount)
	}
}

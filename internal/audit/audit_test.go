package audit

import (
	"fmt"
	"sync"
	"testing"
)

func TestAuditLog_SequentialAppend_MaintainsValidHashChain(t *testing.T) {
	ledger := NewLedger()

	for i := 0; i < 20; i++ {
		_, err := ledger.Append(
			"user-admin",
			"org-acme",
			"key.create",
			fmt.Sprintf("keys/vk-%d", i),
			map[string]string{"env": "prod", "index": fmt.Sprintf("%d", i)},
		)
		if err != nil {
			t.Fatalf("failed to append entry %d: %v", i, err)
		}
	}

	valid, err := ledger.VerifyChain()
	if err != nil || !valid {
		t.Fatalf("expected audit chain to be valid, got valid=%v, err=%v", valid, err)
	}

	entries := ledger.GetEntries(0, 50)
	if len(entries) != 20 {
		t.Errorf("expected 20 entries, got %d", len(entries))
	}
}

func TestAuditLog_TamperPayload_FailsVerification(t *testing.T) {
	ledger := NewLedger()

	for i := 0; i < 5; i++ {
		_, _ = ledger.Append("admin", "org-1", "wallet.debit", "wallet", map[string]string{"amount": "100"})
	}

	valid, err := ledger.VerifyChain()
	if !valid || err != nil {
		t.Fatalf("baseline chain must be valid")
	}

	// Tamper entry 2's metadata directly in memory
	entries := ledger.entries
	entries[2].Metadata["amount"] = "99999" // malicious balance change

	valid, err = ledger.VerifyChain()
	if valid || err == nil {
		t.Fatalf("expected verification to fail on tampered payload metadata")
	}
}

func TestAuditLog_TamperPreviousHash_FailsVerification(t *testing.T) {
	ledger := NewLedger()

	for i := 0; i < 5; i++ {
		_, _ = ledger.Append("admin", "org-1", "user.create", "users/u1", nil)
	}

	// Alter PreviousHash on entry 3
	ledger.entries[3].PreviousHash = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

	valid, err := ledger.VerifyChain()
	if valid || err == nil {
		t.Fatalf("expected verification to detect broken hash link")
	}
}

func TestAuditLog_DeletedEntry_DetectsSequenceGap(t *testing.T) {
	ledger := NewLedger()

	for i := 0; i < 5; i++ {
		_, _ = ledger.Append("admin", "org-1", "policy.update", "policies", nil)
	}

	// Delete entry at index 2 (simulating an adversary trying to erase an audit record)
	ledger.entries = append(ledger.entries[:2], ledger.entries[3:]...)

	valid, err := ledger.VerifyChain()
	if valid || err == nil {
		t.Fatalf("expected verification to detect missing deleted block")
	}
}

func TestAuditLog_ConcurrentAppend_ThreadSafe(t *testing.T) {
	ledger := NewLedger()
	var wg sync.WaitGroup

	workers := 10
	appendsPerWorker := 20

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < appendsPerWorker; i++ {
				_, _ = ledger.Append(
					fmt.Sprintf("worker-%d", workerID),
					"org-concurrency",
					"model.invoke",
					"models/gpt-4o",
					map[string]string{"req": fmt.Sprintf("%d", i)},
				)
			}
		}(w)
	}

	wg.Wait()

	if len(ledger.entries) != workers*appendsPerWorker {
		t.Fatalf("expected %d entries, got %d", workers*appendsPerWorker, len(ledger.entries))
	}

	valid, err := ledger.VerifyChain()
	if !valid || err != nil {
		t.Fatalf("concurrently appended chain must be mathematically valid: %v", err)
	}
}

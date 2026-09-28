package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

var (
	ErrChainTampered = errors.New("audit log hash chain verification failed")
)

// AuditEntry represents an individual tamper-evident ledger block.
type AuditEntry struct {
	SequenceID   int64             `json:"sequence_id"`
	Timestamp    time.Time         `json:"timestamp"`
	Actor        string            `json:"actor"`
	OrgID        string            `json:"org_id"`
	Action       string            `json:"action"`
	Resource     string            `json:"resource"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	PayloadHash  string            `json:"payload_hash"`
	PreviousHash string            `json:"previous_hash"`
	EntryHash    string            `json:"entry_hash"`
}

// ComputePayloadHash generates SHA-256 fingerprint of the event metadata.
func ComputePayloadHash(metadata map[string]string) string {
	if len(metadata) == 0 {
		sum := sha256.Sum256([]byte("{}"))
		return hex.EncodeToString(sum[:])
	}
	data, _ := json.Marshal(metadata)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ComputeEntryHash generates the block hash tying this entry to the previous block.
func ComputeEntryHash(seq int64, t time.Time, actor, orgID, action, resource, payloadHash, prevHash string) string {
	raw := fmt.Sprintf("%d:%d:%s:%s:%s:%s:%s:%s",
		seq,
		t.UnixNano(),
		actor,
		orgID,
		action,
		resource,
		payloadHash,
		prevHash,
	)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Ledger manages the append-only cryptographic audit chain.
type Ledger struct {
	mu      sync.RWMutex
	entries []*AuditEntry
}

// NewLedger initializes an in-memory audit ledger.
func NewLedger() *Ledger {
	return &Ledger{
		entries: make([]*AuditEntry, 0),
	}
}

// Append records a new tamper-evident audit event.
func (l *Ledger) Append(actor, orgID, action, resource string, metadata map[string]string) (*AuditEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().UTC()
	seq := int64(len(l.entries) + 1)

	prevHash := GenesisHash
	if len(l.entries) > 0 {
		prevHash = l.entries[len(l.entries)-1].EntryHash
	}

	payloadHash := ComputePayloadHash(metadata)
	entryHash := ComputeEntryHash(seq, now, actor, orgID, action, resource, payloadHash, prevHash)

	entry := &AuditEntry{
		SequenceID:   seq,
		Timestamp:    now,
		Actor:        actor,
		OrgID:        orgID,
		Action:       action,
		Resource:     resource,
		Metadata:     metadata,
		PayloadHash:  payloadHash,
		PreviousHash: prevHash,
		EntryHash:    entryHash,
	}

	l.entries = append(l.entries, entry)
	return entry, nil
}

// GetEntries returns a slice of audit log entries with pagination.
func (l *Ledger) GetEntries(offset, limit int) []*AuditEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if offset < 0 {
		offset = 0
	}
	if offset >= len(l.entries) {
		return []*AuditEntry{}
	}

	end := offset + limit
	if limit <= 0 || end > len(l.entries) {
		end = len(l.entries)
	}

	result := make([]*AuditEntry, end-offset)
	copy(result, l.entries[offset:end])
	return result
}

// VerifyChain validates the cryptographic integrity of the entire ledger.
func (l *Ledger) VerifyChain() (bool, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return VerifyEntries(l.entries)
}

// VerifyEntries validates any slice of audit entries for sequential hash chain integrity.
func VerifyEntries(entries []*AuditEntry) (bool, error) {
	expectedPrev := GenesisHash

	for i, entry := range entries {
		expectedSeq := int64(i + 1)
		if entry.SequenceID != expectedSeq {
			return false, fmt.Errorf("%w: sequence mismatch at index %d (expected %d, got %d)", ErrChainTampered, i, expectedSeq, entry.SequenceID)
		}

		if entry.PreviousHash != expectedPrev {
			return false, fmt.Errorf("%w: previous hash mismatch at sequence %d (expected %s, got %s)", ErrChainTampered, entry.SequenceID, expectedPrev, entry.PreviousHash)
		}

		// Recompute payload hash
		expectedPayloadHash := ComputePayloadHash(entry.Metadata)
		if entry.PayloadHash != expectedPayloadHash {
			return false, fmt.Errorf("%w: payload hash tampered at sequence %d", ErrChainTampered, entry.SequenceID)
		}

		// Recompute entry block hash
		expectedEntryHash := ComputeEntryHash(
			entry.SequenceID,
			entry.Timestamp,
			entry.Actor,
			entry.OrgID,
			entry.Action,
			entry.Resource,
			entry.PayloadHash,
			entry.PreviousHash,
		)

		if entry.EntryHash != expectedEntryHash {
			return false, fmt.Errorf("%w: entry hash signature tampered at sequence %d", ErrChainTampered, entry.SequenceID)
		}

		expectedPrev = entry.EntryHash
	}

	return true, nil
}

package wallet

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrInsufficientBalance = errors.New("organization prepaid wallet balance is exhausted")
	ErrWalletNotFound      = errors.New("organization wallet not found")
	ErrWalletFrozen        = errors.New("organization wallet is frozen")
	ErrInvalidAmount       = errors.New("invalid reservation or settlement amount")
)

type TransactionType string

const (
	TxReservation TransactionType = "RESERVATION"
	TxSettlement  TransactionType = "SETTLEMENT"
	TxRelease     TransactionType = "RELEASE"
	TxCredit      TransactionType = "CREDIT"
)

// LedgerEntry represents an immutable financial transaction row.
type LedgerEntry struct {
	ID                string          `json:"id"`
	OrgID             string          `json:"org_id"`
	RequestID         string          `json:"request_id"`
	VirtualKeyID      string          `json:"virtual_key_id"`
	TransactionType   TransactionType `json:"transaction_type"`
	AmountMicro       int64           `json:"amount_micro"`
	BalanceAfterMicro int64           `json:"balance_after_micro"`
	Description       string          `json:"description"`
	CreatedAt         time.Time       `json:"created_at"`
}

// Wallet maintains the financial balances for an organization.
type Wallet struct {
	OrgID         string
	BalanceMicro  int64 // Total funds in micro-dollars
	ReservedMicro int64 // Active holds in micro-dollars
	IsFrozen      bool
}

// AvailableMicro returns the spendable balance.
func (w *Wallet) AvailableMicro() int64 {
	avail := w.BalanceMicro - w.ReservedMicro
	if avail < 0 {
		return 0
	}
	return avail
}

// Manager implements the atomic two-phase wallet protocol and ledger recording.
type Manager struct {
	mu      sync.Mutex
	wallets map[string]*Wallet
	ledger  []LedgerEntry
}

// NewManager creates an in-memory thread-safe wallet manager.
func NewManager() *Manager {
	return &Manager{
		wallets: make(map[string]*Wallet),
		ledger:  make([]LedgerEntry, 0),
	}
}

// SetBalance initializes or tops up an organization wallet.
func (m *Manager) SetBalance(orgID string, balanceMicro int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, exists := m.wallets[orgID]
	if !exists {
		w = &Wallet{OrgID: orgID}
		m.wallets[orgID] = w
	}
	w.BalanceMicro = balanceMicro
	w.ReservedMicro = 0

	m.ledger = append(m.ledger, LedgerEntry{
		ID:                fmt.Sprintf("tx-credit-%d", time.Now().UnixNano()),
		OrgID:             orgID,
		TransactionType:   TxCredit,
		AmountMicro:       balanceMicro,
		BalanceAfterMicro: w.BalanceMicro,
		Description:       "Initial wallet balance credit",
		CreatedAt:         time.Now(),
	})
}

// GetWallet retrieves wallet balances.
func (m *Manager) GetWallet(orgID string) (*Wallet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, exists := m.wallets[orgID]
	if !exists {
		return nil, ErrWalletNotFound
	}
	copied := *w
	return &copied, nil
}

// Reserve executes Phase 1: atomically places a pre-call hold on funds.
func (m *Manager) Reserve(orgID string, holdAmountMicro int64) error {
	if holdAmountMicro <= 0 {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	w, exists := m.wallets[orgID]
	if !exists {
		return ErrWalletNotFound
	}

	if w.IsFrozen {
		return ErrWalletFrozen
	}

	available := w.BalanceMicro - w.ReservedMicro
	if available < holdAmountMicro {
		return ErrInsufficientBalance
	}

	w.ReservedMicro += holdAmountMicro
	return nil
}

// Settle executes Phase 2: releases the hold, debits actual cost, and logs itemized ledger entry.
func (m *Manager) Settle(
	orgID string,
	holdAmountMicro int64,
	actualCostMicro int64,
	requestID string,
	virtualKeyID string,
	description string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, exists := m.wallets[orgID]
	if !exists {
		return ErrWalletNotFound
	}

	// Release hold
	w.ReservedMicro -= holdAmountMicro
	if w.ReservedMicro < 0 {
		w.ReservedMicro = 0
	}

	// Debit actual cost
	w.BalanceMicro -= actualCostMicro
	if w.BalanceMicro < 0 {
		w.BalanceMicro = 0 // Invariant: no negative overdraft
	}

	// Record immutable ledger entry
	m.ledger = append(m.ledger, LedgerEntry{
		ID:                fmt.Sprintf("tx-settle-%s", requestID),
		OrgID:             orgID,
		RequestID:         requestID,
		VirtualKeyID:      virtualKeyID,
		TransactionType:   TxSettlement,
		AmountMicro:       actualCostMicro,
		BalanceAfterMicro: w.BalanceMicro,
		Description:       description,
		CreatedAt:         time.Now(),
	})

	return nil
}

// Release releases a reservation completely when a request aborts before generating tokens.
func (m *Manager) Release(orgID string, holdAmountMicro int64) error {
	if holdAmountMicro <= 0 {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	w, exists := m.wallets[orgID]
	if !exists {
		return ErrWalletNotFound
	}

	w.ReservedMicro -= holdAmountMicro
	if w.ReservedMicro < 0 {
		w.ReservedMicro = 0
	}
	return nil
}

// GetLedgerHistory returns all ledger records for an organization.
func (m *Manager) GetLedgerHistory(orgID string) []LedgerEntry {
	m.mu.Lock()
	defer m.mu.Unlock()

	var records []LedgerEntry
	for _, l := range m.ledger {
		if l.OrgID == orgID {
			records = append(records, l)
		}
	}
	return records
}

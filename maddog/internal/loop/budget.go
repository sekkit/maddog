package loop

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

var ErrBudgetExceeded = errors.New("budget exceeded")
var ErrBudgetReservationNotFound = errors.New("budget reservation not found")

type BudgetLedgerConfig struct {
	RunID       string
	LimitTokens int64
}

type BudgetLedger struct {
	mu           sync.Mutex
	runID        string
	limit        int64
	used         int64
	reserved     int64
	reservations map[string]BudgetReservation
	next         atomic.Int64
}

type BudgetReservation struct {
	ID     string
	RunID  string
	Role   string
	Tokens int64
}

func NewBudgetLedger(cfg BudgetLedgerConfig) *BudgetLedger {
	return &BudgetLedger{
		runID:        cfg.RunID,
		limit:        cfg.LimitTokens,
		reservations: map[string]BudgetReservation{},
	}
}

func (l *BudgetLedger) Reserve(role string, tokens int64) (BudgetReservation, error) {
	if l == nil || tokens <= 0 {
		return BudgetReservation{}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.limit > 0 && l.used+l.reserved+tokens > l.limit {
		return BudgetReservation{}, ErrBudgetExceeded
	}
	id := fmt.Sprintf("budget-%d", l.next.Add(1))
	res := BudgetReservation{ID: id, RunID: l.runID, Role: role, Tokens: tokens}
	l.reservations[id] = res
	l.reserved += tokens
	return res, nil
}

func (l *BudgetLedger) Debit(id string, tokens int64) (RunEvent, error) {
	if l == nil {
		return RunEvent{}, ErrBudgetReservationNotFound
	}
	if tokens < 0 {
		tokens = 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	res, ok := l.reservations[id]
	if !ok {
		return RunEvent{}, ErrBudgetReservationNotFound
	}
	if l.limit > 0 && l.used+tokens > l.limit {
		return RunEvent{}, ErrBudgetExceeded
	}
	delete(l.reservations, id)
	l.reserved -= res.Tokens
	if l.reserved < 0 {
		l.reserved = 0
	}
	l.used += tokens
	return RunEvent{
		Kind:                  RunEventBudgetDebited,
		RunID:                 l.runID,
		Role:                  res.Role,
		BudgetUsedTokens:      l.used,
		BudgetLimitTokens:     l.limit,
		BudgetRemainingTokens: l.remainingLocked(),
	}, nil
}

func (l *BudgetLedger) RemainingTokens() int64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.remainingLocked()
}

func (l *BudgetLedger) remainingLocked() int64 {
	if l.limit <= 0 {
		return 0
	}
	remaining := l.limit - l.used - l.reserved
	if remaining < 0 {
		return 0
	}
	return remaining
}

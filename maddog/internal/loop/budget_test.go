package loop

import (
	"errors"
	"sync"
	"testing"
)

func TestBudgetLedgerReserveDebitAndRemaining(t *testing.T) {
	ledger := NewBudgetLedger(BudgetLedgerConfig{RunID: "run-1", LimitTokens: 100})

	reservation, err := ledger.Reserve("frontier", 40)
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if got := ledger.RemainingTokens(); got != 60 {
		t.Fatalf("remaining after reserve = %d, want 60", got)
	}
	event, err := ledger.Debit(reservation.ID, 25)
	if err != nil {
		t.Fatalf("Debit: %v", err)
	}
	if event.Kind != RunEventBudgetDebited || event.RunID != "run-1" || event.Role != "frontier" {
		t.Fatalf("debit event = %+v", event)
	}
	if event.BudgetUsedTokens != 25 || event.BudgetRemainingTokens != 75 {
		t.Fatalf("debit budget = used:%d remaining:%d, want 25/75", event.BudgetUsedTokens, event.BudgetRemainingTokens)
	}
	if got := ledger.RemainingTokens(); got != 75 {
		t.Fatalf("remaining after debit = %d, want 75", got)
	}
}

func TestBudgetLedgerRejectsOverReserveAndOverDebit(t *testing.T) {
	ledger := NewBudgetLedger(BudgetLedgerConfig{RunID: "run-1", LimitTokens: 10})
	if _, err := ledger.Reserve("frontier", 11); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("over reserve err = %v, want ErrBudgetExceeded", err)
	}
	reservation, err := ledger.Reserve("frontier", 8)
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if _, err := ledger.Debit(reservation.ID, 11); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("over debit err = %v, want ErrBudgetExceeded", err)
	}
}

func TestBudgetLedgerConcurrentReserveHonorsHardCap(t *testing.T) {
	ledger := NewBudgetLedger(BudgetLedgerConfig{RunID: "run-1", LimitTokens: 60})
	var wg sync.WaitGroup
	var ok int
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := ledger.Reserve("frontier", 20); err == nil {
				ok++
			}
		}()
	}
	wg.Wait()
	if ok != 3 {
		t.Fatalf("successful reservations = %d, want 3", ok)
	}
	if got := ledger.RemainingTokens(); got != 0 {
		t.Fatalf("remaining = %d, want 0", got)
	}
}

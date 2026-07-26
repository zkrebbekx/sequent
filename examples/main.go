// Command examples shows sequent numbering invoices, and demonstrates the honest
// gaplessness contract: a number is only truly gapless when the counter is
// advanced inside the same transaction that persists the document, so a
// rolled-back document consumes no number. The library ships a memory Store for
// single-process use; this program also implements a toy transaction-bound Store
// modelling the SQL UPDATE ... RETURNING pattern the README sketches.
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/zkrebbekx/sequent"
)

func main() {
	ctx := context.Background()
	f, err := sequent.NewFormat("INV-{year}-{seq:05}", sequent.Yearly)
	if err != nil {
		panic(err)
	}

	simpleRun(ctx, f)
	yearlyReset(ctx, f)
	scopeIsolation(ctx, f)
	gaplessTransaction(ctx, f)
}

// simpleRun allocates three consecutive numbers against the in-memory Store.
func simpleRun(ctx context.Context, f *sequent.Format) {
	fmt.Println("== simple run (memory Store) ==")
	store := sequent.NewMemStore()
	at := time.Date(2026, time.February, 14, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		num, _ := f.Allocate(ctx, store, sequent.Scope{Tenant: "acme"}, at)
		fmt.Println(" ", num)
	}
}

// yearlyReset shows the counter restarting at 1 as the civil year rolls over.
func yearlyReset(ctx context.Context, f *sequent.Format) {
	fmt.Println("== yearly reset ==")
	store := sequent.NewMemStore()
	scope := sequent.Scope{Tenant: "acme"}
	dec := time.Date(2026, time.December, 31, 23, 0, 0, 0, time.UTC)
	jan := time.Date(2027, time.January, 1, 0, 30, 0, 0, time.UTC)
	a, _ := f.Allocate(ctx, store, scope, dec)
	b, _ := f.Allocate(ctx, store, scope, dec)
	c, _ := f.Allocate(ctx, store, scope, jan)
	fmt.Printf("  %s, %s, then next year -> %s\n", a, b, c)
}

// scopeIsolation shows two tenants (and two series) keeping independent runs.
func scopeIsolation(ctx context.Context, f *sequent.Format) {
	fmt.Println("== scope isolation ==")
	store := sequent.NewMemStore()
	at := time.Date(2026, time.February, 14, 10, 0, 0, 0, time.UTC)
	acme, _ := f.Allocate(ctx, store, sequent.Scope{Tenant: "acme", Series: "INV"}, at)
	globex, _ := f.Allocate(ctx, store, sequent.Scope{Tenant: "globex", Series: "INV"}, at)
	credit, _ := f.Allocate(ctx, store, sequent.Scope{Tenant: "acme", Series: "CN"}, at)
	fmt.Printf("  acme/INV=%s  globex/INV=%s  acme/CN=%s (all start at 1)\n", acme, globex, credit)
}

// gaplessTransaction demonstrates the correct, gapless pattern. A document
// creation that rolls back consumes no number, so the next successful document
// takes the very number the failed one had reserved — no hole in the run.
func gaplessTransaction(ctx context.Context, f *sequent.Format) {
	fmt.Println("== gapless transaction (rollback burns no number) ==")
	l := newLedger()
	scope := sequent.Scope{Tenant: "acme"}
	at := time.Date(2026, time.February, 14, 10, 0, 0, 0, time.UTC)

	// Attempt 1: allocate inside a transaction, then the document insert fails.
	t1 := l.begin()
	num1, _ := f.Allocate(ctx, t1, scope, at)
	fmt.Printf("  tx1 reserved %s, but the insert failed -> rollback\n", num1)
	t1.rollback()

	// Attempt 2: a fresh transaction commits — and reuses that same number.
	t2 := l.begin()
	num2, _ := f.Allocate(ctx, t2, scope, at)
	t2.commit()
	fmt.Printf("  tx2 committed %s (the number tx1 released — gapless)\n", num2)
}

// ledger is a toy transactional counter store: the durable, transaction-bound
// Store the README sketches with SQL UPDATE ... RETURNING. It commits a counter
// advance only when its transaction commits.
type ledger struct {
	mu       sync.Mutex
	counters map[string]int64
}

func newLedger() *ledger { return &ledger{counters: make(map[string]int64)} }

func (l *ledger) begin() *tx { return &tx{l: l, pending: make(map[string]int64)} }

// tx is one transaction against the ledger. It implements sequent.Store, so
// Allocate binds its counter advance to this transaction's fate.
type tx struct {
	l       *ledger
	pending map[string]int64
}

// Next returns the committed counter plus the reservations already made in this
// transaction, without touching the ledger — the advance is durable only on
// commit.
func (t *tx) Next(_ context.Context, key string) (int64, error) {
	t.l.mu.Lock()
	defer t.l.mu.Unlock()
	t.pending[key]++
	return t.l.counters[key] + t.pending[key], nil
}

func (t *tx) commit() {
	t.l.mu.Lock()
	defer t.l.mu.Unlock()
	for k, n := range t.pending {
		t.l.counters[k] += n
	}
	t.pending = nil
}

func (t *tx) rollback() { t.pending = nil }

package sequent

import (
	"context"
	"sync"
)

// Store hands out the durable, gapless counter for a key. The caller implements
// it over their storage and — for true gaplessness — binds Next to the same
// transaction that persists the document, so a rolled-back transaction consumes
// no number. Next must return strictly consecutive values (1, 2, 3, …) for a
// given key and must never repeat one; that is where gaplessness actually lives.
// sequent renders whatever Next returns and cannot itself create a gap.
//
// A single key corresponds to one (Tenant, Series, period) triple; see
// [Format.Key]. Next may return an error (a cancelled context, a failed query),
// which [Format.Allocate] propagates without consuming a number.
type Store interface {
	Next(ctx context.Context, key string) (int64, error)
}

// MemStore is an in-memory [Store]: a mutex-guarded per-key counter. It is
// gapless within a single process — the reference the conformance tests run
// against — and safe for concurrent use. It is deliberately not durable: its
// counters are lost on restart, which is exactly the job a SQL Store exists to
// do (see the README's FOR UPDATE / UPDATE ... RETURNING sketch). Use MemStore
// for tests, previews, and single-process numbering that need not survive a
// crash.
type MemStore struct {
	mu       sync.Mutex
	counters map[string]int64
}

// NewMemStore returns an empty in-memory Store.
func NewMemStore() *MemStore {
	return &MemStore{counters: make(map[string]int64)}
}

// Next returns the next consecutive counter for key, starting at 1. It honours
// context cancellation: a cancelled ctx returns its error and consumes no
// number, preserving gaplessness. The increment is atomic under an internal
// mutex, so concurrent callers for one key receive distinct consecutive values
// with no gap and no repeat.
func (m *MemStore) Next(ctx context.Context, key string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[key]++
	return m.counters[key], nil
}

// Peek returns the current (last issued) counter for key without advancing it,
// or 0 if key has never been allocated. It is a read-only helper for tests and
// diagnostics and takes the same lock as [MemStore.Next].
func (m *MemStore) Peek(key string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counters[key]
}

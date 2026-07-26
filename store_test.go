package sequent

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"
)

func mustAllocate(t *testing.T, f *Format, s Store, scope Scope, at time.Time) string {
	t.Helper()
	got, err := f.Allocate(context.Background(), s, scope, at)
	if err != nil {
		t.Fatalf("Allocate: unexpected error: %v", err)
	}
	return got
}

// --- Golden vector 3: yearly reset -----------------------------------------

func TestGoldenYearlyReset(t *testing.T) {
	t.Run("Given INV-{year}-{seq:05} yearly over a memory Store", func(t *testing.T) {
		f := mustFormat(t, "INV-{year}-{seq:05}", Yearly)
		store := NewMemStore()
		scope := Scope{Tenant: "acme"}

		dec31 := time.Date(2026, time.December, 31, 23, 0, 0, 0, time.UTC)
		jan1 := time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)

		t.Run("When 42 numbers are allocated in 2026", func(t *testing.T) {
			var last string
			for i := 0; i < 42; i++ {
				last = mustAllocate(t, f, store, scope, dec31)
			}
			t.Run("Then the 42nd is INV-2026-00042", func(t *testing.T) {
				if last != "INV-2026-00042" {
					t.Fatalf("42nd = %q, want INV-2026-00042", last)
				}
			})
			t.Run("Then the first allocate in 2027 resets to INV-2027-00001", func(t *testing.T) {
				got := mustAllocate(t, f, store, scope, jan1)
				if got != "INV-2027-00001" {
					t.Fatalf("first 2027 = %q, want INV-2027-00001", got)
				}
			})
			t.Run("Then 2026 continues from 43 if numbered again", func(t *testing.T) {
				got := mustAllocate(t, f, store, scope, dec31)
				if got != "INV-2026-00043" {
					t.Fatalf("next 2026 = %q, want INV-2026-00043 (period counters are independent)", got)
				}
			})
		})
	})
}

// --- Golden vector 4: monthly reset ----------------------------------------

func TestGoldenMonthlyReset(t *testing.T) {
	t.Run("Given INV-{year}{month}-{seq:04} monthly", func(t *testing.T) {
		f := mustFormat(t, "INV-{year}{month}-{seq:04}", Monthly)
		store := NewMemStore()
		scope := Scope{Tenant: "acme"}

		feb := time.Date(2026, time.February, 20, 12, 0, 0, 0, time.UTC)
		mar := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)

		t.Run("When three numbers are allocated in February", func(t *testing.T) {
			var last string
			for i := 0; i < 3; i++ {
				last = mustAllocate(t, f, store, scope, feb)
			}
			t.Run("Then the third is INV-202602-0003", func(t *testing.T) {
				if last != "INV-202602-0003" {
					t.Fatalf("third Feb = %q, want INV-202602-0003", last)
				}
			})
			t.Run("Then the first allocate in March resets to 0001", func(t *testing.T) {
				got := mustAllocate(t, f, store, scope, mar)
				if got != "INV-202603-0001" {
					t.Fatalf("first Mar = %q, want INV-202603-0001", got)
				}
			})
		})
	})
}

func TestDailyReset(t *testing.T) {
	t.Run("Given {year}{month}{day}-{seq:03} daily", func(t *testing.T) {
		f := mustFormat(t, "{year}{month}{day}-{seq:03}", Daily)
		store := NewMemStore()
		scope := Scope{}
		d14 := time.Date(2026, time.February, 14, 8, 0, 0, 0, time.UTC)
		d15 := time.Date(2026, time.February, 15, 8, 0, 0, 0, time.UTC)
		t.Run("Then each civil day starts a fresh counter", func(t *testing.T) {
			a := mustAllocate(t, f, store, scope, d14)
			b := mustAllocate(t, f, store, scope, d14)
			c := mustAllocate(t, f, store, scope, d15)
			if a != "20260214-001" || b != "20260214-002" || c != "20260215-001" {
				t.Fatalf("got %q,%q,%q want 20260214-001,20260214-002,20260215-001", a, b, c)
			}
		})
	})
}

func TestNeverDoesNotReset(t *testing.T) {
	t.Run("Given a Never policy across two different years", func(t *testing.T) {
		f := mustFormat(t, "N-{seq}", Never)
		store := NewMemStore()
		scope := Scope{}
		y2026 := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
		y2027 := time.Date(2027, time.June, 1, 0, 0, 0, 0, time.UTC)
		t.Run("Then the counter keeps climbing regardless of year", func(t *testing.T) {
			a := mustAllocate(t, f, store, scope, y2026)
			b := mustAllocate(t, f, store, scope, y2027)
			if a != "N-1" || b != "N-2" {
				t.Fatalf("got %q,%q want N-1,N-2", a, b)
			}
		})
	})
}

// --- Golden vector 5: scope isolation --------------------------------------

func TestGoldenScopeIsolation(t *testing.T) {
	f := mustFormat(t, "{tenant}-{series}-{seq:03}", Yearly)
	store := NewMemStore()
	at := feb14

	t.Run("Given two tenants allocating the same instant", func(t *testing.T) {
		t.Run("Then each tenant starts at 1 independently", func(t *testing.T) {
			a1 := mustAllocate(t, f, store, Scope{Tenant: "A", Series: "INV"}, at)
			b1 := mustAllocate(t, f, store, Scope{Tenant: "B", Series: "INV"}, at)
			a2 := mustAllocate(t, f, store, Scope{Tenant: "A", Series: "INV"}, at)
			if a1 != "A-INV-001" || b1 != "B-INV-001" || a2 != "A-INV-002" {
				t.Fatalf("got %q,%q,%q want A-INV-001,B-INV-001,A-INV-002", a1, b1, a2)
			}
		})
	})
	t.Run("Given two series for one tenant", func(t *testing.T) {
		t.Run("Then INV and CN counters are independent", func(t *testing.T) {
			inv := mustAllocate(t, f, store, Scope{Tenant: "C", Series: "INV"}, at)
			cn := mustAllocate(t, f, store, Scope{Tenant: "C", Series: "CN"}, at)
			if inv != "C-INV-001" || cn != "C-CN-001" {
				t.Fatalf("got %q,%q want C-INV-001,C-CN-001", inv, cn)
			}
		})
	})
}

// --- Golden vector 8: consecutive, no gap/repeat ---------------------------

func TestGoldenConsecutive(t *testing.T) {
	t.Run("Given 1000 sequential allocations for one scope+period", func(t *testing.T) {
		f := mustFormat(t, "{seq}", Yearly)
		store := NewMemStore()
		scope := Scope{Tenant: "acme", Series: "INV"}
		seen := make(map[string]bool, 1000)
		t.Run("Then the rendered numbers are exactly 1..1000, no gap, no repeat", func(t *testing.T) {
			for i := 1; i <= 1000; i++ {
				got := mustAllocate(t, f, store, scope, feb14)
				want := fmt.Sprintf("%d", i)
				if got != want {
					t.Fatalf("allocation %d = %q, want %q", i, got, want)
				}
				if seen[got] {
					t.Fatalf("duplicate value %q", got)
				}
				seen[got] = true
			}
			if len(seen) != 1000 {
				t.Fatalf("distinct values = %d, want 1000", len(seen))
			}
		})
	})
}

// --- Delimiter / collision safety (the correction) -------------------------

func TestKeyCollisionSafety(t *testing.T) {
	f := mustFormat(t, "{seq}", Never)
	t.Run("Given adversarial tenant/series strings that a naive delimiter would collide", func(t *testing.T) {
		cases := []struct {
			name string
			a, b Scope
		}{
			{"pipe boundary shift", Scope{Tenant: "a|b"}, Scope{Tenant: "a", Series: "b"}},
			{"pipe in series", Scope{Tenant: "a", Series: "b|c"}, Scope{Tenant: "a|b", Series: "c"}},
			{"colon (our framing char) boundary", Scope{Tenant: "1:x"}, Scope{Tenant: "1", Series: "x"}},
			{"digits mimicking length prefix", Scope{Tenant: "3:abc"}, Scope{Tenant: "3", Series: "abc"}},
			{"empty vs nonempty split", Scope{Tenant: "", Series: "ab"}, Scope{Tenant: "a", Series: "b"}},
		}
		for _, tc := range cases {
			t.Run("When keying "+tc.name, func(t *testing.T) {
				t.Run("Then the two scopes produce distinct keys", func(t *testing.T) {
					ka := f.Key(tc.a, feb14)
					kb := f.Key(tc.b, feb14)
					if ka == kb {
						t.Fatalf("collision: %+v and %+v both key to %q", tc.a, tc.b, ka)
					}
				})
			})
		}
	})

	t.Run("Given adversarial scopes allocated against one Store", func(t *testing.T) {
		t.Run("Then the two counters stay independent (both start at 1)", func(t *testing.T) {
			store := NewMemStore()
			x := mustAllocate(t, f, store, Scope{Tenant: "a|b"}, feb14)
			y := mustAllocate(t, f, store, Scope{Tenant: "a", Series: "b"}, feb14)
			if x != "1" || y != "1" {
				t.Fatalf("got %q,%q want 1,1 (no cross-scope collision)", x, y)
			}
		})
	})
}

// TestKeyInjective is a property check: over many random triples drawn from an
// alphabet rich in the framing characters (colons and digits) and common
// delimiters, distinct (tenant, series, period) triples never share a key.
func TestKeyInjective(t *testing.T) {
	t.Run("Given random (tenant, series, period) triples over a hostile alphabet", func(t *testing.T) {
		r := rand.New(rand.NewSource(1))
		alphabet := []byte("ab:|0123456789 -")
		randStr := func() string {
			n := r.Intn(6)
			bs := make([]byte, n)
			for i := range bs {
				bs[i] = alphabet[r.Intn(len(alphabet))]
			}
			return string(bs)
		}
		t.Run("Then encodeKey is injective (no two distinct triples collide)", func(t *testing.T) {
			seen := make(map[string][3]string, 20000)
			for i := 0; i < 20000; i++ {
				triple := [3]string{randStr(), randStr(), randStr()}
				k := encodeKey(triple[0], triple[1], triple[2])
				if prev, ok := seen[k]; ok && prev != triple {
					t.Fatalf("collision on key %q: %v vs %v", k, prev, triple)
				}
				seen[k] = triple
			}
		})
	})
}

// --- Concurrency: exactly-once consecutive under -race ---------------------

func TestConcurrentAllocate(t *testing.T) {
	t.Run("Given N goroutines each allocating M numbers for one scope+period", func(t *testing.T) {
		const (
			goroutines = 16
			per        = 250
			total      = goroutines * per
		)
		f := mustFormat(t, "{seq}", Yearly)
		store := NewMemStore()
		scope := Scope{Tenant: "acme", Series: "INV"}

		results := make([]string, total)
		var wg sync.WaitGroup
		for g := 0; g < goroutines; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				for i := 0; i < per; i++ {
					got, err := f.Allocate(context.Background(), store, scope, feb14)
					if err != nil {
						t.Errorf("Allocate: %v", err)
						return
					}
					results[g*per+i] = got
				}
			}(g)
		}
		wg.Wait()

		t.Run("Then the union of all values is exactly 1..N*M, no gap, no repeat", func(t *testing.T) {
			seen := make(map[int]bool, total)
			for _, s := range results {
				var n int
				if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
					t.Fatalf("unparseable result %q: %v", s, err)
				}
				if seen[n] {
					t.Fatalf("duplicate value %d", n)
				}
				seen[n] = true
			}
			for n := 1; n <= total; n++ {
				if !seen[n] {
					t.Fatalf("missing value %d (a gap)", n)
				}
			}
		})
	})
}

// --- Store error handling and Allocate guards ------------------------------

type errStore struct{ err error }

func (e errStore) Next(context.Context, string) (int64, error) { return 0, e.err }

// zeroStore violates the Store contract by returning a non-positive counter.
type zeroStore struct{}

func (zeroStore) Next(context.Context, string) (int64, error) { return 0, nil }

// recordingStore captures the key it was last asked for.
type recordingStore struct {
	key string
	n   int64
}

func (r *recordingStore) Next(_ context.Context, key string) (int64, error) {
	r.key = key
	r.n++
	return r.n, nil
}

func TestAllocateErrorPaths(t *testing.T) {
	f := mustFormat(t, "{seq}", Never)

	t.Run("Given a nil Store", func(t *testing.T) {
		t.Run("Then Allocate returns ErrNilStore", func(t *testing.T) {
			_, err := f.Allocate(context.Background(), nil, Scope{}, feb14)
			if !errors.Is(err, ErrNilStore) {
				t.Fatalf("error = %v, want ErrNilStore", err)
			}
		})
	})

	t.Run("Given a Store that returns an error", func(t *testing.T) {
		sentinel := errors.New("db exploded")
		t.Run("Then Allocate propagates it unwrapped", func(t *testing.T) {
			_, err := f.Allocate(context.Background(), errStore{sentinel}, Scope{}, feb14)
			if !errors.Is(err, sentinel) {
				t.Fatalf("error = %v, want the store's sentinel", err)
			}
		})
	})

	t.Run("Given a Store that violates its contract with a zero counter", func(t *testing.T) {
		t.Run("Then Allocate surfaces ErrBadSeq rather than rendering garbage", func(t *testing.T) {
			_, err := f.Allocate(context.Background(), zeroStore{}, Scope{}, feb14)
			if !errors.Is(err, ErrBadSeq) {
				t.Fatalf("error = %v, want ErrBadSeq", err)
			}
		})
	})

	t.Run("Given a cancelled context and the memory Store", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		store := NewMemStore()
		t.Run("Then Allocate returns the cancellation and consumes no number", func(t *testing.T) {
			_, err := f.Allocate(ctx, store, Scope{Tenant: "acme"}, feb14)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
			if got := store.Peek(f.Key(Scope{Tenant: "acme"}, feb14)); got != 0 {
				t.Fatalf("counter advanced to %d despite cancellation", got)
			}
		})
	})

	t.Run("Given a recording Store", func(t *testing.T) {
		t.Run("Then Allocate asks for exactly Format.Key(scope, at)", func(t *testing.T) {
			rec := &recordingStore{}
			scope := Scope{Tenant: "acme", Series: "INV"}
			at := feb14
			if _, err := f.Allocate(context.Background(), rec, scope, at); err != nil {
				t.Fatalf("Allocate: %v", err)
			}
			if want := f.Key(scope, at); rec.key != want {
				t.Fatalf("Store queried key %q, want %q", rec.key, want)
			}
		})
	})
}

func TestMemStorePeek(t *testing.T) {
	t.Run("Given a fresh memory Store", func(t *testing.T) {
		store := NewMemStore()
		t.Run("Then Peek on an unknown key is 0", func(t *testing.T) {
			if got := store.Peek("nope"); got != 0 {
				t.Fatalf("Peek = %d, want 0", got)
			}
		})
		t.Run("Then Peek reflects the last issued value without advancing it", func(t *testing.T) {
			ctx := context.Background()
			if _, err := store.Next(ctx, "k"); err != nil {
				t.Fatalf("Next: %v", err)
			}
			if _, err := store.Next(ctx, "k"); err != nil {
				t.Fatalf("Next: %v", err)
			}
			if got := store.Peek("k"); got != 2 {
				t.Fatalf("Peek = %d, want 2", got)
			}
			if got := store.Peek("k"); got != 2 {
				t.Fatalf("Peek again = %d, want 2 (non-advancing)", got)
			}
		})
	})
}

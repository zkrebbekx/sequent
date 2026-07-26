# sequent

Gapless, formatted, per-scope document numbering for Go — `INV-2026-00042`, then
`INV-2026-00043`, never a gap, reset each year.

A tax authority wants an unbroken run of invoice numbers. Most products get this
wrong. Odoo's own "no-gap" sequence *detects* a gap after the fact rather than
prevents it. The reason it is hard is that gaplessness is a **durability**
property, not a formatting one: a number is truly gapless only when you allocate
it in the same committed transaction as the document it numbers.

So sequent splits the problem honestly. sequent owns the deterministic parts —
the template, the reset policy, the per-scope isolation, the padding. You supply
the atomic durable increment as a `Store`, and you bind it to your transaction.
That is where gaplessness actually lives, and it belongs with your database.

- **A validated template.** `NewFormat` compiles the template once. A bad token
  or a missing `{seq}` is an error at setup, so `Render` and `Allocate` never
  fail on the template later. Tokens: `{year}`, `{year2}`, `{month}`, `{day}`,
  `{seq}`, `{seq:N}` (zero-pad to a minimum width N), `{tenant}`, `{series}`,
  and `{{`/`}}` for a literal brace.
- **Reset policies.** `Never`, `Yearly`, `Monthly`, `Daily`. When the period
  rolls over, the counter starts again at 1. The reset falls out of the key —
  there is no special case.
- **Per-scope isolation.** A `Scope{Tenant, Series}` gives each tenant and each
  series its own counter. Tenant A's invoices and tenant B's never share a run.
  The key encoding is length-prefixed, so a tenant that contains the delimiter
  cannot collide with another scope.
- **A civil-date period.** The period follows the format's timezone, not the
  server's UTC clock. An invoice at `2027-01-01T00:30+09:00` belongs to year
  2026 under a UTC format, because that instant is still 2026 in UTC.
- **Zero dependencies.** Go 1.23+.

```go
import "github.com/zkrebbekx/sequent"
```

## Example

```go
f, _ := sequent.NewFormat("INV-{year}-{seq:05}", sequent.Yearly)
store := sequent.NewMemStore()
scope := sequent.Scope{Tenant: "acme", Series: "INV"}

n, _ := f.Allocate(ctx, store, scope, time.Now())
fmt.Println(n) // INV-2026-00001

// Preview a number without touching the store.
p, _ := f.Render(scope, 42, time.Date(2026, 2, 14, 0, 0, 0, 0, time.UTC))
fmt.Println(p) // INV-2026-00042
```

## Gaplessness — the honest contract

sequent cannot create a gap. `Allocate` renders whatever consecutive counter the
`Store` returns. Whether the whole run is gapless depends on how you use the
`Store`.

- **Gapless (correct).** Call the `Store` inside the same database transaction
  that inserts the document. Let both commit together. A rolled-back
  transaction never consumed a number. This is what a tax authority requires.
- **Not gapless (the trap).** Allocate the number, then create the document in a
  separate step that can fail. A crash between the two steps burns a number.

The memory `Store` is gapless within one process. It is the reference the tests
run against. It is not durable across a restart — that is the job of a SQL
`Store`.

## The Store

`Store` is the atomic durable increment. You implement it over your storage.

```go
type Store interface {
    // Next returns the next consecutive counter for key: 1, 2, 3, …
    // It must never repeat a value. Gaplessness lives here.
    Next(ctx context.Context, key string) (int64, error)
}
```

`Format.Key(scope, at)` gives you the exact key that `Allocate` uses, so you can
lock or inspect the counter row yourself.

A durable SQL `Store` is a few lines. Bind `Next` to the caller's transaction:

```sql
-- One row per key. UPSERT and return the new value, atomically.
INSERT INTO sequence_counters (key, value) VALUES ($1, 1)
ON CONFLICT (key) DO UPDATE SET value = sequence_counters.value + 1
RETURNING value;
```

Run that statement in the same transaction that inserts the document. If the
transaction rolls back, the number is not consumed, and the run stays gapless.

## Scope

sequent numbers documents. It does not generate unordered ids (use a ULID or a
UUID for that), coordinate across a cluster (the `Store` and its database do
that), or store anything (it holds no state between calls). It formats a
sequence number over a counter you own.

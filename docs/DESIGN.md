# sequent — design

Gapless, formatted, per-scope document numbering for Go: `INV-2026-00042`, the
next one `INV-2026-00043`, never a gap, reset each year. Zero dependencies.

Status: Phase 1 implemented — see "Phase 1 — as built". The model below is the
sketch; the built API wraps `Format` in a validating `NewFormat` constructor
with accessor methods, and the `Store` key uses a length-prefixed encoding.

## Why this exists

Gapless sequential numbering is a legal requirement for invoices in many
jurisdictions — the tax authority wants an unbroken run. Yet it is reimplemented
per product and mostly done wrong. Reference ERP is telling: Odoo's "no-gap"
sequence *detects* gaps after the fact ("a Gaps in the sequence message
appears") rather than preventing them, and its own docs call the result "a
unique number" — notably not consecutive. A GitHub search for a standalone Go
library returns nothing; it exists only as ERP-internal code.

The reason it is hard is that gaplessness is a **durability** property, not a
formatting one. A number is only truly gapless if it is allocated in the *same
committed transaction* as the document it numbers — allocate-then-fail leaves a
hole. No formatting library can promise that on its own.

So sequent splits the problem honestly:

- sequent owns the **deterministic** parts: period-key computation, reset
  policies, the template render, per-scope isolation, zero-padding.
- The **atomic durable increment** is a `Store` the caller supplies and binds to
  their own transaction. That is where gaplessness actually lives, and it
  belongs with the caller's database, not with a library that owns no storage.

sequent gives you the tool to *prevent* gaps — where Odoo only detects them —
provided you allocate inside the document's transaction.

## Scope

**In scope — the whole library:**
- A **`Format`**: a template (`INV-{year}-{seq:05}`) and a reset policy.
- **Template rendering** with tokens: `{year}`/`{year2}`, `{month}`, `{day}`,
  `{seq}`/`{seq:N}` (zero-padded to N), `{tenant}`, `{series}`, and a literal
  `{{`/`}}` escape.
- **Reset policies**: Never, Yearly, Monthly, Daily — the sequence restarts at 1
  when the period rolls over.
- **Scope**: `(tenant, series)` isolation, so tenant A's invoices and tenant B's
  never share a counter, and invoices vs credit-notes are separate runs.
- A **`Store`** interface (the atomic durable increment) and an in-memory
  implementation for tests and single-process use.
- `Allocate` = compute the period key from the instant + reset policy, call
  `Store.Next` for that scope+period, render the template. One call, one number.

**Explicitly NOT in scope:**
- **The database.** sequent defines the `Store` boundary and ships a memory
  Store; the durable, transactional one (a row locked `FOR UPDATE`, or an
  atomic `UPDATE ... RETURNING`) is the caller's, bound to their transaction.
  Documenting *how* to make it gapless is in scope; owning the database is not.
- **Distributed coordination, ID generation (ULID/snowflake), formatting money
  or dates beyond the sequence tokens.** A sequence number is not a UUID; if you
  want an unordered unique id, use one of those instead.

## The model

```go
type Reset uint8
const ( Never Reset = iota; Yearly; Monthly; Daily )

type Format struct {
    Template string  // e.g. "INV-{year}-{seq:05}"
    Reset    Reset
}

type Scope struct { Tenant, Series string }  // "" is a valid single-tenant default

// Store hands out the durable, gapless counter for a key. The caller implements
// it over their storage and — for true gaplessness — binds Next to the same
// transaction that persists the document. Next must return consecutive values
// (1, 2, 3, …) for a key and never repeat one; it is where gaplessness lives.
type Store interface {
    Next(ctx context.Context, key string) (int64, error)
}

// Allocate produces the next formatted number for scope at instant at: it
// derives the period key from at and the reset policy, asks the Store for the
// next counter under (scope, period), and renders the template.
func (f Format) Allocate(ctx context.Context, s Store, scope Scope, at time.Time) (string, error)

// Render formats an explicit seq without touching a Store — for previewing a
// template, or numbering when the caller already holds the counter.
func (f Format) Render(scope Scope, seq int64, at time.Time) (string, error)

// Key is the Store key Allocate would use: scope plus the reset period of at.
// Exposed so a caller can pre-lock or inspect the exact counter row.
func (f Format) Key(scope Scope, at time.Time) string
```

### The period key
The `Store` key encodes everything that must share a counter, and nothing that
must not: `tenant | series | period`, where `period` is `""` for Never,
`"2026"` for Yearly, `"2026-02"` for Monthly, `"2026-02-14"` for Daily — derived
from `at` **in a fixed timezone the Format carries** (default UTC; a caller
numbering in a business zone sets it), because "which year is this invoice in"
is a civil-date question and must not flip on a UTC boundary. When the period
rolls over, the key changes, so `Store.Next` starts a fresh counter at 1 — that
is how a reset is a reset, with no special-casing.

### Rendering
`Render` walks the template, substituting tokens from `(scope, seq, at)`.
`{seq:05}` zero-pads to width 5 (`00042`); `{seq}` is the bare integer.
`{year}` is 4-digit, `{year2}` 2-digit, `{month}`/`{day}` 2-digit. An unknown
token, or a malformed `{seq:x}`, is a construction-time error (Format is
validated once), not a silent literal. `{{` and `}}` emit literal braces.

## Gaplessness — the honest contract (README + doc.go, verbatim intent)

sequent cannot *create* a gap: `Allocate` renders whatever consecutive counter
the `Store` returns. Whether the overall run is gapless is decided by how the
caller uses the `Store`:

- **Gapless (correct):** call `Store.Next` inside the same DB transaction that
  inserts the document, and let both commit together. A rolled-back transaction
  never consumed a number. This is what a tax authority requires, and sequent's
  memory `Store` and the documented SQL pattern both support it.
- **Not gapless (the Odoo trap):** allocate the number, then create the document
  in a separate step that can fail — a crash between the two burns a number.
  sequent documents this and does not pretend the library alone prevents it.

The memory `Store` is gapless within a process (mutex-guarded counters) and is
the reference the conformance tests run against; it is not durable across a
restart — that is the SQL Store's job, sketched in the README.

## Golden test vectors (become the suite)

1. **Render.** `{Template: "INV-{year}-{seq:05}"}`, seq 42, at 2026-02-14 →
   `INV-2026-00042`. seq 43 → `INV-2026-00043`.
2. **Padding.** `{seq:05}` of 42 → `00042`; of 100000 → `100000` (no
   truncation — pad is a minimum width); `{seq}` of 42 → `42`.
3. **Yearly reset.** Allocate over a memory Store: …, Dec 31 2026 → seq 42;
   first allocate on Jan 1 2027 → seq **1** (`INV-2027-00001`), because the
   period key changed from `2026` to `2027`.
4. **Monthly reset.** Feb → `…`, Mar 1 first allocate → seq 1.
5. **Scope isolation.** Tenant A and tenant B allocating the same instant get
   independent counters (both start at 1); series "INV" and "CN" likewise.
6. **Tokens.** `{year2}` → `26`, `{month}` → `02`, `{day}` → `14`, `{tenant}`,
   `{series}`, `{{literal}}` → `{literal}`.
7. **Timezone.** An `at` of `2027-01-01T00:30:00+09:00` with a UTC Format is
   still period `2026` (it is 15:30 Dec 31 UTC) — the period follows the
   Format's zone, tested both ways.
8. **Consecutive.** 1000 allocations for one scope+period yield exactly 1..1000,
   no gap, no repeat (the memory Store's gapless property).

## Open questions

1. Token syntax: `{seq:05}` (colon-width) vs `%05d` (printf). Lean colon-width —
   readable, and printf verbs invite misuse. Pin in phase 1.
2. Should `Render` validate the template every call, or should `Format` be
   validated once by a constructor (`NewFormat`) that returns an error? Lean a
   validating constructor so a bad template fails at setup, and `Render`/
   `Allocate` on a validated Format cannot fail on the template. Decide and
   document.
3. Concurrency of the memory Store under `-race`, and whether `Allocate` holds
   any lock across `Store.Next` (it must not — the Store owns its own atomicity).
4. A negative or zero `seq` in `Render` — reject or render? A counter is ≥1;
   document the rule.

## Phase 1 — as built

Phase 1 ships the full design above: `NewFormat` (a validating constructor),
`Format.Render`/`Allocate`/`Key`, the four reset policies, `Scope` isolation, the
`Store` interface, and `MemStore`. The library has zero dependencies and 100%
statement coverage. It is clean under `go vet`, `gofmt -s`, `-race`, and
`golangci-lint`.

Resolutions to the open questions:

- **Delimiter safety (open question, and the main correctness risk) — solved by
  a length-prefixed key.** The `Store` key encodes each part as `len:value`, for
  example `3:acme3:INV4:2026`. A delimiter character inside a tenant or a series
  cannot merge two distinct scopes into one counter. `Key("a|b", "c")` and
  `Key("a", "b|c")` produce different keys, so two tenants never share a run.
- **Token syntax — colon width.** `{seq:05}` sets a minimum width of 5. It pads
  with a zero but never truncates: `{seq:05}` of `100000` is `100000`.
- **Template validation — once, at construction.** `NewFormat` returns
  `ErrBadTemplate` for an unknown or malformed token and `ErrNoSeq` for a
  template with no `{seq}`. `Render` and `Allocate` on a valid `Format` never
  fail on the template.
- **A counter starts at 1.** `Render` and `Allocate` reject a `seq` below 1, so
  a rendered number is always a real counter value.
- **The period follows the format's timezone.** An instant that is a different
  year in UTC than in its own offset takes the year of the format's zone. The
  default zone is UTC.

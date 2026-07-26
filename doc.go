// Package sequent produces gapless, formatted, per-scope document numbers —
// INV-2026-00042, then INV-2026-00043, never a gap, reset each year. It has zero
// dependencies: only the standard library.
//
// Gapless sequential numbering is a legal requirement for invoices in many
// jurisdictions, yet it is reimplemented per product and mostly done wrong,
// because gaplessness is a durability property, not a formatting one. A number
// is only truly gapless if it is allocated in the same committed transaction as
// the document it numbers; allocate-then-fail leaves a hole. So sequent splits
// the problem honestly: it owns the deterministic parts — period keys, reset
// policies, the template render, per-scope isolation, zero-padding — and the
// atomic durable increment is a [Store] the caller supplies and binds to their
// own transaction.
//
//	f, _ := sequent.NewFormat("INV-{year}-{seq:05}", sequent.Yearly)
//	store := sequent.NewMemStore()
//	num, _ := f.Allocate(ctx, store, sequent.Scope{Tenant: "acme"}, time.Now())
//	// num == "INV-2026-00001", then "-00002", …, restarting at -00001 next year.
//
// # The model
//
// A [Format] is a compiled template plus a [Reset] policy and a timezone,
// constructed and validated once by [NewFormat]. [Format.Allocate] derives the
// period key from an instant, asks the [Store] for the next counter under that
// (scope, period), and renders the template — one call, one number.
// [Format.Render] formats an explicit counter without a Store, and [Format.Key]
// exposes the exact Store key so a caller can pre-lock the counter row.
//
// # Template tokens
//
// {year} (4-digit) {year2} (2-digit) {month} (2-digit) {day} (2-digit) {seq}
// (bare counter) {seq:N} (counter zero-padded to a minimum of N digits — never
// truncated, so 100000 under {seq:05} is "100000") {tenant} {series}, and {{ }}
// for literal braces. An unknown token or a malformed {seq:N} is a construction-
// time [ErrBadTemplate], not a silent literal; a template with no {seq} token is
// [ErrNoSeq]. A validated Format therefore never fails on its template at render
// time.
//
// # Reset is a period-key change
//
// The [Store] key is (Tenant, Series, period), where period is "" for [Never],
// "2026" for [Yearly], "2026-02" for [Monthly], "2026-02-14" for [Daily] —
// derived from the instant in the Format's timezone, because "which year is this
// invoice in" is a civil-date question that must not flip on a UTC boundary.
// When the period rolls over the key changes, so the Store starts a fresh
// counter at 1: a reset with no special-casing. The key is a length-prefixed
// encoding of the three parts, injective over arbitrary bytes, so no Tenant or
// Series string — however adversarial — can collide two distinct scopes onto one
// counter.
//
// # Gaplessness — the honest contract
//
// sequent cannot create a gap: [Format.Allocate] renders whatever consecutive
// counter the [Store] returns. Whether the overall run is gapless is decided by
// how the caller uses the Store:
//
//   - Gapless (correct): call the Store inside the same database transaction
//     that inserts the document, and let both commit together. A rolled-back
//     transaction never consumed a number. This is what a tax authority
//     requires; the memory Store and the documented SQL pattern both support it.
//   - Not gapless: allocate the number, then create the document in a separate
//     step that can fail — a crash between the two burns a number. sequent
//     documents this and does not pretend the library alone prevents it.
//
// [MemStore] is gapless within a process (mutex-guarded counters) and is the
// reference the conformance tests run against; it is not durable across a
// restart — that is the SQL Store's job, sketched in the README.
//
// # Concurrency
//
// A *[Format] is immutable and safe for concurrent use; Allocate takes no lock
// and holds none across the Store call. [MemStore] is safe for concurrent Next.
//
// # Errors
//
// [NewFormat] returns [ErrBadTemplate], [ErrNoSeq], [ErrBadReset] or
// [ErrNilLocation]; [Format.Render] and [Format.Allocate] return [ErrBadSeq] for
// a counter below 1, and Allocate returns [ErrNilStore] for a nil Store. All are
// matchable with [errors.Is].
//
// # Not in scope
//
// The database (sequent defines the Store boundary and ships a memory one; the
// durable transactional Store is the caller's), distributed coordination,
// unordered id generation (ULID/snowflake), and formatting beyond the sequence
// tokens. See docs/DESIGN.md, including the "Phase 1 — as built" note recording
// the decisions and corrections this implementation made.
package sequent

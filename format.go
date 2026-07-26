package sequent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Reset selects when the per-scope counter restarts at 1. The restart is not
// special-cased logic: it falls out of the period key (see [Format.Key]). When
// the civil period named by the policy rolls over, the Store key changes, so the
// Store hands out a fresh counter beginning at 1.
type Reset uint8

const (
	// Never keeps one ever-growing counter per scope; the period is always "".
	Never Reset = iota
	// Yearly restarts the counter each civil year (period "2026").
	Yearly
	// Monthly restarts the counter each civil month (period "2026-02").
	Monthly
	// Daily restarts the counter each civil day (period "2026-02-14").
	Daily
)

// String returns the reset policy's name for diagnostics.
func (r Reset) String() string {
	switch r {
	case Never:
		return "Never"
	case Yearly:
		return "Yearly"
	case Monthly:
		return "Monthly"
	case Daily:
		return "Daily"
	default:
		return "Reset(" + strconv.Itoa(int(r)) + ")"
	}
}

// valid reports whether r is one of the four defined policies.
func (r Reset) valid() bool { return r <= Daily }

// Scope isolates counters. Two allocations share a counter only when their
// Tenant, Series and reset period all match; differ in any one and the counters
// are independent. The zero Scope ({"", ""}) is the valid single-tenant,
// single-series default. Tenant and Series are opaque caller strings and may
// contain any bytes — the period key encodes them unambiguously (see
// [Format.Key]).
type Scope struct {
	Tenant string
	Series string
}

// Option configures a [Format] at construction. The only option today is
// [InLocation]; the type exists so the reset policy stays a required positional
// argument while the timezone (which defaults to UTC) is opt-in.
type Option func(*formatConfig)

type formatConfig struct {
	loc *time.Location
}

// InLocation sets the timezone in which civil periods and the {year}/{month}/
// {day} tokens are computed. The default is UTC. Pass the business zone when
// "which year is this invoice in" must follow local midnight rather than a UTC
// boundary. A nil location is an [ErrNilLocation] error; omit the option for the
// default instead.
func InLocation(loc *time.Location) Option {
	return func(c *formatConfig) { c.loc = loc }
}

// Format is a compiled, immutable numbering template plus its reset policy and
// timezone. Construct one with [NewFormat], which validates the template once;
// thereafter [Format.Render] and [Format.Allocate] cannot fail on the template.
// A *Format is safe for concurrent use — it holds no mutable state and takes no
// lock (the Store owns counter atomicity).
type Format struct {
	template string
	reset    Reset
	loc      *time.Location
	tokens   []token
}

// NewFormat compiles template with reset policy reset, validating the template
// once. It returns [ErrBadTemplate] for a malformed template, [ErrNoSeq] for one
// with no {seq} token, [ErrBadReset] for an unknown policy, and [ErrNilLocation]
// for an explicit nil [InLocation]. On success the returned *Format renders
// without ever failing on the template.
//
//	f, err := sequent.NewFormat("INV-{year}-{seq:05}", sequent.Yearly)
//	f, err := sequent.NewFormat("INV-{year}-{seq:05}", sequent.Yearly, sequent.InLocation(loc))
func NewFormat(template string, reset Reset, opts ...Option) (*Format, error) {
	if !reset.valid() {
		return nil, fmt.Errorf("%w: %d", ErrBadReset, reset)
	}
	cfg := formatConfig{loc: time.UTC}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.loc == nil {
		return nil, ErrNilLocation
	}
	tokens, err := parseTemplate(template)
	if err != nil {
		return nil, err
	}
	return &Format{
		template: template,
		reset:    reset,
		loc:      cfg.loc,
		tokens:   tokens,
	}, nil
}

// Template returns the template string the Format was built from.
func (f *Format) Template() string { return f.template }

// Reset returns the Format's reset policy.
func (f *Format) Reset() Reset { return f.reset }

// Location returns the timezone the Format computes civil periods and date
// tokens in (never nil; UTC by default).
func (f *Format) Location() *time.Location { return f.loc }

// Render formats an explicit seq for scope at instant at, without touching a
// Store — for previewing a template, or numbering when the caller already holds
// the counter. Date tokens and the period are computed in the Format's timezone.
// seq must be >= 1; a smaller value is [ErrBadSeq]. Render never fails on the
// template.
func (f *Format) Render(scope Scope, seq int64, at time.Time) (string, error) {
	if seq < 1 {
		return "", fmt.Errorf("%w: got %d", ErrBadSeq, seq)
	}
	t := at.In(f.loc)
	var b strings.Builder
	for _, tok := range f.tokens {
		switch tok.kind {
		case litToken:
			b.WriteString(tok.lit)
		case yearToken:
			b.WriteString(pad(int64(t.Year()), 4))
		case year2Token:
			b.WriteString(pad(int64(t.Year()%100), 2))
		case monthToken:
			b.WriteString(pad(int64(t.Month()), 2))
		case dayToken:
			b.WriteString(pad(int64(t.Day()), 2))
		case seqToken:
			b.WriteString(pad(seq, tok.width))
		case tenantToken:
			b.WriteString(scope.Tenant)
		case seriesToken:
			b.WriteString(scope.Series)
		}
	}
	return b.String(), nil
}

// Allocate produces the next formatted number for scope at instant at: it
// derives the period key from at and the reset policy, asks the Store for the
// next counter under (scope, period), and renders the template. One call, one
// number.
//
// Allocate takes no lock and holds none across the call into s — the Store owns
// its own atomicity, and that is where gaplessness lives. A nil Store is
// [ErrNilStore]; an error from the Store propagates unwrapped so the caller's
// own errors.Is checks still work; a Store that violates its contract by
// returning a counter below 1 surfaces as [ErrBadSeq].
func (f *Format) Allocate(ctx context.Context, s Store, scope Scope, at time.Time) (string, error) {
	if s == nil {
		return "", ErrNilStore
	}
	seq, err := s.Next(ctx, f.Key(scope, at))
	if err != nil {
		return "", err
	}
	return f.Render(scope, seq, at)
}

// Key is the exact Store key [Format.Allocate] would use for scope at instant
// at: the scope's Tenant and Series plus the reset period of at. It is exported
// so a caller can pre-lock or inspect the counter row (for the FOR UPDATE
// pattern) before allocating.
//
// The key is a length-prefixed encoding of (Tenant, Series, period): each part
// is written as its byte length in decimal, a colon, then the bytes. This
// framing is injective — no two distinct triples can produce the same key — so
// adversarial Tenant/Series values (containing colons, digits, or any delimiter)
// can never collide two separate scopes onto one counter. See docs/DESIGN.md,
// "Correction — the period key must be collision-proof".
func (f *Format) Key(scope Scope, at time.Time) string {
	return encodeKey(scope.Tenant, scope.Series, f.period(at))
}

// period is the reset period of at, computed in the Format's timezone so that
// "which civil year/month/day is this" cannot flip across a UTC boundary.
func (f *Format) period(at time.Time) string {
	t := at.In(f.loc)
	switch f.reset {
	case Yearly:
		return pad(int64(t.Year()), 4)
	case Monthly:
		return pad(int64(t.Year()), 4) + "-" + pad(int64(t.Month()), 2)
	case Daily:
		return pad(int64(t.Year()), 4) + "-" + pad(int64(t.Month()), 2) + "-" + pad(int64(t.Day()), 2)
	default: // Never
		return ""
	}
}

// encodeKey joins parts with a length-prefixed framing (<len>:<bytes> per part)
// that is injective over arbitrary byte strings: distinct part tuples always
// yield distinct keys, so no caller-supplied Tenant or Series can forge a
// collision with another scope.
func encodeKey(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(strconv.Itoa(len(p)))
		b.WriteByte(':')
		b.WriteString(p)
	}
	return b.String()
}

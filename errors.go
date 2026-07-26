package sequent

import "errors"

// Sentinel errors. NewFormat reports template problems at construction time;
// Render and Allocate report a bad counter value at call time. Match them with
// [errors.Is] rather than by comparing error strings — the wrapped errors carry
// a descriptive suffix (the offending token, width, or template) that is not
// part of the stable contract.
var (
	// ErrBadTemplate is returned by [NewFormat] when a template is malformed:
	// empty, an unknown token such as {bogus}, a malformed {seq:N} width, an
	// empty {} token, or an unbalanced brace. Every construction-time template
	// error wraps this sentinel, so errors.Is(err, ErrBadTemplate) matches them
	// all; the message names the specific fault.
	ErrBadTemplate = errors.New("sequent: malformed template")

	// ErrNoSeq is returned by [NewFormat] when a template is otherwise well
	// formed but contains no {seq} (or {seq:N}) token. A sequence number needs a
	// place to render; a template without one is a constant string, which sequent
	// treats as a mistake rather than silently numbering nothing.
	ErrNoSeq = errors.New("sequent: template has no {seq} token")

	// ErrBadReset is returned by [NewFormat] when the reset policy is not one of
	// [Never], [Yearly], [Monthly] or [Daily].
	ErrBadReset = errors.New("sequent: unknown reset policy")

	// ErrNilLocation is returned by [NewFormat] when [InLocation] is given a nil
	// *time.Location. Omit the option entirely for the default (UTC) instead.
	ErrNilLocation = errors.New("sequent: nil location")

	// ErrBadSeq is returned by [Format.Render] (and by [Format.Allocate] if a
	// Store hands back a non-positive counter) when a sequence value is less than
	// 1. A gapless counter is 1, 2, 3, …; zero or negative has no rendering.
	ErrBadSeq = errors.New("sequent: seq must be >= 1")

	// ErrNilStore is returned by [Format.Allocate] when the Store argument is nil.
	ErrNilStore = errors.New("sequent: nil Store")
)

package sequent

import (
	"fmt"
	"strconv"
	"strings"
)

// maxSeqWidth caps the zero-pad width a {seq:N} token may request. Padding is a
// minimum width, so this bounds only the leading zeros a template can demand,
// not the magnitude of a counter (a large seq renders in full regardless). The
// cap keeps a hostile or fat-fingered template — {seq:999999999} — from asking
// Render to build a gigabyte of zeros. Thirty-two digits is already past any
// real numbering scheme.
const maxSeqWidth = 32

// tokenKind identifies one compiled template token.
type tokenKind uint8

const (
	litToken    tokenKind = iota // a run of literal text (including escaped braces)
	yearToken                    // {year}  -> 4-digit civil year
	year2Token                   // {year2} -> 2-digit civil year
	monthToken                   // {month} -> 2-digit month
	dayToken                     // {day}   -> 2-digit day
	seqToken                     // {seq} / {seq:N} -> the counter, min-width N
	tenantToken                  // {tenant} -> scope.Tenant verbatim
	seriesToken                  // {series} -> scope.Series verbatim
)

// token is one compiled unit of a template. lit holds literal text for
// litToken; width holds the minimum zero-pad width for seqToken; both are zero
// for the date and scope tokens.
type token struct {
	kind  tokenKind
	lit   string
	width int
}

// parseTemplate compiles a template into a token stream once, at construction
// time, so that Render and Allocate on the resulting Format can never fail on
// the template. It returns an error wrapping [ErrBadTemplate] for a malformed
// template, or [ErrNoSeq] for a well-formed template with no {seq} token.
//
// Tokens: {year} {year2} {month} {day} {seq} {seq:N} {tenant} {series}. The
// sequences {{ and }} emit literal { and } respectively; any other unescaped
// brace is unbalanced and rejected.
func parseTemplate(tmpl string) ([]token, error) {
	if tmpl == "" {
		return nil, fmt.Errorf("%w: empty template", ErrBadTemplate)
	}

	var toks []token
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			toks = append(toks, token{kind: litToken, lit: lit.String()})
			lit.Reset()
		}
	}

	hasSeq := false
	for i := 0; i < len(tmpl); {
		switch c := tmpl[i]; c {
		case '{':
			if i+1 < len(tmpl) && tmpl[i+1] == '{' {
				lit.WriteByte('{')
				i += 2
				continue
			}
			rel := strings.IndexByte(tmpl[i+1:], '}')
			if rel < 0 {
				return nil, fmt.Errorf("%w: unbalanced '{' in %q", ErrBadTemplate, tmpl)
			}
			tok, err := parseToken(tmpl[i+1 : i+1+rel])
			if err != nil {
				return nil, err
			}
			flush()
			toks = append(toks, tok)
			hasSeq = hasSeq || tok.kind == seqToken
			i += 1 + rel + 1
		case '}':
			if i+1 < len(tmpl) && tmpl[i+1] == '}' {
				lit.WriteByte('}')
				i += 2
				continue
			}
			return nil, fmt.Errorf("%w: unbalanced '}' in %q", ErrBadTemplate, tmpl)
		default:
			lit.WriteByte(c)
			i++
		}
	}
	flush()

	if !hasSeq {
		return nil, fmt.Errorf("%w: %q", ErrNoSeq, tmpl)
	}
	return toks, nil
}

// parseToken compiles the text between a single (non-escaped) { } pair.
func parseToken(spec string) (token, error) {
	switch spec {
	case "":
		return token{}, fmt.Errorf("%w: empty token {}", ErrBadTemplate)
	case "year":
		return token{kind: yearToken}, nil
	case "year2":
		return token{kind: year2Token}, nil
	case "month":
		return token{kind: monthToken}, nil
	case "day":
		return token{kind: dayToken}, nil
	case "seq":
		return token{kind: seqToken}, nil
	case "tenant":
		return token{kind: tenantToken}, nil
	case "series":
		return token{kind: seriesToken}, nil
	}

	if width, ok := strings.CutPrefix(spec, "seq:"); ok {
		w, err := strconv.Atoi(width)
		if err != nil || w < 0 {
			return token{}, fmt.Errorf("%w: malformed seq width in {%s}", ErrBadTemplate, spec)
		}
		if w > maxSeqWidth {
			return token{}, fmt.Errorf("%w: seq width %d in {%s} exceeds max %d", ErrBadTemplate, w, spec, maxSeqWidth)
		}
		return token{kind: seqToken, width: w}, nil
	}

	return token{}, fmt.Errorf("%w: unknown token {%s}", ErrBadTemplate, spec)
}

// pad renders n in base 10, left-padded with zeros to a minimum of width
// digits. It never truncates: a value wider than width renders in full, so pad
// is a floor on width, not a ceiling on magnitude.
func pad(n int64, width int) string {
	s := strconv.FormatInt(n, 10)
	if len(s) >= width {
		return s
	}
	var b strings.Builder
	b.Grow(width)
	for i := len(s); i < width; i++ {
		b.WriteByte('0')
	}
	b.WriteString(s)
	return b.String()
}

package sequent

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// feb14 is the reference instant used by the render golden vectors: a civil date
// of 2026-02-14 in UTC.
var feb14 = time.Date(2026, time.February, 14, 10, 30, 0, 0, time.UTC)

func mustFormat(t *testing.T, template string, reset Reset, opts ...Option) *Format {
	t.Helper()
	f, err := NewFormat(template, reset, opts...)
	if err != nil {
		t.Fatalf("NewFormat(%q): unexpected error: %v", template, err)
	}
	return f
}

// --- Golden vector 1 + 2: Render and padding -------------------------------

func TestGoldenRender(t *testing.T) {
	t.Run("Given INV-{year}-{seq:05} yearly", func(t *testing.T) {
		f := mustFormat(t, "INV-{year}-{seq:05}", Yearly)
		t.Run("When rendering seq 42 at 2026-02-14", func(t *testing.T) {
			got, err := f.Render(Scope{}, 42, feb14)
			t.Run("Then it is INV-2026-00042", func(t *testing.T) {
				if err != nil {
					t.Fatalf("Render: %v", err)
				}
				if got != "INV-2026-00042" {
					t.Fatalf("Render = %q, want INV-2026-00042", got)
				}
			})
		})
		t.Run("When rendering seq 43", func(t *testing.T) {
			got, _ := f.Render(Scope{}, 43, feb14)
			t.Run("Then it is INV-2026-00043", func(t *testing.T) {
				if got != "INV-2026-00043" {
					t.Fatalf("Render = %q, want INV-2026-00043", got)
				}
			})
		})
	})
}

func TestGoldenPadding(t *testing.T) {
	t.Run("Given a {seq:05} template", func(t *testing.T) {
		f := mustFormat(t, "{seq:05}", Never)
		cases := []struct {
			seq  int64
			want string
		}{
			{42, "00042"},      // pads to width 5
			{100000, "100000"}, // wider than 5 -> no truncation
			{1, "00001"},       // low boundary
			{99999, "99999"},   // exactly width 5
		}
		for _, tc := range cases {
			t.Run("When rendering "+tc.want, func(t *testing.T) {
				got, _ := f.Render(Scope{}, tc.seq, feb14)
				t.Run("Then it is "+tc.want, func(t *testing.T) {
					if got != tc.want {
						t.Fatalf("Render(%d) = %q, want %q", tc.seq, got, tc.want)
					}
				})
			})
		}
	})
	t.Run("Given a bare {seq} template", func(t *testing.T) {
		f := mustFormat(t, "{seq}", Never)
		t.Run("When rendering seq 42", func(t *testing.T) {
			got, _ := f.Render(Scope{}, 42, feb14)
			t.Run("Then it is 42 (no padding)", func(t *testing.T) {
				if got != "42" {
					t.Fatalf("Render = %q, want 42", got)
				}
			})
		})
	})
	t.Run("Given an explicit-zero-width {seq:0} template", func(t *testing.T) {
		f := mustFormat(t, "{seq:0}", Never)
		t.Run("When rendering seq 42", func(t *testing.T) {
			got, _ := f.Render(Scope{}, 42, feb14)
			t.Run("Then width 0 means no padding", func(t *testing.T) {
				if got != "42" {
					t.Fatalf("Render = %q, want 42", got)
				}
			})
		})
	})
}

// --- Golden vector 6: tokens -----------------------------------------------

func TestGoldenTokens(t *testing.T) {
	t.Run("Given a template exercising every token", func(t *testing.T) {
		// Every token must sit in a seq-bearing, valid template; the {{ }} escape
		// is verified inline as {literal}.
		f := mustFormat(t, "{year}|{year2}|{month}|{day}|{tenant}|{series}|{{literal}}|{seq}", Never)
		t.Run("When rendering for tenant acme / series INV at 2026-02-14", func(t *testing.T) {
			got, err := f.Render(Scope{Tenant: "acme", Series: "INV"}, 7, feb14)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			want := "2026|26|02|14|acme|INV|{literal}|7"
			t.Run("Then each token renders as specified", func(t *testing.T) {
				if got != want {
					t.Fatalf("Render = %q, want %q", got, want)
				}
			})
		})
	})
	t.Run("Given a template that is only escaped braces plus a seq", func(t *testing.T) {
		f := mustFormat(t, "{{}}{seq}", Never)
		t.Run("Then {{ and }} emit literal { and }", func(t *testing.T) {
			got, _ := f.Render(Scope{}, 1, feb14)
			if got != "{}1" {
				t.Fatalf("Render = %q, want {}1", got)
			}
		})
	})
}

// --- Golden vector 7: timezone period boundary -----------------------------

func TestGoldenTimezone(t *testing.T) {
	t.Run("Given the instant 2027-01-01T00:30:00+09:00", func(t *testing.T) {
		tokyo := time.FixedZone("JST", 9*60*60)
		instant := time.Date(2027, time.January, 1, 0, 30, 0, 0, tokyo)

		t.Run("When the Format is UTC (default)", func(t *testing.T) {
			f := mustFormat(t, "{year}-{seq}", Yearly)
			t.Run("Then the period is 2026 (it is 15:30 Dec 31 UTC)", func(t *testing.T) {
				if got := f.period(instant); got != "2026" {
					t.Fatalf("period = %q, want 2026", got)
				}
			})
			t.Run("Then {year} also renders 2026, consistent with the period", func(t *testing.T) {
				got, _ := f.Render(Scope{}, 1, instant)
				if got != "2026-1" {
					t.Fatalf("Render = %q, want 2026-1", got)
				}
			})
		})

		t.Run("When the Format is InLocation(+09:00)", func(t *testing.T) {
			f := mustFormat(t, "{year}-{seq}", Yearly, InLocation(tokyo))
			t.Run("Then the period is 2027 (local civil year)", func(t *testing.T) {
				if got := f.period(instant); got != "2027" {
					t.Fatalf("period = %q, want 2027", got)
				}
			})
			t.Run("Then {year} renders 2027", func(t *testing.T) {
				got, _ := f.Render(Scope{}, 1, instant)
				if got != "2027-1" {
					t.Fatalf("Render = %q, want 2027-1", got)
				}
			})
		})
	})
}

func TestYear2LowValue(t *testing.T) {
	t.Run("Given a {year2} template and a year like 2005", func(t *testing.T) {
		f := mustFormat(t, "{year2}-{seq}", Never)
		t.Run("Then the 2-digit year zero-pads to 05", func(t *testing.T) {
			at := time.Date(2005, time.March, 1, 0, 0, 0, 0, time.UTC)
			got, _ := f.Render(Scope{}, 1, at)
			if got != "05-1" {
				t.Fatalf("Render = %q, want 05-1", got)
			}
		})
	})
}

// --- NewFormat validation --------------------------------------------------

func TestNewFormatValidation(t *testing.T) {
	t.Run("Given assorted templates and options", func(t *testing.T) {
		cases := []struct {
			name     string
			template string
			reset    Reset
			opts     []Option
			want     error // nil means valid
		}{
			{name: "well-formed", template: "INV-{year}-{seq:05}", reset: Yearly},
			{name: "bare seq only", template: "{seq}", reset: Never},
			{name: "all tokens", template: "{year}{year2}{month}{day}{tenant}{series}{seq}", reset: Daily},
			{name: "escaped braces with seq", template: "{{x}}{seq}", reset: Never},

			{name: "empty template", template: "", reset: Never, want: ErrBadTemplate},
			{name: "unknown token", template: "{bogus}{seq}", reset: Never, want: ErrBadTemplate},
			{name: "empty token", template: "{}{seq}", reset: Never, want: ErrBadTemplate},
			{name: "malformed seq width x", template: "{seq:x}", reset: Never, want: ErrBadTemplate},
			{name: "negative seq width", template: "{seq:-1}", reset: Never, want: ErrBadTemplate},
			{name: "empty seq width", template: "{seq:}", reset: Never, want: ErrBadTemplate},
			{name: "seq width over max", template: "{seq:33}", reset: Never, want: ErrBadTemplate},
			{name: "unbalanced open brace", template: "{year", reset: Never, want: ErrBadTemplate},
			{name: "unbalanced close brace", template: "year}{seq}", reset: Never, want: ErrBadTemplate},
			{name: "lone close after seq", template: "{seq}}", reset: Never, want: ErrBadTemplate},

			{name: "no seq token", template: "INV-{year}", reset: Yearly, want: ErrNoSeq},
			{name: "constant string", template: "static", reset: Never, want: ErrNoSeq},

			{name: "unknown reset", template: "{seq}", reset: Reset(9), want: ErrBadReset},

			{name: "nil location", template: "{seq}", reset: Never, opts: []Option{InLocation(nil)}, want: ErrNilLocation},
		}
		for _, tc := range cases {
			t.Run("When constructing "+tc.name, func(t *testing.T) {
				t.Run("Then NewFormat reports the expected result", func(t *testing.T) {
					f, err := NewFormat(tc.template, tc.reset, tc.opts...)
					if tc.want == nil {
						if err != nil {
							t.Fatalf("unexpected error: %v", err)
						}
						if f == nil {
							t.Fatal("nil Format for a valid template")
						}
						return
					}
					if !errors.Is(err, tc.want) {
						t.Fatalf("error = %v, want %v", err, tc.want)
					}
					if f != nil {
						t.Fatalf("Format = %v, want nil on error", f)
					}
				})
			})
		}
	})
}

// --- seq <= 0 rule ----------------------------------------------------------

func TestRenderRejectsNonPositiveSeq(t *testing.T) {
	t.Run("Given a valid Format", func(t *testing.T) {
		f := mustFormat(t, "INV-{seq:05}", Yearly)
		for _, seq := range []int64{0, -1, -100} {
			t.Run("When rendering a non-positive seq", func(t *testing.T) {
				t.Run("Then Render returns ErrBadSeq and no string", func(t *testing.T) {
					got, err := f.Render(Scope{}, seq, feb14)
					if !errors.Is(err, ErrBadSeq) {
						t.Fatalf("Render(%d) error = %v, want ErrBadSeq", seq, err)
					}
					if got != "" {
						t.Fatalf("Render(%d) = %q, want empty on error", seq, got)
					}
				})
			})
		}
	})
}

// --- Accessors and Reset.String --------------------------------------------

func TestAccessors(t *testing.T) {
	t.Run("Given a Format built with an explicit location", func(t *testing.T) {
		loc := time.FixedZone("X", 3600)
		f := mustFormat(t, "INV-{seq}", Monthly, InLocation(loc))
		t.Run("Then Template, Reset and Location echo the construction", func(t *testing.T) {
			if f.Template() != "INV-{seq}" {
				t.Fatalf("Template() = %q", f.Template())
			}
			if f.Reset() != Monthly {
				t.Fatalf("Reset() = %v, want Monthly", f.Reset())
			}
			if f.Location() != loc {
				t.Fatalf("Location() = %v, want %v", f.Location(), loc)
			}
		})
	})
	t.Run("Given the default (no location option)", func(t *testing.T) {
		f := mustFormat(t, "{seq}", Never)
		t.Run("Then Location defaults to UTC", func(t *testing.T) {
			if f.Location() != time.UTC {
				t.Fatalf("Location() = %v, want UTC", f.Location())
			}
		})
	})
}

func TestResetString(t *testing.T) {
	t.Run("Given each reset policy", func(t *testing.T) {
		cases := []struct {
			r    Reset
			want string
		}{
			{Never, "Never"},
			{Yearly, "Yearly"},
			{Monthly, "Monthly"},
			{Daily, "Daily"},
			{Reset(42), "Reset(42)"},
		}
		for _, tc := range cases {
			t.Run("Then "+tc.want, func(t *testing.T) {
				if got := tc.r.String(); got != tc.want {
					t.Fatalf("Reset(%d).String() = %q, want %q", tc.r, got, tc.want)
				}
			})
		}
	})
}

// Errors carry a descriptive suffix beyond the sentinel, so error strings stay
// useful for humans while errors.Is stays stable.
func TestErrorMessagesAreDescriptive(t *testing.T) {
	t.Run("Given a malformed template", func(t *testing.T) {
		_, err := NewFormat("{bogus}{seq}", Never)
		t.Run("Then the message names the offending token", func(t *testing.T) {
			if err == nil || !strings.Contains(err.Error(), "bogus") {
				t.Fatalf("error = %v, want it to mention 'bogus'", err)
			}
		})
	})
}

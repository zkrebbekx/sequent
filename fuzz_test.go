package sequent

import (
	"testing"
	"time"
)

// FuzzTemplate drives arbitrary template strings through NewFormat and asserts
// the core safety properties: parsing never panics, and a template NewFormat
// accepts always renders (for any seq >= 1) without panicking or returning a
// template error. This pins the invariant that a validated Format cannot fail on
// its template — the whole reason NewFormat exists.
func FuzzTemplate(f *testing.F) {
	seeds := []string{
		"INV-{year}-{seq:05}",
		"{seq}",
		"{{literal}}{seq}",
		"{year2}{month}{day}-{seq}",
		"{tenant}/{series}/{seq:3}",
		"",
		"{bogus}",
		"{seq:x}",
		"{seq:-1}",
		"{unbalanced",
		"trailing}",
		"{}",
		"{seq:99}",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	at := time.Date(2026, time.February, 14, 12, 0, 0, 0, time.UTC)
	scope := Scope{Tenant: "t", Series: "s"}

	f.Fuzz(func(t *testing.T, template string) {
		form, err := NewFormat(template, Yearly)
		if err != nil {
			// A rejected template must yield no Format.
			if form != nil {
				t.Fatalf("NewFormat returned a Format alongside error %v", err)
			}
			return
		}
		// A Format NewFormat accepted must render for any positive seq without a
		// template error and without panicking.
		for _, seq := range []int64{1, 42, 100000, 1 << 40} {
			got, rerr := form.Render(scope, seq, at)
			if rerr != nil {
				t.Fatalf("Render(%d) on accepted template %q errored: %v", seq, template, rerr)
			}
			if got == "" && template != "" {
				t.Fatalf("Render produced empty output for non-empty template %q", template)
			}
		}
		// A non-positive seq must always be rejected, never rendered.
		if _, rerr := form.Render(scope, 0, at); rerr == nil {
			t.Fatalf("Render(0) on %q did not reject a non-positive seq", template)
		}
	})
}

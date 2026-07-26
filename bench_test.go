package sequent

import (
	"context"
	"testing"
	"time"
)

var (
	benchStr string
	benchErr error
)

// BenchmarkRender measures the pure template render for a realistic invoice
// format with a date, padded counter, and scope tokens.
func BenchmarkRender(b *testing.B) {
	f := mustFormatB(b, "{tenant}-INV-{year}{month}-{seq:06}", Monthly)
	scope := Scope{Tenant: "acme", Series: "INV"}
	at := time.Date(2026, time.February, 14, 12, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchStr, benchErr = f.Render(scope, int64(i+1), at)
	}
}

// BenchmarkAllocate measures a full Allocate against the memory Store: key
// derivation, the mutex-guarded increment, and the render.
func BenchmarkAllocate(b *testing.B) {
	f := mustFormatB(b, "INV-{year}-{seq:05}", Yearly)
	store := NewMemStore()
	scope := Scope{Tenant: "acme"}
	at := time.Date(2026, time.February, 14, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchStr, benchErr = f.Allocate(ctx, store, scope, at)
	}
}

// BenchmarkKey isolates the collision-proof key encoding.
func BenchmarkKey(b *testing.B) {
	f := mustFormatB(b, "INV-{seq}", Daily)
	scope := Scope{Tenant: "acme", Series: "INV"}
	at := time.Date(2026, time.February, 14, 12, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchStr = f.Key(scope, at)
	}
}

func mustFormatB(b *testing.B, template string, reset Reset) *Format {
	b.Helper()
	f, err := NewFormat(template, reset)
	if err != nil {
		b.Fatalf("NewFormat: %v", err)
	}
	return f
}

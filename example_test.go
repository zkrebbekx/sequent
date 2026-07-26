package sequent_test

import (
	"context"
	"fmt"
	"time"

	"github.com/zkrebbekx/sequent"
)

// Example allocates three consecutive invoice numbers for one tenant, all in the
// same year, against the in-memory Store.
func Example() {
	f, _ := sequent.NewFormat("INV-{year}-{seq:05}", sequent.Yearly)
	store := sequent.NewMemStore()
	at := time.Date(2026, time.February, 14, 10, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		num, _ := f.Allocate(context.Background(), store, sequent.Scope{Tenant: "acme"}, at)
		fmt.Println(num)
	}
	// Output:
	// INV-2026-00001
	// INV-2026-00002
	// INV-2026-00003
}

// Example_yearlyReset shows the counter restarting at 1 when the civil year
// rolls over, because the period key changes from 2026 to 2027.
func Example_yearlyReset() {
	f, _ := sequent.NewFormat("INV-{year}-{seq:05}", sequent.Yearly)
	store := sequent.NewMemStore()
	scope := sequent.Scope{Tenant: "acme"}

	dec := time.Date(2026, time.December, 31, 23, 0, 0, 0, time.UTC)
	jan := time.Date(2027, time.January, 1, 0, 30, 0, 0, time.UTC)

	last2026, _ := f.Allocate(context.Background(), store, scope, dec)
	first2027, _ := f.Allocate(context.Background(), store, scope, jan)
	fmt.Println(last2026)
	fmt.Println(first2027)
	// Output:
	// INV-2026-00001
	// INV-2027-00001
}

// Example_render previews a template for an explicit counter without a Store,
// showing that {seq:05} is a minimum width, not a truncating one.
func Example_render() {
	f, _ := sequent.NewFormat("INV-{year}-{seq:05}", sequent.Yearly)
	at := time.Date(2026, time.February, 14, 0, 0, 0, 0, time.UTC)

	small, _ := f.Render(sequent.Scope{}, 42, at)
	big, _ := f.Render(sequent.Scope{}, 100000, at)
	fmt.Println(small)
	fmt.Println(big)
	// Output:
	// INV-2026-00042
	// INV-2026-100000
}

// Example_timezone shows the civil period following the Format's timezone: an
// instant just after midnight in +09:00 is still the previous year in UTC.
func Example_timezone() {
	tokyo := time.FixedZone("JST", 9*60*60)
	instant := time.Date(2027, time.January, 1, 0, 30, 0, 0, tokyo)

	utc, _ := sequent.NewFormat("{year}-{seq}", sequent.Yearly)
	local, _ := sequent.NewFormat("{year}-{seq}", sequent.Yearly, sequent.InLocation(tokyo))

	u, _ := utc.Render(sequent.Scope{}, 1, instant)
	l, _ := local.Render(sequent.Scope{}, 1, instant)
	fmt.Println(u)
	fmt.Println(l)
	// Output:
	// 2026-1
	// 2027-1
}

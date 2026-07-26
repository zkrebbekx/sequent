//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
)

// A scenario is a named preset: a small, accurate configuration for one panel
// that teaches a single idea in about fifteen seconds. It carries the inputs the
// page loads into a panel's controls, which panel to focus, a short blurb for
// the button, and a one-line note on what to watch. The numbers are pinned to
// the library's golden vectors, so loading one and clicking through reproduces a
// figure from the README exactly.
type scenario struct {
	Name   string         `json:"name"`
	Title  string         `json:"title"`
	Blurb  string         `json:"blurb"`
	Panel  string         `json:"panel"`
	Note   string         `json:"note"`
	Config map[string]any `json:"config"`
}

// scenarios are the loadable presets, keyed by name.
var scenarios = map[string]*scenario{
	"german-invoice-run": {
		Name:  "german-invoice-run",
		Title: "German invoice run (Yearly)",
		Blurb: "A gapless, year-scoped invoice number — the unbroken run a tax authority requires.",
		Panel: "ledger",
		Note: "Template RE-{year}-{seq:04} with a Yearly reset. Click Allocate to watch the counter march " +
			"RE-2026-0001, 0002, 0003 — one committed transaction, one number, no gap. Change the template and " +
			"the live preview follows; a bad token is a construction error, not a silent literal.",
		Config: map[string]any{
			"template": "RE-{year}-{seq:04}",
			"reset":    "yearly",
			"tenant":   "handel-gmbh",
			"series":   "RE",
			"at":       "2026-03-14",
		},
	},

	"two-tenants": {
		Name:  "two-tenants",
		Title: "Two tenants, no collision",
		Blurb: "acme and globex allocate at the same instant. Both start at 1. Their counters never touch.",
		Panel: "scope",
		Note: "One Store, two scopes. acme and globex allocate the same template at the same instant and both " +
			"start at 1, because the Store key includes the tenant. The key is length-prefixed (4:acme…), so even " +
			"a tenant that contains the delimiter cannot merge two runs into one.",
		Config: map[string]any{
			"a":     map[string]any{"tenant": "acme", "series": "INV"},
			"b":     map[string]any{"tenant": "globex", "series": "INV"},
			"reset": "yearly",
			"at":    "2026-06-01",
		},
	},

	"new-year-reset": {
		Name:  "new-year-reset",
		Title: "New Year reset",
		Blurb: "Allocate across Dec 31 → Jan 1 under Yearly and watch the sequence fall back to 1.",
		Panel: "reset",
		Note: "Under a Yearly reset, allocate a few numbers on Dec 31 2026, then advance the clock to Jan 1 2027 " +
			"and allocate again: the number rolls to INV-2027-00001. The reset is not a special case — the period " +
			"key changed from 2026 to 2027, so the Store starts a fresh counter. Try Monthly and Daily too.",
		Config: map[string]any{
			"reset": "yearly",
			"at":    "2026-12-30",
		},
	},

	"crash-mid-transaction": {
		Name:  "crash-mid-transaction",
		Title: "Crash mid-transaction",
		Blurb: "Allocate, then let the document fail. In the trap a number burns; bound to the transaction it does not.",
		Panel: "gapless",
		Note: "Gaplessness is a durability property. Bind the allocate to the document's transaction (correct): a " +
			"failed save rolls back and consumes no number. Allocate first, then save in a separate step (the trap): " +
			"a failed save leaves the counter advanced, so the run gaps — …00007, [gap], 00009.",
		Config: map[string]any{
			"mode": "commit",
		},
	},
}

// scenarioOrder fixes the presentation order in the UI, the flagship ledger
// story first.
var scenarioOrder = []string{"german-invoice-run", "two-tenants", "new-year-reset", "crash-mid-transaction"}

func scenarioList() []map[string]any {
	out := make([]map[string]any, 0, len(scenarioOrder))
	for _, name := range scenarioOrder {
		sc, ok := scenarios[name]
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"name":  sc.Name,
			"title": sc.Title,
			"blurb": sc.Blurb,
			"panel": sc.Panel,
		})
	}
	return out
}

// handleLoadScenario returns a scenario's full preset by name. The name may
// arrive as a bare string or as {"name": ...}.
func handleLoadScenario(in []byte) (any, error) {
	name := scenarioName(in)
	sc, ok := scenarios[name]
	if !ok {
		return nil, badArg("unknown_scenario", fmt.Sprintf("unknown scenario %q", name))
	}
	return sc, nil
}

// scenarioName extracts the requested name from a bare string or a {name}
// object, so loadScenario("two-tenants") and loadScenario({name:"..."}) both
// work.
func scenarioName(in []byte) string {
	s := string(in)
	if s == "" || s == "{}" {
		return ""
	}
	if s[0] != '{' {
		return s
	}
	var obj struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(in, &obj); err == nil {
		return obj.Name
	}
	return ""
}

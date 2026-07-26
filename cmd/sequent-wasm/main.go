//go:build js && wasm

// Command sequent-wasm is the browser playground's engine: the sequent
// document-numbering core compiled to WebAssembly, exposing a small JSON API on
// the global "sequent" object. It has no backend — every call runs in the
// visitor's tab over the pure library, and nothing leaves the page.
//
// The library's honest split is carried faithfully across the JS boundary.
// sequent owns the deterministic parts (the template render, the period key,
// per-scope isolation, the padding), and the atomic durable increment is a
// Store the caller supplies. So this bridge holds per-session Stores in Go maps
// keyed by an id the page passes: each ledger keeps its own counter, marching
// 00001, 00002, 00003…, across many Allocate calls, and a reset call clears one.
//
// Two Store kinds are held. The ledger, scope-isolation and reset panels use the
// library's reference *sequent.MemStore — the gapless in-process counter the
// conformance tests run against. The gapless-vs-trap panel needs a transaction
// it can roll back, which MemStore deliberately does not offer (rollback lives
// in the caller's database, not in the library), so that one panel drives a tiny
// txStore that models a commit or a rollback. Both are real sequent.Store
// implementations; Format.Allocate does the identical work over either.
//
// This file carries the js && wasm build constraint so the native toolchain —
// go build ./..., go vet ./..., go test ./..., golangci-lint — never tries to
// compile it (syscall/js exists only under GOOS=js). The root module stays
// zero-dependency: syscall/js is stdlib and adds nothing to go.sum, and this
// command imports only the sequent library beside it.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/zkrebbekx/sequent"
)

func main() {
	obj := map[string]any{}
	register(obj, "render", handleRender)
	register(obj, "allocate", handleAllocate)
	register(obj, "crash", handleCrash)
	register(obj, "reset", handleReset)
	register(obj, "loadScenario", handleLoadScenario)
	obj["scenarios"] = toJS(scenarioList())

	js.Global().Set("sequent", js.ValueOf(obj))
	js.Global().Set("__sequentReady", js.ValueOf(true))
	if cb := js.Global().Get("__sequentOnReady"); cb.Type() == js.TypeFunction {
		cb.Invoke()
	}

	// Keep the Go runtime alive for the lifetime of the page.
	select {}
}

// register wraps a handler so every call recovers from a panic into a returned
// {error} object rather than tearing down the wasm instance, and marshals the
// result to a real JS object the page can read without parsing.
func register(obj map[string]any, name string, fn func([]byte) (any, error)) {
	obj[name] = js.FuncOf(func(_ js.Value, args []js.Value) (result any) {
		defer func() {
			if r := recover(); r != nil {
				result = toJS(map[string]any{"error": fmt.Sprintf("panic: %v", r), "code": "panic"})
			}
		}()
		out, err := fn(input(args))
		if err != nil {
			return toJS(errObj(err))
		}
		return toJS(out)
	})
}

// input normalises the first argument to JSON bytes. A JS object is stringified;
// a bare string is passed through verbatim (loadScenario accepts one); anything
// absent becomes an empty object.
func input(args []js.Value) []byte {
	if len(args) == 0 {
		return []byte("{}")
	}
	a := args[0]
	switch a.Type() {
	case js.TypeString:
		return []byte(a.String())
	case js.TypeUndefined, js.TypeNull:
		return []byte("{}")
	default:
		return []byte(js.Global().Get("JSON").Call("stringify", a).String())
	}
}

// toJS marshals a Go value and parses it back into a native JS object so the
// page receives structured data rather than a string to parse itself.
func toJS(v any) js.Value {
	b, err := json.Marshal(v)
	if err != nil {
		b, _ = json.Marshal(map[string]any{"error": err.Error(), "code": "marshal"})
	}
	return js.Global().Get("JSON").Call("parse", string(b))
}

// --- error contract ---------------------------------------------------------

// errObj renders an error as the page's {error, code} shape.
func errObj(err error) map[string]any {
	return map[string]any{"error": err.Error(), "code": errCode(err)}
}

// argErr is a bridge-minted validation error (a malformed instant, an unknown
// reset policy) distinct from a library sentinel. It carries a stable code.
type argErr struct {
	code string
	msg  string
}

func (e *argErr) Error() string { return e.msg }

func badArg(code, msg string) error { return &argErr{code: code, msg: msg} }

// errCode maps an error to a stable string code: a bridge argErr keeps its own
// code, and every sequent sentinel is mirrored onto its name. An unrecognised
// error is "error".
func errCode(err error) string {
	var a *argErr
	if errors.As(err, &a) {
		return a.code
	}
	switch {
	case errors.Is(err, sequent.ErrBadTemplate):
		return "bad_template"
	case errors.Is(err, sequent.ErrNoSeq):
		return "no_seq"
	case errors.Is(err, sequent.ErrBadReset):
		return "bad_reset"
	case errors.Is(err, sequent.ErrNilLocation):
		return "nil_location"
	case errors.Is(err, sequent.ErrBadSeq):
		return "bad_seq"
	case errors.Is(err, sequent.ErrNilStore):
		return "nil_store"
	default:
		return "error"
	}
}

// --- session Stores ---------------------------------------------------------

// maxTemplateLen bounds a template's length. A real numbering template needs a
// few dozen characters; the cap stops a hostile input from forcing a huge token
// stream in the tab. The library already caps the {seq:N} pad width at 32, so a
// giant-pad allocation is impossible; this bounds only the literal length.
const maxTemplateLen = 512

var (
	mu sync.Mutex
	// stores holds the reference MemStore for each ledger session id. A ledger's
	// counter persists here across Allocate calls, so the visible run marches.
	stores = map[string]*sequent.MemStore{}
	// txStores holds the transactional store for each gapless-panel session id.
	txStores = map[string]*txStore{}
)

// memStore returns the MemStore for id, creating an empty one on first use.
func memStore(id string) *sequent.MemStore {
	mu.Lock()
	defer mu.Unlock()
	s, ok := stores[id]
	if !ok {
		s = sequent.NewMemStore()
		stores[id] = s
	}
	return s
}

// txMemStore returns the transactional store for id, creating one on first use.
func txMemStore(id string) *txStore {
	mu.Lock()
	defer mu.Unlock()
	s, ok := txStores[id]
	if !ok {
		s = newTxStore()
		txStores[id] = s
	}
	return s
}

// txStore is a sequent.Store that models a durable counter with an explicit
// transaction boundary the gapless-vs-trap panel can commit or roll back. Next
// advances the committed counter; rollback undoes the last advance for a key,
// modelling a transaction that rolled back and therefore consumed no number.
// This is exactly the durability the library leaves to the caller's database.
type txStore struct {
	mu       sync.Mutex
	counters map[string]int64
}

func newTxStore() *txStore { return &txStore{counters: map[string]int64{}} }

// Next returns the next consecutive counter for key, starting at 1.
func (s *txStore) Next(_ context.Context, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters[key]++
	return s.counters[key], nil
}

// peek returns the current counter for key without advancing it.
func (s *txStore) peek(key string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counters[key]
}

// rollback undoes the last advance for key: the transaction rolled back, so the
// number it drew is returned to the pool and the next Next reuses it.
func (s *txStore) rollback(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.counters[key] > 0 {
		s.counters[key]--
	}
}

// --- shared parsing ---------------------------------------------------------

// formatSpec is the common description of a Format plus the scope and instant a
// call operates on. Every handler embeds it.
type formatSpec struct {
	Template string `json:"template"`
	Reset    string `json:"reset"`
	Tenant   string `json:"tenant"`
	Series   string `json:"series"`
	At       string `json:"at"`
}

// build compiles the Format and resolves the scope and instant, surfacing the
// exact library sentinel for a malformed template (ErrBadTemplate / ErrNoSeq).
func (spec formatSpec) build() (*sequent.Format, sequent.Scope, time.Time, error) {
	if len(spec.Template) > maxTemplateLen {
		return nil, sequent.Scope{}, time.Time{}, badArg("invalid_argument", fmt.Sprintf(
			"template must be at most %d characters; got %d", maxTemplateLen, len(spec.Template)))
	}
	reset, err := parseReset(spec.Reset)
	if err != nil {
		return nil, sequent.Scope{}, time.Time{}, err
	}
	f, err := sequent.NewFormat(spec.Template, reset)
	if err != nil {
		return nil, sequent.Scope{}, time.Time{}, err
	}
	at, err := parseTime("at", spec.At)
	if err != nil {
		return nil, sequent.Scope{}, time.Time{}, err
	}
	return f, sequent.Scope{Tenant: spec.Tenant, Series: spec.Series}, at, nil
}

// parseReset maps a UI string to a sequent.Reset. An empty string is Never,
// matching the library's zero value.
func parseReset(s string) (sequent.Reset, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "never":
		return sequent.Never, nil
	case "yearly":
		return sequent.Yearly, nil
	case "monthly":
		return sequent.Monthly, nil
	case "daily":
		return sequent.Daily, nil
	default:
		return 0, badArg("bad_reset", fmt.Sprintf(
			"reset must be one of never, yearly, monthly, daily; got %q", s))
	}
}

// parseTime accepts an RFC 3339 instant (with any offset, so the timezone panel
// can pass +09:00) or a bare date, which is read as UTC midnight. An empty value
// falls back to the current instant so a fresh ledger still allocates.
func parseTime(field, value string) (time.Time, error) {
	if value == "" {
		return time.Now().UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, badArg("invalid_argument", fmt.Sprintf(
		"%s must be an RFC 3339 instant such as 2026-01-01T00:00:00Z or a date such as 2026-01-01, got %q",
		field, value))
}

// decodeKey splits sequent's length-prefixed Store key (<len>:<bytes> per part)
// back into its three parts (tenant, series, period). The page shows these to
// prove the exact key Allocate used and that two scopes never share one.
func decodeKey(key string) (tenant, series, period string) {
	parts := make([]string, 0, 3)
	for len(key) > 0 && len(parts) < 3 {
		i := strings.IndexByte(key, ':')
		if i < 0 {
			break
		}
		n, err := strconv.Atoi(key[:i])
		if err != nil || n < 0 || i+1+n > len(key) {
			break
		}
		parts = append(parts, key[i+1:i+1+n])
		key = key[i+1+n:]
	}
	for len(parts) < 3 {
		parts = append(parts, "")
	}
	return parts[0], parts[1], parts[2]
}

// keyDTO is the decomposed Store key the page displays under a ledger.
type keyDTO struct {
	Key    string `json:"key"`
	Tenant string `json:"tenant"`
	Series string `json:"series"`
	Period string `json:"period"`
}

func keyOf(f *sequent.Format, scope sequent.Scope, at time.Time) keyDTO {
	key := f.Key(scope, at)
	t, s, p := decodeKey(key)
	return keyDTO{Key: key, Tenant: t, Series: s, Period: p}
}

// --- render (preview, no Store) ---------------------------------------------

type renderRequest struct {
	formatSpec
	Seq int64 `json:"seq"`
}

type renderResponse struct {
	Rendered string `json:"rendered"`
	Seq      int64  `json:"seq"`
	keyDTO
}

// handleRender previews the template for an explicit counter without touching a
// Store. It powers the live "next render" preview and the timezone panel, and it
// surfaces a construction error (a bad token, a missing {seq}) inline.
func handleRender(in []byte) (any, error) {
	var req renderRequest
	if err := json.Unmarshal(in, &req); err != nil {
		return nil, err
	}
	f, scope, at, err := req.build()
	if err != nil {
		return nil, err
	}
	seq := req.Seq
	if seq < 1 {
		seq = 1
	}
	rendered, err := f.Render(scope, seq, at)
	if err != nil {
		return nil, err
	}
	return renderResponse{Rendered: rendered, Seq: seq, keyDTO: keyOf(f, scope, at)}, nil
}

// --- allocate (real Store increment) ----------------------------------------

type allocateRequest struct {
	formatSpec
	Store string `json:"store"`
}

type allocateResponse struct {
	Number string `json:"number"`
	Seq    int64  `json:"seq"`
	keyDTO
}

// handleAllocate draws the next number from the session's MemStore. The counter
// persists across calls under the (scope, period) key, so the visible ledger
// marches, and it restarts at 1 whenever the period key changes.
func handleAllocate(in []byte) (any, error) {
	var req allocateRequest
	if err := json.Unmarshal(in, &req); err != nil {
		return nil, err
	}
	f, scope, at, err := req.build()
	if err != nil {
		return nil, err
	}
	st := memStore(req.Store)
	number, err := f.Allocate(context.Background(), st, scope, at)
	if err != nil {
		return nil, err
	}
	k := keyOf(f, scope, at)
	return allocateResponse{Number: number, Seq: st.Peek(k.Key), keyDTO: k}, nil
}

// --- crash (gapless vs the trap) --------------------------------------------

type crashRequest struct {
	formatSpec
	Store   string `json:"store"`
	Mode    string `json:"mode"`    // "commit" (correct) or "trap"
	Outcome string `json:"outcome"` // "ok" or "fail"
}

type crashResponse struct {
	Number    string `json:"number"`
	Seq       int64  `json:"seq"`
	Committed bool   `json:"committed"`
	Gap       bool   `json:"gap"`
	keyDTO
}

// handleCrash drives the gapless-vs-trap panel over a transactional store. Every
// call allocates a real number; what happens next models the caller's choice:
//
//   - commit + ok: the transaction commits. The number is kept.
//   - commit + fail: the transaction rolls back. The number is returned to the
//     pool, so the next allocate reuses it — no gap. This is the correct pattern.
//   - trap + fail: the number was allocated in one step, the document created in
//     another that failed. The counter already advanced, so the number is burned
//     and the run gaps. This is the Odoo trap.
func handleCrash(in []byte) (any, error) {
	var req crashRequest
	if err := json.Unmarshal(in, &req); err != nil {
		return nil, err
	}
	f, scope, at, err := req.build()
	if err != nil {
		return nil, err
	}
	st := txMemStore(req.Store)
	k := keyOf(f, scope, at)
	number, err := f.Allocate(context.Background(), st, scope, at)
	if err != nil {
		return nil, err
	}
	seq := st.peek(k.Key)
	committed, gap := true, false
	if req.Outcome == "fail" {
		if req.Mode == "trap" {
			committed, gap = false, true
		} else {
			st.rollback(k.Key)
			committed, gap = false, false
		}
	}
	return crashResponse{Number: number, Seq: seq, Committed: committed, Gap: gap, keyDTO: k}, nil
}

// --- reset (clear a session) ------------------------------------------------

type resetRequest struct {
	Store string `json:"store"`
}

// handleReset drops the session's counters (both store kinds), so a ledger
// starts again from empty.
func handleReset(in []byte) (any, error) {
	var req resetRequest
	if err := json.Unmarshal(in, &req); err != nil {
		return nil, err
	}
	mu.Lock()
	delete(stores, req.Store)
	delete(txStores, req.Store)
	mu.Unlock()
	return map[string]any{"ok": true, "store": req.Store}, nil
}

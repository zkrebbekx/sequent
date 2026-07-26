/* sequent playground — vanilla JS front end over the WebAssembly engine.
 *
 * The engine exposes a global `sequent` object (see cmd/sequent-wasm). Every
 * call takes and returns plain JS objects; a returned {error, code} means the
 * engine rejected the call. There is no network and no framework. Each ledger
 * keeps its counter in a session-keyed Store inside the wasm instance, so a run
 * marches across many Allocate calls until you clear or reload it.
 */

(function () {
  "use strict";

  let C = null; // the sequent engine (window.sequent)

  // ---- tiny helpers --------------------------------------------------------

  function $(id) { return document.getElementById(id); }
  function escapeHtml(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
  }

  // showErr surfaces an engine {error} in a panel's banner, or hides it.
  function showErr(id, resp) {
    const b = $(id);
    if (resp && resp.error) {
      b.textContent = resp.error;
      b.hidden = false;
      return true;
    }
    b.hidden = true;
    return false;
  }

  // keyHTML renders a decoded Store key: the raw length-prefixed string plus its
  // three parts, so the length-prefix framing is visible.
  function keyHTML(r) {
    return (
      `key <b>${escapeHtml(r.key)}</b> — ` +
      `tenant <code>${escapeHtml(r.tenant || "∅")}</code> · ` +
      `series <code>${escapeHtml(r.series || "∅")}</code> · ` +
      `period <code>${escapeHtml(r.period || "∅")}</code>`
    );
  }

  function scrollTo(id) {
    const e = $(id);
    if (e) e.scrollIntoView({ behavior: "smooth", block: "start" });
  }

  // addChip appends a value chip to a ledger, animating the newest one.
  function addChip(host, number, seq, cls) {
    const empty = host.querySelector(".ledger-empty");
    if (empty) empty.remove();
    const chip = document.createElement("div");
    chip.className = "chip fresh " + cls;
    chip.innerHTML = `<span class="seq-tag">seq ${escapeHtml(seq)}</span>${escapeHtml(number)}`;
    host.appendChild(chip);
    chip.addEventListener("animationend", () => chip.classList.remove("fresh"), { once: true });
    host.scrollTop = host.scrollHeight;
    return chip;
  }

  // addGap appends a burned-number marker: a number consumed by a failed
  // document, leaving a hole in the run.
  function addGap(host, number) {
    const empty = host.querySelector(".ledger-empty");
    if (empty) empty.remove();
    const g = document.createElement("div");
    g.className = "gap-marker fresh";
    g.textContent = `⦸ ${number} burned`;
    host.appendChild(g);
    g.addEventListener("animationend", () => g.classList.remove("fresh"), { once: true });
    host.scrollTop = host.scrollHeight;
  }

  function resetHost(host, emptyHTML) {
    host.innerHTML = emptyHTML || "";
  }

  // =========================================================================
  //  PANEL 1 — LIVE ALLOCATION LEDGER
  // =========================================================================

  const LG_EMPTY = '<p id="lg-empty" class="ledger-empty">No numbers yet — press <b>Allocate</b> to draw the first.</p>';
  const lgSeqByKey = {}; // last issued counter per Store key, for the preview

  function ledgerReq() {
    return {
      store: "ledger",
      template: $("lg-tmpl").value,
      reset: $("lg-reset").value,
      tenant: $("lg-tenant").value,
      series: $("lg-series").value,
      at: $("lg-at").value,
    };
  }

  function renderLedgerPreview() {
    const base = ledgerReq();
    const probe = C.render(Object.assign({}, base, { seq: 1 }));
    if (showErr("lg-err", probe)) {
      $("lg-preview").textContent = "—";
      $("lg-key").textContent = "";
      $("lg-count").textContent = "0";
      return;
    }
    const issued = lgSeqByKey[probe.key] || 0;
    const next = issued + 1;
    const r = next === 1 ? probe : C.render(Object.assign({}, base, { seq: next }));
    if (showErr("lg-err", r)) return;
    $("lg-preview").textContent = r.rendered;
    $("lg-key").innerHTML = keyHTML(r);
    $("lg-count").textContent = String(issued);
  }

  function allocateLedger() {
    const resp = C.allocate(ledgerReq());
    if (showErr("lg-err", resp)) return;
    lgSeqByKey[resp.key] = resp.seq;
    addChip($("lg-ledger"), resp.number, resp.seq, "committed");
    renderLedgerPreview();
  }

  function clearLedger() {
    C.reset({ store: "ledger" });
    for (const k in lgSeqByKey) delete lgSeqByKey[k];
    resetHost($("lg-ledger"), LG_EMPTY);
    renderLedgerPreview();
  }

  // =========================================================================
  //  PANEL 2 — SCOPE ISOLATION
  // =========================================================================

  const SC_TEMPLATE = "INV-{year}-{seq:05}";

  function scReq(side) {
    return {
      store: "scope",
      template: SC_TEMPLATE,
      reset: $("sc-reset").value,
      tenant: $(`sc-${side}-tenant`).value,
      series: $(`sc-${side}-series`).value,
      at: $("sc-at").value,
    };
  }

  function refreshScopeKeys() {
    const a = C.render(Object.assign({}, scReq("a"), { seq: 1 }));
    const b = C.render(Object.assign({}, scReq("b"), { seq: 1 }));
    if (showErr("sc-err", a) || showErr("sc-err", b)) return;
    $("sc-a-key").innerHTML = keyHTML(a);
    $("sc-b-key").innerHTML = keyHTML(b);
    const note = $("sc-collision");
    if (a.key === b.key) {
      note.className = "frac-line";
      note.innerHTML =
        `Both scopes produced the <b>same</b> key <code>${escapeHtml(a.key)}</code>, so they share one counter — set them apart to isolate.`;
    } else {
      note.className = "frac-line ok";
      note.innerHTML =
        `The two keys <b>differ</b>, so the counters never merge. The length prefix (<code>4:acme…</code>) makes the encoding injective: no tenant string can forge a collision.`;
    }
  }

  function allocateScope() {
    const a = C.allocate(scReq("a"));
    if (showErr("sc-err", a)) return;
    const b = C.allocate(scReq("b"));
    if (showErr("sc-err", b)) return;
    addChip($("sc-a-ledger"), a.number, a.seq, "committed");
    addChip($("sc-b-ledger"), b.number, b.seq, "committed");
    $("sc-a-count").textContent = String(a.seq);
    $("sc-b-count").textContent = String(b.seq);
    refreshScopeKeys();
  }

  function clearScope() {
    C.reset({ store: "scope" });
    resetHost($("sc-a-ledger"), "");
    resetHost($("sc-b-ledger"), "");
    $("sc-a-count").textContent = "0";
    $("sc-b-count").textContent = "0";
    refreshScopeKeys();
  }

  // =========================================================================
  //  PANEL 3 — PERIOD RESET
  // =========================================================================

  const RS_EMPTY = '<p id="rs-empty" class="ledger-empty">Allocate on Dec 30, then step to Jan 1 and allocate again.</p>';
  const state = {
    rsPolicy: "yearly",
    rsClock: new Date(Date.UTC(2026, 11, 30)),
    rsLastPeriod: null,
    gpMode: "commit",
  };

  function rsTemplate() {
    switch (state.rsPolicy) {
      case "monthly": return "INV-{year}{month}-{seq:04}";
      case "daily": return "INV-{year}{month}{day}-{seq:03}";
      default: return "INV-{year}-{seq:05}";
    }
  }
  function rsClockISO() { return state.rsClock.toISOString().slice(0, 10); }
  function refreshClock() { $("rs-clock").textContent = rsClockISO(); }

  function allocateReset() {
    const resp = C.allocate({
      store: "reset",
      template: rsTemplate(),
      reset: state.rsPolicy,
      tenant: "",
      series: "",
      at: rsClockISO(),
    });
    if (showErr("rs-err", resp)) return;
    const host = $("rs-ledger");
    const empty = host.querySelector(".ledger-empty");
    if (empty) empty.remove();
    if (state.rsLastPeriod !== null && resp.period !== state.rsLastPeriod) {
      const div = document.createElement("div");
      div.className = "period-divider";
      div.innerHTML = `<span class="pd-label">period ${escapeHtml(resp.period)} · counter reset → 1</span>`;
      host.appendChild(div);
    }
    addChip(host, resp.number, resp.seq, "committed");
    state.rsLastPeriod = resp.period;
  }

  function clearReset() {
    C.reset({ store: "reset" });
    state.rsLastPeriod = null;
    resetHost($("rs-ledger"), RS_EMPTY);
  }

  function setResetPolicy(policy) {
    state.rsPolicy = policy;
    ["yearly", "monthly", "daily"].forEach((p) => $(`rs-${p}`).classList.toggle("active", p === policy));
    clearReset(); // the template changes with the policy, so start a fresh run
  }

  function stepClock(fn) { fn(); refreshClock(); }

  // =========================================================================
  //  PANEL 4 — GAPLESS VS THE TRAP
  // =========================================================================

  const GP_EMPTY = '<p id="gp-empty" class="ledger-empty">Save a few documents, then fail one and watch the run.</p>';

  function gpNoteText() {
    const note = $("gp-note");
    if (state.gpMode === "commit") {
      note.className = "gp-note good";
      note.innerHTML =
        "<b>Bound to the transaction.</b> The allocate lives inside the document's transaction. " +
        "Save commits both; Fail rolls back and returns the number to the pool, so the next Save reuses it. The run stays gapless.";
    } else {
      note.className = "gp-note bad";
      note.innerHTML =
        "<b>Allocate, then create.</b> The number is drawn in one step and the document saved in another. " +
        "Save keeps it; Fail leaves the counter already advanced, so the number is burned and the run gaps.";
    }
  }

  function setGapMode(mode) {
    state.gpMode = mode;
    $("gp-correct").classList.toggle("active", mode === "commit");
    $("gp-trap").classList.toggle("active", mode === "trap");
    gpNoteText();
  }

  function gaplessAction(outcome) {
    const resp = C.crash({
      store: "gapless",
      template: "INV-{seq:05}",
      reset: "never",
      tenant: "",
      series: "",
      at: "2026-01-01",
      mode: state.gpMode,
      outcome: outcome,
    });
    if (showErr("gp-err", resp)) return;
    const host = $("gp-ledger");
    if (resp.committed) {
      addChip(host, resp.number, resp.seq, "committed");
    } else if (resp.gap) {
      addGap(host, resp.number);
    } else {
      // rolled back inside the transaction — shown struck, consumes no number
      addChip(host, resp.number, resp.seq, "rolled");
    }
  }

  function clearGapless() {
    C.reset({ store: "gapless" });
    resetHost($("gp-ledger"), GP_EMPTY);
  }

  // =========================================================================
  //  PANEL 5 — TIMEZONE
  // =========================================================================

  function renderTz(instant, offsetLabel) {
    const resp = C.render({
      template: "INV-{year}-{seq:05}",
      reset: "yearly",
      tenant: "",
      series: "",
      at: instant,
      seq: 1,
    });
    if (showErr("tz-err", resp)) return;
    let utcISO = instant;
    const d = new Date(instant);
    if (!isNaN(d.getTime())) utcISO = d.toISOString().replace(".000", "");
    const host = $("tz-out");
    host.innerHTML =
      `<div class="tz-card">` +
      `<div class="tz-num">${escapeHtml(resp.rendered)}</div>` +
      `<div class="tz-row"><span class="tzk">instant&nbsp;&nbsp;</span>${escapeHtml(instant)} <span class="tzk">(${escapeHtml(offsetLabel)})</span></div>` +
      `<div class="tz-row"><span class="tzk">in UTC&nbsp;&nbsp;&nbsp;&nbsp;</span>${escapeHtml(utcISO)}</div>` +
      `<div class="tz-row"><span class="tzk">period&nbsp;&nbsp;&nbsp;&nbsp;</span><b>${escapeHtml(resp.period)}</b></div>` +
      `<div class="tz-verdict">The Format reads the instant in <b>UTC</b> before it picks the year, so this number belongs to the <b>${escapeHtml(resp.period)}</b> run.</div>` +
      `</div>`;
  }

  function tzTokyo() {
    $("tz-tokyo").className = "btn";
    $("tz-utc").className = "btn ghost";
    renderTz("2027-01-01T00:30:00+09:00", "just after midnight in Tokyo");
  }
  function tzUtc() {
    $("tz-utc").className = "btn";
    $("tz-tokyo").className = "btn ghost";
    renderTz("2027-01-01T00:30:00Z", "just after midnight in UTC");
  }

  // =========================================================================
  //  SCENARIOS
  // =========================================================================

  function buildScenarioButtons() {
    const host = $("scenario-buttons");
    resetHost(host, "");
    const list = C.scenarios || [];
    for (const sc of list) {
      const b = document.createElement("button");
      b.className = "btn"; b.type = "button"; b.textContent = sc.title;
      b.dataset.name = sc.name;
      b.addEventListener("click", () => loadScenario(sc.name, b));
      host.appendChild(b);
    }
  }

  function loadScenario(name, btn) {
    const sc = C.loadScenario(name);
    if (!sc || sc.error) return;
    document.querySelectorAll("#scenario-buttons .btn").forEach((b) => b.classList.toggle("active", b === btn));
    const note = $("scenario-note");
    note.innerHTML = `<b>${escapeHtml(sc.title)}.</b> ${escapeHtml(sc.note)}`;
    note.hidden = false;

    const cfg = sc.config || {};
    switch (sc.panel) {
      case "ledger": applyLedgerConfig(cfg); scrollTo("panel-ledger"); break;
      case "scope": applyScopeConfig(cfg); scrollTo("panel-scope"); break;
      case "reset": applyResetConfig(cfg); scrollTo("panel-reset"); break;
      case "gapless": applyGaplessConfig(cfg); scrollTo("panel-gapless"); break;
    }
  }

  function applyLedgerConfig(cfg) {
    if (cfg.template != null) $("lg-tmpl").value = cfg.template;
    if (cfg.reset) $("lg-reset").value = cfg.reset;
    if (cfg.tenant != null) $("lg-tenant").value = cfg.tenant;
    if (cfg.series != null) $("lg-series").value = cfg.series;
    if (cfg.at) $("lg-at").value = cfg.at;
    clearLedger();
  }

  function applyScopeConfig(cfg) {
    if (cfg.a) { if (cfg.a.tenant != null) $("sc-a-tenant").value = cfg.a.tenant; if (cfg.a.series != null) $("sc-a-series").value = cfg.a.series; }
    if (cfg.b) { if (cfg.b.tenant != null) $("sc-b-tenant").value = cfg.b.tenant; if (cfg.b.series != null) $("sc-b-series").value = cfg.b.series; }
    if (cfg.reset) $("sc-reset").value = cfg.reset;
    if (cfg.at) $("sc-at").value = cfg.at;
    clearScope();
  }

  function applyResetConfig(cfg) {
    if (cfg.reset) setResetPolicy(cfg.reset);
    else clearReset();
    if (cfg.at) { state.rsClock = new Date(cfg.at + "T00:00:00Z"); refreshClock(); }
  }

  function applyGaplessConfig(cfg) {
    setGapMode(cfg.mode === "trap" ? "trap" : "commit");
    clearGapless();
  }

  // =========================================================================
  //  WIRING + BOOT
  // =========================================================================

  function wire() {
    // ledger
    ["lg-tmpl", "lg-reset", "lg-tenant", "lg-series", "lg-at"].forEach((id) =>
      $(id).addEventListener("input", renderLedgerPreview));
    $("lg-allocate").addEventListener("click", allocateLedger);
    $("lg-clear").addEventListener("click", clearLedger);

    // scope
    ["sc-a-tenant", "sc-a-series", "sc-b-tenant", "sc-b-series", "sc-reset", "sc-at"].forEach((id) =>
      $(id).addEventListener("input", clearScope));
    $("sc-allocate").addEventListener("click", allocateScope);
    $("sc-clear").addEventListener("click", clearScope);

    // reset
    $("rs-yearly").addEventListener("click", () => setResetPolicy("yearly"));
    $("rs-monthly").addEventListener("click", () => setResetPolicy("monthly"));
    $("rs-daily").addEventListener("click", () => setResetPolicy("daily"));
    $("rs-allocate").addEventListener("click", allocateReset);
    $("rs-clear").addEventListener("click", clearReset);
    $("rs-minus-day").addEventListener("click", () => stepClock(() => { state.rsClock = new Date(state.rsClock.getTime() - 86400000); }));
    $("rs-plus-day").addEventListener("click", () => stepClock(() => { state.rsClock = new Date(state.rsClock.getTime() + 86400000); }));
    $("rs-plus-month").addEventListener("click", () => stepClock(() => {
      const d = state.rsClock;
      state.rsClock = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, d.getUTCDate()));
    }));
    $("rs-newyear").addEventListener("click", () => stepClock(() => {
      state.rsClock = new Date(Date.UTC(state.rsClock.getUTCFullYear() + 1, 0, 1));
    }));

    // gapless
    $("gp-correct").addEventListener("click", () => setGapMode("commit"));
    $("gp-trap").addEventListener("click", () => setGapMode("trap"));
    $("gp-save").addEventListener("click", () => gaplessAction("ok"));
    $("gp-fail").addEventListener("click", () => gaplessAction("fail"));
    $("gp-clear").addEventListener("click", clearGapless);

    // timezone
    $("tz-tokyo").addEventListener("click", tzTokyo);
    $("tz-utc").addEventListener("click", tzUtc);
  }

  let started = false;
  function start() {
    if (started) return;
    started = true;
    C = window.sequent;

    buildScenarioButtons();
    wire();

    // initial render of every panel
    renderLedgerPreview();
    refreshScopeKeys();
    refreshClock();
    setGapMode("commit");
    tzTokyo();

    $("boot").classList.add("hidden");

    // open on the flagship story
    const flagshipBtn = document.querySelector("#scenario-buttons .btn");
    loadScenario("german-invoice-run", flagshipBtn);
  }

  function boot() {
    if (!window.Go) { fail("wasm_exec.js failed to load."); return; }
    const go = new Go();
    const url = "sequent.wasm";
    window.__sequentOnReady = () => start();
    const inst = (obj) => { go.run(obj.instance); if (window.__sequentReady) start(); };
    if (WebAssembly.instantiateStreaming) {
      WebAssembly.instantiateStreaming(fetch(url), go.importObject).then(inst).catch(() => fallback(go, url, inst));
    } else {
      fallback(go, url, inst);
    }
  }
  function fallback(go, url, inst) {
    fetch(url).then((r) => r.arrayBuffer()).then((buf) => WebAssembly.instantiate(buf, go.importObject)).then(inst)
      .catch((e) => fail("Could not load the wasm module: " + e));
  }
  function fail(msg) { const b = $("boot"); b.textContent = msg; b.classList.add("error"); }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();

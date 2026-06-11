# Squad Ops Panel Implementation Plan (M3.3a)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ops view in the serve dashboard — registry management (runtime add/remove), per-kind agent wiring with content-aware status, and seat provenance (join `client` metadata + change warnings).

**Architecture:** Bottom-up like M3.1/M3.2: (O1) `client` on join (engine+MCP+CLI, `deps`-pattern additive); (O2) serve registry dynamics + endpoints; (O3) wiring probe extraction + endpoints; (O4) seats/provenance endpoint; (O5) web ops view (three panels); (O6) embed rebuild + docs + PR. Conventions are ESTABLISHED — mirror them: author.go error shapes, author_test.go fixtures, deps' conditional-serialization tests, doctor's content probes, web api.ts client + vitest patterns. The spec (`docs/superpowers/specs/2026-06-11-squad-ops-panel-design.md`) §2/§3 carry the exact endpoint and UI contracts — treat them as normative.

**Branch:** `feat/squad-ops`.

---

### Task O1: join `client` metadata (engine + CLI + MCP + schema + addendum)

- [ ] TDD (`internal/pact/client_test.go`): JoinWithClient writes `client:{name,version}` into the join payload; plain Join (CLI wrapper) writes `pactify-cli` + injected version (plumb the version: add a package-level `var ClientVersion = "dev"` in pact set from main's version var at startup — simplest: cmd/pactify main() sets `pact.ClientVersion = version`); a join WITHOUT client metadata (e.g. constructed event) still validates + renders identically (STATE untouched by design — assert STATE.yml has no client text ever).
- [ ] Engine: `(p *Project) JoinWithClient(seatID, roles, clientName, clientVersion string)`; `Join` wraps. Payload `client` only when name non-empty. STATE/projection: NO changes (provenance lives in the log only).
- [ ] MCP (`internal/mcp/tools.go` join handler): `req.Session.InitializeParams().ClientInfo` → JoinWithClient (nil-safe: missing initialize/clientInfo → fall back to plain names empty → no client field). Test via in-process client with `Implementation{Name:"testclient",Version:"9"}` asserting the log.
- [ ] Schema: join branch optional `client` object {name,version strings}; 2 additive bats cases. Addendum subsection in `docs/specs/pact-protocol.md` (advisory, self-reported, not identity proof).
- [ ] Gate: full go + bats (existing untouched green; interop green). Commit: `feat(pact): optional join client metadata (provenance) — CLI + MCP clientInfo, schema + addendum`

### Task O2: serve registry dynamics + endpoints

- [ ] `Server.AddProject(p registry.Project) error` / `RemoveProject(name string) error`: map+watcher under a new mutex (watcher start/stop — read watch.go's StartWatchers to extract a single-project start; stop via the watcher's existing close path — investigate and mirror; if watchers are one fsnotify per project keep handles in a map).
- [ ] Endpoints per spec §2: GET /api/registry (status fold: pact.At(dir).Validate() error string, seat count + lastEventTs from the log — reuse ProjectState), POST (validations in order: abs path / exists / `git rev-parse --git-dir` ok / has `.pact` dir → registry.Load/Add/Save → AddProject → 200 {name}; duplicate name 409; validation failures 400 with reason), DELETE (registry Remove/Save + RemoveProject; 404 unknown).
- [ ] TDD httptest (mirror author_test fixtures): invalid path cases, happy register makes the project visible in GET /api/projects AND its SSE watcher live (append to log → hub event received), remove → 404 afterwards + watcher stopped (append → no event).
- [ ] Gate + commit: `feat(serve): registry endpoints — runtime register/remove with live watchers`

### Task O3: wiring probes + endpoints

- [ ] Extract doctor's content-aware probe into `internal/agent/probe.go`: `func ProbeWiring(dir string) []WiringStatus` — per registry kind: {Kind, Wired bool, Detail, Global bool, DocOnly bool} (JSON configs contain `"pact"` key via the kind's ConfigTarget path — ExpandPath for global; entry files contain `pact:begin`; wired = config-wired OR entry-baked — report which in Detail). Doctor's checkAgentWiring refactors onto it (doctor tests stay green).
- [ ] Endpoints: GET wiring (ProbeWiring), POST wiring/{kind} {seat, roles} → validate slug+roles non-empty → `agent.Wire(kind, seat, roles, dir)` → response {wrote, global, path} from the kind's ConfigTarget (codex kinds: skip Wire? NO — Wire bakes the entry file for codex too; response adds docOnly:true + snippet via agent.Render). No acting-seat requirement (setup-class operation).
- [ ] TDD: probes on wired/unwired fixtures (incl. a global-kind probe against a temp HOME); POST project kind writes opencode.json; POST desktop kind with temp HOME → file under fake ~/Library + response global:true; codex → docOnly + snippet contains `[mcp_servers.pact]`.
- [ ] Gate + commit: `feat(serve): wiring status probes + wire endpoint (desktop flagged global, codex doc-only)`

### Task O4: seats provenance endpoint

- [ ] Fold from raw log events (`event.ReadAll`): per roster seat → joins ordered by ts → lastJoin {client,version,ts} (client may be absent → omit), clientChanged = last two joins' client.name differ (both present). Endpoint GET /api/projects/{id}/seats per spec.
- [ ] TDD: no joins; one CLI join (pactify-cli); two joins different names → clientChanged true; same names → false.
- [ ] Gate + commit: `feat(serve): seats endpoint — roster + join provenance + change warnings`

### Task O5: web ops view

- [ ] api.ts: getRegistry/postRegister/deleteRegistry/getWiring/postWire/getSeats (error-verbatim pattern). types.ts additions.
- [ ] `components/ops/{OpsView,Projects,Wiring,Seats}.tsx` per spec §3; TopBar 3-way switch (kanban|canvas|ops); App routes the view + refreshes the project switcher after register/remove (re-fetch projects list). Observe-only: mutations hidden, Seats visible.
- [ ] Desktop-kind wire modal: red machine-global line + checkbox gate on Confirm. Codex row: copyable snippet block.
- [ ] vitest: provenance warning pure derivation; Projects register error path renders server message; Wiring rows render probe fixtures (global/docOnly variants); component smokes. Keep all existing green.
- [ ] Gate (`npm test`, tsc, build+commit dist) + commit: `feat(web): ops view — projects registry, agent wiring, seat provenance`

### Task O6: e2e + docs + PR

- [ ] `tests/serve_ops.bats`: boot serve on a scratch repo dir (NOT pre-registered) → POST register → GET wiring shows unwired → CLI join as worker → GET seats shows pactify-cli client. 1-2 @test blocks.
- [ ] docs/architecture.md ops paragraph; .agent records.
- [ ] Full baseline (go+race serve, bats all, web), PR `Squad M3.3a: ops panel — registry, wiring, seat provenance`, CI green → merge (authorized), pull main, rebuild local binary.

## Self-Review Notes
- Spec §1→O1, §2→O2-O4, §3→O5, §4 distributed, §5 respected. Endpoint/UI contracts normative in spec — plan doesn't restate payload shapes to avoid drift.
- Known seams called out: watcher single-project start/stop needs investigation in O2 (watch.go structure); pact.ClientVersion plumbing in O1; HOME-faking for global probes in O3 tests (os.UserHomeDir honors $HOME on darwin — established in M2.2 tests).
- Risk: O2 watcher lifecycle (stop semantics) — gate is the live/stopped SSE assertions.

# M3.3a — Squad Ops Panel (registry + wiring + seat provenance) — Design

- **Date:** 2026-06-11 · **Status:** approved (brainstorm)
- **Origin:** user acceptance-testing findings on M3.1/M3.2 — "repo registration and agent
  wiring must be visual" + "I need to be sure the assigned seat is correct".
- **Depends on:** Squad M3.1+M3.2 merged (author API, canvas, dir-aware engine, deps pattern).

## Decisions locked in brainstorm

1. **Registry: full management in the UI** — register (server-validated absolute path),
   remove (registry-only, never touches files, confirmed), per-project status. serve adds
   projects/watchers at RUNTIME (no restart).
2. **Wiring: ALL kinds including desktop** — project-scoped kinds write in-repo configs;
   desktop kinds (claude-desktop/antigravity) write machine-global files THROUGH serve
   (same localhost single-user trust as the CLI), gated by an explicit UI confirmation and
   a prominent machine-global label; codex stays doc-only (snippet shown).
3. **Provenance: optional `client` field on the join event** (additive to protocol v1,
   same pattern as `deps`): `client: {name, version}` — MCP path fills it from the
   session's initialize `clientInfo` (verified reachable: `req.Session.InitializeParams()`),
   CLI path fills `pactify-cli/<version>`. Rendered/serialized only when present;
   deps-free…client-free logs stay byte-identical (interop untouched); bash reference not
   extended; protocol addendum extended.
4. **Provenance semantics v1: display + soft warning** — seats panel shows each seat's
   most recent join client+time; consecutive joins of the SAME seat by DIFFERENT client
   names raise an informational warning flag (yellow dot, hover shows before→after).
   No enforcement/binding (Phase 4 RBAC territory).

## §1 Protocol surface (minimal)

- `join` payload optional `"client": {"name": string, "version": string}`.
- Schema: optional typed property on the join branch; valid+invalid bats cases (additive).
- Projection: `Task`-style conditional — seat/agents rendering gains nothing in STATE
  (provenance is NOT part of STATE.yml — it stays in the log only, read by serve when
  folding seats; this keeps STATE byte-parity trivially intact for ALL logs).
- `docs/specs/pact-protocol.md` addendum gains a `client` subsection (advisory metadata,
  self-reported, not an identity proof — wording matters).

## §2 Engine + serve

- `(p *Project) Join(seatID, roles string)` gains optional client plumbing: new method or
  variadic — concrete shape: `JoinWithClient(seatID, roles, clientName, clientVersion string)`;
  existing `Join` wraps it with the CLI identity (`pactify-cli`, injected version). MCP
  join tool handler calls JoinWithClient with the session's clientInfo.
- **Registry dynamics**: `Server.AddProject(p registry.Project) error` (duplicate-name
  guard, watcher start, map insert under a lock) and `RemoveProject(name)` (watcher stop,
  map delete). Registry file (~/.pactify/projects.json) updated via the existing registry
  package (Load/Add/Remove/Save) so CLI and serve stay consistent.
- New endpoints (author conventions: errors `{"error"}` verbatim, 404 unknown project):
  ```
  GET    /api/registry                          → [{name, path, status:{valid, error?, seats, lastEventTs}}]
  POST   /api/registry      {path, name?}       → validate: absolute path, exists, is a git repo,
                                                   has .pact/ (NO implicit init — register only) → add + watch → 200 {name}
  DELETE /api/registry/{name}                   → remove from registry + stop watcher (files untouched)
  GET    /api/projects/{id}/wiring              → [{kind, wired, detail, global, docOnly}] (content-aware probes)
  POST   /api/projects/{id}/wiring/{kind} {seat, roles} → agent.Wire (acting-seat NOT required — wiring
                                                   is setup, not a protocol verb; slug-validate seat) →
                                                   {wrote, global, path} | codex → {docOnly, snippet}
  GET    /api/projects/{id}/seats               → [{id, roles, lastJoin:{client, version, ts}?, clientChanged}]
  ```
- Wiring probes: extract doctor's content-aware `checkAgentWiring` logic into a reusable
  helper (per-kind: JSON configs contain `"pact"`, entry files contain `pact:begin`) —
  doctor and serve share it (one source of truth).
- Seats provenance fold: scan the log's join events per seat; `lastJoin` = most recent;
  `clientChanged` = the two most recent joins exist and have different client names.

## §3 UI (web/)

- TopBar view switch becomes `kanban | canvas | ops`.
- **Ops view**, three stacked panels:
  1. **Projects** — registry cards (status badge valid✓/error✗ + seat count + last
     activity), Register form (absolute path input + optional name; server errors shown
     verbatim), Remove with confirm. Registering/removing updates the project switcher live.
  2. **Wiring** (for the selected project) — one row per kind: status dot (content-aware),
     Wire button → modal (seat id + roles inputs, slug-validated client-side); desktop
     kinds show a red "writes a machine-global file: <path>" line + an explicit checkbox
     before Confirm; codex rows show a copyable snippet instead of a button.
  3. **Seats** — roster table: seat id, roles (role-colored chips), last join client +
     version + relative time, warning dot when clientChanged (hover: "client changed:
     opencode → claude-code").
- Observe-only (no acting seat): Projects/Wiring mutations hidden, Seats visible
  (read-only is the point of provenance).

## §4 Testing

- Engine: client plumbing (log carries client only when provided; CLI wrapper fills
  pactify-cli; render/STATE unchanged for all logs — interop suite untouched).
- Schema: join+client valid / client-not-object invalid (additive bats).
- serve: registry CRUD httptest (invalid path 400 with reason; duplicate name 409;
  register → project appears in /api/projects AND SSE watcher live (touch log → event);
  remove → gone, watcher stopped); wiring GET probes (wired/unwired fixtures), POST
  (project kind writes config; desktop kind response flags global; codex docOnly+snippet);
  seats endpoint (no joins → no lastJoin; two joins different clients → clientChanged).
- web: vitest — ops panels render from fixtures; provenance warning derivation pure fn;
  register form error path. Component smokes.
- bats: one serve_ops.bats curl flow (register scratch repo → wiring probe → join via CLI
  → seats shows pactify-cli).
- MCP: join tool test asserts client name from session clientInfo lands in the log
  (in-process client fixture sets Implementation{Name:...}).

## §5 Out of scope

Seat binding/keys (Phase 4) · comms visualization (M3.3b) · multi-user/remote ·
implicit `pact init` on register · layout cleanup on remove · bash reference client support.

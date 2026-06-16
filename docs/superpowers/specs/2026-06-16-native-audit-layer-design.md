# Pactify Native Audit Layer — Design Spec

> Date: 2026-06-16 · Status: Draft for review · Author: claude (opus-4.8)
> Supersedes the "integrate AgentPact" option (too heavy: Rust 2-binary, SQLite,
> daemon, separate Web UI). This is a Pactify-native, Go, file-based audit layer.

**Goal:** Give Pactify a permission **audit log** — record every tool call each
seat makes while executing a task (Bash / file read+write / MCP), queryable by
project · seat · task · session — as a lightweight capability folded into the
single `pactify` Go binary.

**Architecture (one line):** a `pactify audit hook` subcommand installed as each
client's PreToolUse hook captures every tool call, normalizes it, and appends one
JSON line to a machine-local append-only store; the runner stamps the seat/task
into the agent's env so each record is attributable; CLI + a dashboard lens read
the store back.

**Tech stack:** Go (new `internal/audit` package + `audit` cobra subcommands),
append-only JSONL, reuse of the existing serve/dashboard. No Rust, no DB, no
daemon, no classifier, no network in the hot path.

---

## 1. Scope & priorities

- **P1 (this spec, build now): Audit log.** Capture → store → query. The hook is
  **log-only**: it records the call and always allows (emits nothing, exits 0).
- **P2 (designed-for here, NOT built): Governance + presets.** The same hook later
  returns a `permissionDecision` (allow/deny/ask) by consulting a policy file;
  presets = `~/.pactify/policy.toml` + profiles. §13 shows the seam is already in
  place so P2 is additive, not a rewrite.

### Non-goals (v1)
- No blocking / approval prompts (log-only).
- No risk classifier beyond a cheap static `risk` tag (§7).
- No cloud sync (local-first, like the rest of Pactify).
- No new protocol events — audit is **out-of-band**, never written to
  `.pact/log.jsonl` (keeps the protocol log clean and git-committable).

---

## 2. Decisions (the 5 forks, locked with rationale — open to review)

| # | Decision | Choice | Why |
|---|----------|--------|-----|
| D1 | Capture mechanism | **PreToolUse hook** (not stdout-parsing) | Complete (every Bash/Read/Write/Edit/MCP), not bypassable, works for orchestrated AND manual runs. Stdout-parsing is per-kind-fragile and orchestrate-only. |
| D2 | Store location | **`~/.pactify/audit/<project>/YYYY-MM-DD.jsonl`** | Machine-level, repo-clean, cross-project, survives repo moves, co-located with the `~/.pactify` registry. Never pollutes git. |
| D3 | v1 client scope | **claude-code + opencode** first; **codex** fast-follow | The two primary orchestrate kinds; both hook-capable (AgentPact-verified). gemini-cli has no PreToolUse hook → deferred (§11 note). |
| D4 | Risk tag in v1 | **Yes — cheap static tag** (`read`/`write`/`exec`/`net`/`mcp`) | Near-free, makes `audit summary` immediately useful; not a classifier. |
| D5 | Install UX | **Standalone `pactify audit install --<client>/--detect`** + optional call from `setup` | Mirrors a known pattern, reuses wiring, explicit and idempotent. |

---

## 3. Data flow

```
orchestrate runner (or a human running claude/opencode directly)
   └─ launches agent with env: PACT_AGENT_ID=<seat> PACT_TASK_ID=<task> PACT_PROJECT=<id>
        └─ agent makes a tool call (Bash/Read/Write/Edit/MCP)
             └─ client fires PreToolUse hook:  pactify audit hook --kind <kind>
                  ├─ reads stdin JSON (tool_name, tool_input, session_id, cwd)
                  ├─ reads env (PACT_AGENT_ID / PACT_TASK_ID / PACT_PROJECT)
                  ├─ normalizes → audit.Record
                  ├─ audit.Append(record)  → ~/.pactify/audit/<project>/<date>.jsonl
                  └─ emits nothing, exits 0  (log-only = allow)

read side:
   pactify audit log/summary  ──┐
   GET /api/projects/{id}/audit ─┴─► audit.Query(filter) folds the JSONL
   dashboard "Audit" lens ──────────► renders per seat/task
```

---

## 4. Interception: PreToolUse hook contract

### 4.1 stdin (what the client sends the hook), per the Claude Code hook protocol
```jsonc
{ "tool_name": "Bash",
  "tool_input": { "command": "git push origin main" },   // or {file_path}/{path}
  "session_id": "uuid",
  "cwd": "/abs/project/repo" }
```

### 4.2 Tool normalization (per-kind `map_tool`)
Normalize each client's tool vocabulary to a small canonical set. Grounded in
AgentPact's verified mapping for claude-code:

| client tool | canonical `tool` | `summary` source | `risk` |
|---|---|---|---|
| `Bash` | `bash` | `tool_input.command` | `exec` |
| `Write`/`Edit`/`MultiEdit`/`NotebookEdit` | `fs.write` | `tool_input.file_path` | `write` |
| `Read` | `fs.read` | `tool_input.file_path` | `read` |
| `mcp__*` (any MCP tool) | the full `mcp__…` name | compact args | `mcp` |
| (unmapped) | record raw `tool_name` | best-effort | `other` |

- **claude-code** uses `file_path`; other clients differ (`path`, etc.) → the
  per-kind branch owns the field name. **opencode + codex exact field names and
  the PreToolUse stdin shape are a §16 verification item** (实测 before wiring).
- A leading `KEY=value env` prefix on a Bash command is skipped when deriving the
  executable (for the future exec-risk/governance; harmless in v1).

### 4.3 stdout
- **v1 (log-only):** emit nothing, `exit 0` → non-blocking, the call proceeds.
- **P2 (governance):** emit
  `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow|deny|ask","permissionDecisionReason":"…"}}`.
  The v1 hook already runs at this exact point — P2 only adds the decision return.

### 4.4 Failure posture
The hook must **never block the agent on its own failure**. Any error (bad stdin,
unwritable store, unknown kind) → log to stderr, `exit 0`. Audit is best-effort;
a broken audit must not break the user's agent. (Mirrors the relay's best-effort
stance in architecture.md.)

---

## 5. Audit record schema (`audit.Record`)

```jsonc
{
  "ts":      "2026-06-16T05:10:00.123Z", // RFC3339 UTC
  "project": "myrepo",      // PACT_PROJECT, else basename(cwd)
  "repo":    "/abs/path",   // cwd from stdin
  "seat":    "dev",         // PACT_AGENT_ID ("" if manual/unknown)
  "task":    "t3",          // PACT_TASK_ID ("" if not in an orchestrated stint)
  "kind":    "claude-code", // which client fired the hook (from --kind flag)
  "session": "uuid",        // stdin.session_id ("" if absent)
  "tool":    "bash",        // canonical (§4.2)
  "summary": "git push origin main", // SHORT, redacted (§8.2); truncated to N chars
  "risk":    "exec",        // read|write|exec|net|mcp|other (static §4.2)
  "decision":"allow"        // v1 always "allow"; P2 may be deny/ask
}
```
- Forward-compatible: readers ignore unknown fields (same discipline as pact events).
- `decision` present from v1 so the schema never changes when P2 lands.

---

## 6. Store: `~/.pactify/audit/<project>/YYYY-MM-DD.jsonl`

- **Append-only JSONL**, one file per project per UTC day (natural rotation;
  cheap to query a day/range; trivially prunable).
- Path resolution mirrors `internal/registry` (`~/.pactify`); honor `PACTIFY_HOME`
  for tests (the registry/serve already do).
- **Concurrency:** the hook is a short-lived process; multiple may append
  concurrently (parallel orchestrate / a busy agent). Append with `O_APPEND` +
  one `write` of a single `\n`-terminated line (atomic for small writes on local
  FS) — no lock file. A torn line is tolerated by the reader (skip-on-parse-error).
- **Retention:** v1 keeps everything; `pactify audit prune --older-than 30d`
  deletes old day-files. (No silent cap — prune is explicit.)

---

## 7. Correlation (env stamping)

`internal/orchestrate/runner.go` already injects `PACT_AGENT_ID=<seat>`. Add two
more when launching a stint (both the owner and reviewer paths already know the
task and project):

```go
env := []string{
    "PACT_AGENT_ID=" + seatID,
    "PACT_TASK_ID="  + taskID,   // NEW
    "PACT_PROJECT="  + projectID,// NEW (or omit → hook falls back to basename(cwd))
}
```
- `runner.Run` currently takes `(seatID, kind, briefing, repoDir)`; it has no task
  id. **Decided: replace the loose params with a `LaunchContext` struct** —
  `Run(ctx, LaunchContext{Seat, Kind, Task, Project, Briefing, RepoDir}) error`.
  Blast radius (measured): the `Runner` interface, `CmdRunner.Run`, the single
  `launchAgent` helper + its two callers (runOwner/runReviewer already hold the
  task), and 3 test fakes (`fakeRunner`/`crashRunner`/`parFakeRunner`); the
  parallel path reuses `launchAgent` (no extra site). ~7 mechanical edits, no
  protocol/behavior change beyond the new env vars (inert when no hook installed).
  The struct (vs more positional params) avoids future signature churn when audit
  correlation grows.
- Manual runs (human invokes claude directly): env absent → `seat`/`task` empty,
  still recorded and correlated by `repo` + `session`.

---

## 8. Components & interfaces

### 8.1 `internal/audit` (new)
```go
package audit

type Record struct {
    TS, Project, Repo, Seat, Task, Kind, Session, Tool, Summary, Risk, Decision string
}

// Append writes one record to ~/.pactify/audit/<project>/<utcDate>.jsonl (O_APPEND).
// Best-effort: returns an error for the caller to log, never panics.
func Append(r Record) error

// Filter selects records on read.
type Filter struct {
    Project, Seat, Task, Session string // "" = any
    Since, Until time.Time              // zero = unbounded
    Risk string                          // "" = any
}

// Query folds the relevant day-files, newest-first, skipping unparseable lines.
func Query(f Filter) ([]Record, error)

// Summary aggregates counts by tool/risk/seat for a digest.
func Summarize(rs []Record) Summary

// FromHook parses a client's PreToolUse stdin JSON for `kind` into a Record,
// stamping seat/task/project from env. ok=false when the tool is unmapped
// (caller then no-ops, exit 0). Pure (env + bytes in, Record out) for testability.
func FromHook(kind string, stdin []byte, env Env, now time.Time) (Record, bool)
```

### 8.2 Redaction (privacy)
`summary` is a **short, redacted** rendering — never full file contents, never the
raw tool payload:
- `bash` → the command string, truncated to ~200 chars.
- `fs.read`/`fs.write` → the path only (no contents).
- `mcp__*` → tool name + key arg names (not values) when values look secret-ish.
- A small redactor masks `KEY=…`/`token`/`secret`/`Bearer …`/long base64-ish runs.
- Truncation cap is a const; `audit log --full` is **not** offered in v1 (the
  store deliberately never holds the untruncated payload).

### 8.3 `cmd/pactify` — `audit` command group
```
pactify audit hook --kind <kind>     # the PreToolUse entry (reads stdin, appends, exit 0)
pactify audit log [--project P] [--seat S] [--task T] [--session ID]
                  [--since 24h] [--risk exec] [--json] [--limit N]
pactify audit summary [--project P] [--since 24h]   # counts by tool/risk/seat
pactify audit prune --older-than 30d
pactify audit install --detect                       # show clients + hook status
pactify audit install --claude-code [--global]       # default: project ./.claude/settings.json
pactify audit install --opencode
pactify audit uninstall --<client>
```

### 8.4 serve + dashboard
- `GET /api/projects/{id}/audit?seat=&task=&since=` → `audit.Query` as JSON.
- Dashboard **"Audit" lens** (sits beside the D1 "Cost" lens): per seat/task, the
  tool-call timeline + counts (n bash / n write / n read / n mcp), risk-colored.
  Read-only. (Can ship after the CLI; not on the v1 critical path.)

---

## 9. Install / wiring

- **Default = project-scoped** `./.claude/settings.json` (the pact project's repo),
  so the audit hook is on exactly for repos Pactify manages; `--global` writes
  `~/.claude/settings.json`. Claude shape (AgentPact-verified):
  ```jsonc
  { "hooks": { "PreToolUse": [
      { "matcher": "*", "hooks": [ { "type":"command", "command":"pactify audit hook --kind claude-code" } ] }
  ] } }
  ```
- **Idempotent upsert**: remove any prior Pactify audit entry before inserting
  (re-runnable; never doubles). Mirror `pact.BakeManagedBlock`'s managed-region
  discipline.
- **opencode / codex**: their PreToolUse hook config locations + JSON shapes are a
  §16 verification item; the `audit install --opencode/--codex` branch is written
  only after `--help`/docs 实测.
- **Conflict guard**: `audit install --detect` warns if AgentPact (or any other
  PreToolUse hook) is already registered, to avoid double-governance/double-logging.

---

## 10. P2 extension points (governance + presets) — not built, but seam-ready

- **Governance:** `pactify audit hook` already sits at the decision point and reads
  the same inputs. P2 adds: load `~/.pactify/policy.toml` (or project
  `.pact/policy.toml`) → evaluate `deny > allow > ask > default` → emit the
  `permissionDecision` JSON (§4.3). The v1 record already carries `decision`.
- **Presets:** `policy.toml` per-kind/per-seat allow/deny/ask + named profiles
  (`developer`/`restricted`), selectable via `pactify audit profile use …`. The
  store/record/CLI/dashboard built in v1 are unchanged; P2 only adds the policy
  read + decision emit + profile management.

---

## 11. Coverage matrix (v1)

| kind | drivable by orchestrate | PreToolUse hook | v1 audit |
|---|---|---|---|
| claude-code | ✓ | ✓ (verified) | ✅ |
| opencode | ✓ | ✓ (verify shape) | ✅ |
| codex-cli | (runner pending) | ✓ (verify shape) | fast-follow |
| gemini-cli | ✓ | ✗ (no PreToolUse) | ❌ deferred — needs MCP-proxy or json-parse, out of scope |
| GUI/desktop kinds | ✗ | ✗ | n/a |

gemini gap is explicit: no silent claim of coverage it doesn't have.

---

## 12. Testing plan

- **`audit.FromHook` (table tests):** per-kind stdin fixtures (claude Bash/Read/
  Write/MultiEdit/mcp__*) → expected canonical Record incl. risk; unmapped → ok=false;
  env stamping (PACT_* present/absent); redaction (secret-ish summaries masked,
  truncation cap). Pure function — no process spawn.
- **`audit.Append`/`Query` (temp `PACTIFY_HOME`):** round-trip; day-file rotation;
  torn-line skip; filters (seat/task/session/since/risk); newest-first order.
- **CLI:** `audit hook` reads a fixture on stdin → asserts the appended line + exit 0;
  `audit hook` with a broken store still exits 0 (never blocks the agent);
  `audit log/summary` formatting; `audit install --claude-code --project` writes the
  idempotent settings block (re-run = no dup).
- **runner:** asserts `PACT_TASK_ID`/`PACT_PROJECT` are injected alongside
  `PACT_AGENT_ID` (extend the existing env-capture runner test).
- **serve:** `GET …/audit` shape + filters (hermetic, injected store).
- No real LLM/agent needed for any test (the hook is a pure stdin→file step).

## 13. Verification items (实测 before wiring — gates per CLAUDE.md "禁止凭文档断言")

1. **opencode** PreToolUse hook: does opencode emit PreToolUse hooks? config
   location + stdin JSON shape + tool field names. (`opencode --help`, settings.)
2. **codex** ditto (AgentPact has a `--codex` branch to cross-check).
3. claude-code **project-scoped** `./.claude/settings.json` PreToolUse fires for a
   `pactify orchestrate`-launched `claude -p` (env propagation through the hook).
4. Confirm the hook process inherits the launched agent's env (so PACT_* reach it)
   — true for child-of-agent hooks; verify per client.

## 14. Rollout / phasing

- **A — core (no client wiring):** `internal/audit` (Record/Append/Query/Summarize/
  FromHook + redaction) + `audit hook/log/summary/prune` CLI + runner env stamping
  + tests. Deliverable: a working store + a manual `echo <json> | pactify audit hook`
  proof. No settings touched.
- **B — claude-code wiring:** `audit install --claude-code` (project-scoped),
  idempotent upsert, `--detect`. 实测 #3/#4. End-to-end on a real orchestrate stint.
- **C — opencode wiring:** 实测 #1, add the branch. (codex after its runner lands.)
- **D — dashboard Audit lens:** serve endpoint + UI lens beside Cost.

Each phase ships independently green (go test; web gate for D).

---

## Resolved decisions (review closed 2026-06-16)
- **D2 store location:** `~/.pactify/audit/<project>/YYYY-MM-DD.jsonl` (machine-
  level). Audit is a **first-class product feature**, not an incidental log file.
- **D3 v1 client scope:** **claude-code + opencode together** (both wired in v1).
  codex fast-follow once its runner lands; gemini-cli deferred (no PreToolUse).
- **§7 runner seam:** **`LaunchContext` struct** (see §7).
- **Dashboard lens:** **in v1** — built as the last phase (D) so A/B/C ship first,
  but it IS part of the v1 deliverable (the product surface for the audit log),
  not a later follow-up.

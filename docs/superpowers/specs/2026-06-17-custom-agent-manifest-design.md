# Custom-Agent Manifest API — Design Spec

> Date: 2026-06-17 · Status: Draft for review · Author: claude (opus-4.8)

**Goal:** Let a user plug a new/niche agent into Pactify **without editing Go
source**, by declaring the per-kind knowledge (binary, headless argv, MCP wiring,
entry file, models, permission posture) in a **TOML manifest** that Pactify loads
at startup and merges into the built-in agent registry.

**Architecture (one line):** machine-level `~/.pactify/agents/*.toml` manifests are
parsed → validated → mapped onto the same internal `spec` + `RunnerProfile`
structures the built-in kinds use → merged into the registry (add-only, never
overriding a built-in), so every existing consumer (orchestrate runner, agentcfg,
scan/roster, Settings model dropdown, MCP wiring) works unchanged.

**Tech Stack:** Go + a TOML parser (`github.com/pelletier/go-toml/v2`, new direct
dep — the project is otherwise dependency-light, STATE.yml is hand-rolled). New
`internal/agentmanifest` package; serve endpoint + a web "Add custom agent" form.

---

## 1. Locked decisions (review closed 2026-06-17)

| # | Decision |
|---|----------|
| Format | **TOML** (no `{placeholder}` brace footgun like YAML's flow-mapping; strong typing; config-native; matches codex `config.toml`). |
| argv | **placeholder template** `{briefing}` `{model}` `{permission}` `{tools}` `{seat}` (see §3). |
| Override | **Add-only** — a manifest whose `kind` collides with a built-in is **rejected** (built-ins are the verified source of truth; they carry special logic like GLM/gemini-key). |
| Location | **Machine-level only**: `~/.pactify/agents/*.toml` (honors `PACTIFY_HOME`). No project-level manifests in v1. |
| Settings UI | **Included**: an Ops "Add custom agent" form generates+validates a manifest. |

---

## 2. Manifest schema (full)

`~/.pactify/agents/<kind>.toml`:

```toml
kind   = "myagent"          # unique id; MUST NOT collide with a built-in kind
binary = "myagent"          # LookPath target (install detection) + runner command
entry  = "AGENTS.md"        # baked pact-block entry file; "" = none (desktop-style)

[identity]                  # how the launched agent learns WHICH seat it is
via = "env"                 # "env" (default: PACT_AGENT_ID, child inherits) | "arg"
                            # via="arg" → put {seat} in [runner].args

[mcp]                       # how the pact MCP server is wired into this agent
config_path = ".myagent/mcp.json"   # repo-relative (project) or ~/abs (global)
scope       = "project"     # "project" | "global"
format      = "mcpServers"  # "mcpServers" | "opencode" | "toml" | "none"

[runner]                    # how orchestrate launches it headlessly. Omit the whole
                            # [runner] table → kind is MANUAL (no headless driver),
                            # like a GUI/desktop seat.
args          = ["run", "-m", "{model}", "{permission}", "{briefing}"]
default_model = "myagent-pro"        # "" = no -m pin (drop {model} when empty)
models        = ["myagent-pro", "myagent-mini"]   # Settings dropdown candidates

[runner.permission]         # what {permission} expands to per posture
blanket = ["--yolo"]                          # auto-approve (default posture)
scoped  = ["--allowed-tools", "{tools}"]      # allowlist; {tools}=comma-joined
```

**Field notes**
- `kind`/`binary` required. `entry` optional (""). `[identity]` optional (default env).
- `[mcp]` optional: omit → no MCP wiring (the agent uses the entry file + CLI fallback only). `format = "none"` is the same as omitting.
- `[runner]` optional: omit → kind is registered but **not drivable** (manual seat, surfaced in scan as "manual").
- `[runner.permission]` optional: omit → posture is ignored (the agent always runs as-is; `{permission}` expands to nothing), matching kinds like opencode/kimi that have no allowlist flag.

---

## 3. argv template rendering

The manifest's `[runner].args` is rendered into the real argv by substituting
placeholders. This replaces the per-kind `BuildArgs` Go closures with one
data-driven renderer. Given `(model, posture, briefing, seat)`:

| placeholder | expands to | notes |
|---|---|---|
| `{briefing}` | the prompt text | exactly one arg |
| `{model}` | `model` | if `model == ""` (no pin), the `{model}` element AND an immediately-preceding lone `-m`/`--model` flag are dropped (so codex-style "omit -m when unset" works) |
| `{seat}` | the seat id | only when `identity.via = "arg"` |
| `{permission}` | the `blanket` **or** `scoped` arg list (a slice, spliced in place) | chosen by posture; each sub-arg may itself contain `{tools}` |
| `{tools}` | comma-joined allowed-tools | only meaningful inside a `scoped` fragment |

Rendering is a pure function `RenderArgs(tmpl []string, perm PermPosture, model,
briefing, seat string) []string`. It produces the same `[]string` the built-in
`RunnerProfile.BuildArgs` produces, so the manifest path reuses the existing
`RunnerProfile` type:

```go
RunnerProfile{
    Command:      m.Binary,
    DefaultModel: m.Runner.DefaultModel,
    Models:       m.Runner.Models,
    BuildArgs: func(model string, perm PermPosture, briefing string) []string {
        return RenderArgs(m.Runner.Args, perm, model, briefing, seatPlaceholderValue)
    },
}
```

> `{seat}` is known per-launch, not at profile-build time. To keep the existing
> `BuildArgs(model, perm, briefing)` signature, the renderer treats `{seat}` as a
> literal token left in the args, and the orchestrate runner substitutes it with
> `lc.Seat` at exec time (the runner already post-processes args, e.g. opencode
> `--title`). Built-in kinds never emit `{seat}`, so this is manifest-only.

---

## 4. Loading & merge

New `internal/agentmanifest`:
- `Load() ([]Manifest, error)` — read every `~/.pactify/agents/*.toml`, parse
  (strict: unknown keys error), and **validate** (§6). One bad manifest is
  reported and skipped (logged), never crashing the load.
- `Manifest` → two derived values: an `agent.Spec`-equivalent (kind/entry/mcp
  config/scope/format/detectBin) and a `RunnerProfile` (when `[runner]` present).

`internal/agent` gains a registration seam so the built-in `registry` map and
`runnerProfiles` map can be augmented at process start:
- `agent.RegisterExternal(specs map[string]spec, profiles map[string]RunnerProfile)`
  — called once from `main`/serve init with the manifest-derived entries.
- **Add-only guard:** if a manifest kind already exists in the built-in registry,
  `RegisterExternal` rejects it (returns an error naming the collision); the
  manifest is skipped with a warning. Built-ins always win.
- `Get`/`Kinds`/`RunnerProfileFor`/`CandidateModels`/`Drivable`/`Scan` then see the
  merged set with zero changes to their callers (orchestrate, agentcfg, serve).

`agent.Format`/`Scope` get string parsers: `"mcpServers"→JSONMcpServers`,
`"opencode"→JSONOpencode`, `"toml"→TOML`, `"none"→(no MCP)`; `"project"→Project`,
`"global"→Global`.

---

## 5. CLI + serve + UI

**CLI** (`pactify agent manifest …`):
```
pactify agent manifest list                 # built-in + user kinds, source-tagged
pactify agent manifest validate <file>      # parse + validate, print errors/OK
pactify agent manifest add <file>           # validate → copy into ~/.pactify/agents/
pactify agent manifest remove <kind>        # delete the user manifest (built-ins refuse)
pactify agent manifest show <kind>          # effective manifest (incl. built-ins, read-only)
```

**Serve**:
- `GET  /api/manifests` → list user manifests + validity.
- `POST /api/manifests` (author-gated) → body = manifest fields (or raw
  TOML); validate → write `~/.pactify/agents/<kind>.toml`; 422 with field errors on
  invalid; refuses built-in collisions.
- `DELETE /api/manifests/{kind}` (author-gated).

**Settings UI** (Ops, beside AgentRoster): an **"Add custom agent"** form —
kind/binary/entry, the runner argv (with a live placeholder hint), default+models,
MCP path/scope/format, permission fragments. On submit → POST above → the kind
appears in the roster (`getAgents` re-scans) and is registerable. Read-only "source:
custom" badge distinguishes user kinds from built-ins.

---

## 6. Validation rules

A manifest is valid iff:
1. `kind` non-empty, `[a-z0-9-]+`, **not a built-in kind** (add-only).
2. `binary` non-empty. (drivable iff `binary` is on PATH at scan time — same as built-ins.)
3. `entry` is "" or a filename (no path traversal: no `/`, no `..`).
4. If `[mcp]`: `format ∈ {mcpServers,opencode,toml,none}`, `scope ∈ {project,global}`,
   `config_path` present and not absolute unless scope=global (then must start `~/` or `/`).
5. If `[runner]`: `args` non-empty and contains exactly one `{briefing}`. If
   `identity.via="arg"`, `args` must contain `{seat}`. `{tools}` only inside a
   `[runner.permission].scoped` fragment.
6. `[runner.permission]` optional; if present, at least one of blanket/scoped.

`validate` returns the **list** of violations (not just the first), for the UI.

---

## 7. Security

A manifest declares a local binary + argv to execute — identical trust to a user
manually configuring an agent today (the user authorizes their own machine's
tools). Guards: no remote fetch; manifests are local files the user writes;
`binary` must resolve on PATH to be drivable; path fields reject traversal; the
add-only rule prevents shadowing a verified built-in (e.g. a malicious
`claude-code` manifest can't hijack the real one).

---

## 8. Scope

**v1 (this spec):** manifest schema + loader/merge (add-only) + argv renderer +
the format/scope parsers + `agent manifest` CLI + serve endpoints + the Settings
"Add custom agent" form. Enough to plug a custom agent into orchestrate + Settings.

**P2 (designed-for, not built):**
- `[session]` block (list/delete commands) → feeds `internal/sessions` for cleanup.
- `[audit]` block (hook mechanism: command vs plugin) → feeds `internal/audit` install.
- Project-level manifests (`.pact/agents/`) for team-shared custom kinds.
These are advanced; a kind works (drives, configures, audits via claude-style) without them.

**Non-goals:** a fully scriptable/Turing-complete manifest; remote manifest registries.

---

## 9. Internal mapping (what each manifest field drives)

| manifest | internal target | consumer |
|---|---|---|
| kind, entry, binary | `agent.spec{kind,entry,detectBin}` | scan/detect, onboarding |
| `[mcp]` path/scope/format | `agent.spec{cfgPath,scope,format}` + `ConfigTarget` | `agent.Wire`, MCP wiring, doctor |
| `[runner].args/default_model/models` | `agent.RunnerProfile{Command,DefaultModel,Models,BuildArgs}` | orchestrate runner, agentcfg.Resolve, `CandidateModels` (Settings dropdown), `Drivable` |
| `[runner.permission]` | `RenderArgs` `{permission}` expansion | per-run PermPosture (#4 scoped) |
| `[identity]` via | `{seat}` substitution at exec (runner) | seat identity for arg-style agents |

---

## 10. Testing plan

- **agentmanifest parse/validate** (table tests): valid manifest → expected
  Spec+RunnerProfile; each violation in §6 → reported; unknown TOML key → error;
  built-in-collision → rejected. Temp `PACTIFY_HOME`, no real binaries.
- **RenderArgs** (table tests): `{briefing}/{model}/{permission}/{tools}/{seat}`
  substitution; empty model drops `{model}` + preceding `-m`; blanket vs scoped;
  no-permission table → `{permission}` vanishes. Pure function.
- **merge/add-only**: `RegisterExternal` augments registry; collision with a
  built-in is refused; merged `Kinds()/RunnerProfileFor()/CandidateModels()` see
  the new kind.
- **CLI**: `manifest validate` on a fixture (OK + error cases); `add` writes the
  file; `remove` deletes user but refuses built-in.
- **serve**: POST valid → file written + 200; POST invalid → 422 with field errors;
  POST built-in kind → refused.
- **web**: the Add-custom-agent form posts and surfaces field errors (vitest).
- End-to-end smoke: a real custom manifest for an installed CLI (e.g. wrap `cat`
  or a trivial script as a fake "agent") → `agent manifest add` → it shows in
  `agent scan` as drivable → a `RenderArgs` dry-run matches expectation. (No LLM.)

## 11. Verification items (实测 before relying on a real custom agent)

1. Confirm `RegisterExternal` runs in BOTH entry points that build the registry —
   the CLI (`main`) and `pactify serve` — so manifests load consistently.
2. A manifest-driven kind flows through `agentcfg.Resolve` (model/posture override)
   identically to a built-in (the runner path is shared).
3. The Settings "Add custom agent" → roster → register → setup → orchestrate chain
   works for one real installed CLI wrapped as a custom kind.

## 12. Rollout phases

- **A — core (no UI):** `internal/agentmanifest` (Manifest, Load, validate),
  `RenderArgs`, `agent.RegisterExternal` + add-only guard + format/scope parsers,
  wire `RegisterExternal` into `main` + serve init. `agent manifest validate/list/
  show`. Tests. Deliverable: hand-write a TOML → `agent scan` shows the kind drivable.
- **B — CLI add/remove:** `agent manifest add/remove`.
- **C — serve endpoints:** GET/POST/DELETE manifests.
- **D — Settings UI:** the Add-custom-agent form + source badge; vitest + e2e green.

Each phase ships green independently (go test; web gate for D).

---

## Resolved decisions (review closed 2026-06-17)
- **TOML parser**: `github.com/pelletier/go-toml/v2` (strict decode via
  `toml.Decoder.DisallowUnknownFields`, actively maintained).
- **`{model}`-empty handling**: the drop rule (§3, option A) — the author writes
  `-m {model}` (or `--model {model}`) naturally; when the effective model is empty
  (Settings "default" / a kind with no `default_model`), the renderer drops the
  `{model}` token **and** an immediately-preceding lone `-m`/`--model` flag. This
  keeps one template working for both a pinned model and a cleared/default model,
  matching the built-in behavior (e.g. codex omits `-m` when unset) and the
  existing Settings model-dropdown "default" option. RenderArgs §10 tests pin the
  exact rule (only a directly-preceding `-m`/`--model`, not an arbitrary earlier
  flag, is dropped).

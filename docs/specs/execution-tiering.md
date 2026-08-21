# Execution Tiering — bounded budgets + failure-driven model routing

Status: SPEC (not implemented)
Author: claude (orchestrator seat), 2026-08-21
Related: `review-runtime-deepening.md` (fix-until-green / critic / QA), `driver-modernization.md` (transports, session resume), `pact-protocol.md` (frozen v1)

---

## 1. Problem

The driver spends the **same** verification and reasoning budget on a one-line
rename as on an architecture migration, and it has no way to buy less
intelligence for a cheap task. Three measured consequences (tradelinks,
2026-07-26, and this repo's own ledger):

- **Uninformative escalation.** 9 consecutive escalations all read
  `reason: iteration limit exceeded` / `evidence: (global cap)` / `task: ""`,
  while `history` already held the real cause
  (`worker run failed: exit status 1`). The orchestrator had to re-read the
  whole project state 9 times to learn what the driver already knew.
- **Uniform budgets.** `MaxFixRounds` / `MaxRework` / `MaxFails` / critic / QA /
  quorum are all **global flags**. An L0 task pays for a reviewer stint, an
  optional critic stint and a QA stint exactly like an L3 task.
- **No complexity routing.** `PlanTask.Role` exists but its own doc comment says
  *"Purely informational: apply lands only owner/reviewer"*. There is **no**
  effort/complexity concept anywhere in the codebase; the sole complexity signal
  is one sentence in the planner prompt (`"Allocate by complexity"`) that
  nothing enforces. The strongest available model therefore runs every task.

The reference workflow this spec draws from (`AI_Coding_Workflow_Optimization.md`,
"DEP") states the rule directly: **the strong model must be the escalation
coder, not the default coder.**

## 2. Design principle

pactify already implements most of DEP **mechanically** rather than by
instruction: scoped `verify:` per task, a project `config gate` as final
acceptance, `MaxFixRounds`=2 targeted repairs, failure-driven escalation, and a
hard stop at the two protocol invariants.

So this spec does **not** add a prompt-level protocol document asking agents to
behave. It closes the four places where the engine is weaker than that
philosophy, and it does so by **reusing existing channels**:

> `tier` travels in the **spec file**, exactly like `verify:` and `qa:` —
> not in the ledger.

That choice is load-bearing:

- **pact protocol v1 is frozen.** `Assign(taskID, feature, branch, owner,
  reviewer, spec, deps)` carries no room for a tier, and adding an event field
  would be a protocol change. The spec-file channel needs none.
- Runtime already reads spec lines: `taskGateCommand` does
  `extractVerify(readSpec(dir, task.Spec))` with a project-level fallback.
  `tier:` plugs into the same parser in `gate.go` beside `verifyPrefix` /
  `qaPrefix`.
- Spec files are git-tracked and human-editable, so a human reviewing the plan
  can **correct the planner's tier by editing one line** — the cheap answer to
  "what if the planner over- or under-rates a task".

## 3. Scope

Three workstreams. **C is independently shippable and should land first.**

| WS | Name | Depends on |
|----|------|-----------|
| **A** | Tier as a first-class, routable task attribute | — |
| **B** | Tier-derived verification budget | A |
| **C** | Escalation carries real evidence | — |

Non-goals: changing the two protocol invariants; changing owner/reviewer
selection at runtime; a cost model in USD (token budget only — see §8);
per-task session resume for the cmd transport (tracked separately).

---

## 4. WS-A — Tier as a routable attribute

### 4.1 The tier ladder

| Tier | Meaning | Typical |
|------|---------|---------|
| `L0` | Trivial, bounded, low risk | rename, copy edit, one-line fix, add a test case |
| `L1` | **Default.** Ordinary feature work | clear feature, 2–8 files, ordinary refactor |
| `L2` | Complex | cross-module, multi-step, non-obvious bug, migration |
| `L3` | High uncertainty / high risk | architecture, ambiguous root cause, security, concurrency, destructive data ops |

### 4.2 Wire format

The planner writes the tier into **both** places, mirroring how `verify` already
appears in both the manifest and the spec — the manifest copy is what the plan
review UI shows, the spec line is what runtime reads:

Spec file (`.pact/tasks/<feature>-<id>.md`), a line anywhere in the file:

```
tier: L1
```

Manifest (`.pact/plan-<feature>.json`):

```json
{ "id": "step1", "owner": "...", "reviewer": "...", "spec": "...",
  "verify": "go test ./internal/foo/", "tier": "L1", "dimension": "correctness" }
```

### 4.3 Parsing + default (backward compatibility)

- New `tierPrefix = "tier:"` in `internal/orchestrate/gate.go`, parsed by the
  same helper shape as `extractVerify`.
- Value matching is **case-insensitive** and trimmed (`l2`, `L2 `, `L2` all
  parse).
- **Any absent, unparseable, or unknown value resolves to `L1`.**
- `L1` MUST reproduce today's behavior **byte-for-byte** (see §7). Every
  existing spec file and manifest therefore keeps working with zero edits.

### 4.4 New field

```go
// internal/planner/manifest.go
Tier string `json:"tier,omitempty"` // L0|L1|L2|L3; empty = L1
```

Validation: reject a manifest whose `tier` is present but not one of the four
values (fail at plan review, not at runtime).

### 4.5 Runtime routing (tier → model + effort)

Routing changes **how the assigned seat is launched**, never **which seat** is
assigned — the owner/reviewer in the ledger are untouched and the two invariants
are unaffected.

`agentcfg.Effective` today is `{Command, Args, Model, Scoped}`. Add:

```go
Effort string // "", "low", "medium", "high" — "" = leave the CLI's default
```

Because each vendor expresses effort differently, the mapping lives in the
per-kind runner profile as an optional templated flag; a kind that declares none
simply ignores effort (**zero behavior change for that kind**):

| kind | effort flag (illustrative) |
|------|---------------------------|
| `codex-cli` | `-c model_reasoning_effort={{effort}}` |
| `claude-code` | `--effort {{effort}}` |
| `kimi-cli` | `[thinking] effort` / `--no-thinking` at L0 |
| others | none → unchanged |

Default ladder (overridable per project, see §4.6):

| Tier | Effort | Model preference |
|------|--------|------------------|
| `L0` | `low` | cheapest capable seat model |
| `L1` | `medium` | seat default |
| `L2` | `medium` | seat default |
| `L3` | `high` | seat default |

Note the deliberate restraint: **L2 does not raise effort.** Escalation raises
it (§6). Tier sets the *starting* budget; only evidence of failure buys more.

### 4.6 Plan-time routing

The planner already receives the machine's **role catalog** (`role → kind/model`
+ bound seats). WS-A makes the existing advisory instruction concrete: the
planner MUST emit a tier per task, and SHOULD prefer a cheap role-bound seat for
`L0`/`L1` and a capable one for `L2`/`L3`. This is the lever that actually moves
cost — runtime effort tuning is secondary to not handing a rename to the
strongest model in the first place.

---

## 5. WS-B — Tier-derived verification budget

Today `MaxFixRounds`, `MaxRework`, `MaxFails`, critic and QA are global. WS-B
resolves them **per task** from its tier, with the CLI flag still winning when
explicitly set (an operator override must remain absolute).

| Tier | fix rounds | MaxRework | MaxFails | critic | QA |
|------|-----------:|----------:|---------:|--------|----|
| `L0` | 1 | 2 | 2 | off | off |
| `L1` | **2** | **3** | **2** | **off** | per `qa:` line |
| `L2` | 2 | 3 | 2 | on (if configured) | per `qa:` line |
| `L3` | 3 | 4 | 3 | on (if configured) | per `qa:` line |

L1 reproduces today's defaults exactly (`MaxFixRounds`=2, `MaxRework`=3,
`MaxFails`=2, critic only when configured).

Resolution order for each knob, highest first:

1. explicit CLI flag (`--max-fix-rounds`, `--critic`, …)
2. tier-derived value
3. current default

The reviewer stint itself is **never** tier-gated — "a worker cannot accept its
own work" is a protocol invariant, not a budget. What L0 skips is the *optional*
critic and QA stints, which are pure overhead on a rename.

---

## 6. WS-C — Escalation carries real evidence

Two defects, both cheap to fix, both already backed by data the driver holds.

### 6.1 The global cap must not discard per-task context

The check order is **already correct**: `tripped()` (per-task failure/rework,
loop.go:437) runs *before* the global `MaxIters` cap (loop.go:479). No reordering
is needed.

The actual defect is what happens when the **global** cap fires. `Fails` counts
*consecutive* failures and is reset on any progress
(`h.Fails[act.Task] = 0 // the review ran (progress)`), so a run that keeps
half-progressing never accumulates to `MaxFails` — `tripped()` never fires, and
the loop runs to the iteration cap. That path then escalates with
`task: ""` / `evidence: "(global cap)"`, **throwing away everything the driver
still holds in `h`**: which tasks are unfinished, each one's `LastFail` /
`LastClass`, rework counts, and fix rounds spent. That is precisely the
tradelinks case — `history` recorded
`legacy-backfill → "worker run failed: exit status 1"` while the escalation said
only "iteration limit exceeded".

Requirement: when the iteration cap fires, the record MUST enumerate the
unfinished tasks with their recorded last failure and class, instead of a single
global slogan.

### 6.2 Escalation records carry a handoff, not a slogan

An escalation record MUST include, when known:

```
Goal            feature + task id
Current state   task status, iteration, fix rounds spent
Failure         history.LastFail[task]  (the real cause)
Class           history.LastClass[task] (env vs logic)
Evidence        last verify/gate output tail (bounded, see below)
Files           the task spec's declared file scope
Next            the concrete resume command
```

`LastFail` and `LastClass` are **already persisted** — this is wiring, not new
machinery. Bound the evidence tail (suggest 4 KB) so an escalation record can
never become the next `pact://log` bloat problem.

**Why this is the highest-value item:** an escalation whose reason is
`"(global cap)"` forces the orchestrator to re-read the full project state to
diagnose it. Measured cost of that re-read: `status` ≈ 8 k tokens (tradelinks) /
≈ 24 k (this repo), `pact://log` ≈ 17 k. Nine escalations paid it nine times.

---

## 7. Backward compatibility (hard requirement)

- A spec file with no `tier:` line → `L1` → **byte-identical** briefings, gates,
  budgets, critic/QA behavior, and escalation flow vs today.
- A manifest with no `tier` field → valid, `L1`.
- A kind with no effort flag → launched exactly as today.
- No ledger event gains a field; no `Assign` signature changes; `pact-protocol.md`
  v1 is untouched.

A regression test MUST pin the no-tier path against current behavior.

---

## 8. Token budget (deferred, but reserve the seam)

A `Thresholds.MaxTokens` hard stop is the natural fourth piece (the per-task
store `internal/tokens` already accumulates usage). It is **out of scope here**
to keep this change reviewable, but WS-B's per-task budget resolution is the
place it will plug in. Do not design it away.

---

## 9. Acceptance

Each task carries its own scoped `verify:`; these are the feature-level gates.

**WS-A**
- `tier:` parses case-insensitively from a spec file; absent/garbage → `L1`.
- Manifest round-trips `tier`; a bad value is rejected at plan validation.
- A launch for an L0 task on an effort-capable kind carries the low-effort flag;
  a kind without an effort flag launches byte-identically to today.
- `verify: go test ./internal/orchestrate/ ./internal/planner/ ./internal/agentcfg/`

**WS-B**
- Budgets resolve per task from tier; an explicit CLI flag overrides the tier.
- An L0 task runs no critic and no QA stint; its fix budget is 1.
- An L1 task's resolved budgets equal today's defaults.
- `verify: go test ./internal/orchestrate/`

**WS-C**
- An iteration-cap escalation enumerates the unfinished tasks with each one's
  recorded `LastFail` / `LastClass`, instead of only `"(global cap)"`.
- A per-task escalation record contains Goal / Failure / Class / Evidence / Next,
  with the evidence tail bounded.
- Regression: a run whose `Fails` never accumulate (reset by partial progress)
  still produces a diagnosable record when it hits the cap — the tradelinks
  shape.
- `verify: go test ./internal/orchestrate/`

**Feature gate:** `go build ./... && go test ./... && go vet ./...`

---

## 10. Why this also makes pactify's own development cheaper

pactify is developed **by** pactify (dogfood: multi-agent CLI fleet, orchestrator
seat drives the ledger). Every budget the engine stops spending is a budget
pactify's own delivery stops paying. That is why this lands in the engine rather
than in an `AGENTS.md`: a document constrains only the agents that read it; the
engine constrains every agent, including the ones building pactify.

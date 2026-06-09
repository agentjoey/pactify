# Phase 0 — Pact Protocol Skill Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `.pact/bin/pact.sh` (7-verb bash tool implementing the pact protocol) + a thin Claude skill, then dogfood it to validate "eliminate human relay."

**Architecture:** `log.jsonl` is the append-only source of truth (JSON lines, queried with `jq`). `STATE.yml` is a **render-only projection** — re-rendered from the log after every mutation, never parsed. Every verb: validate invariant (from log via jq) → append event → re-render STATE.yml. The two invariants (no self-accept, no merge-until-all-accepted) are computed from the log. Agent identity is a self-declared project-scoped "seat" carried in `PACT_AGENT_ID`; verbs fail-closed if it is unset.

**Tech Stack:** bash, `jq` (already installed), `bats-core` (test framework, installed in Task 0), git.

---

## File Structure

| File | Responsibility |
|---|---|
| `.pact/bin/pact.sh` | The tool: shared vars, private helpers, projection (jq), 7 verbs + `log`/`validate`/`--help`. Self-contained & self-copying (pact_init copies it). |
| `.pact/PROJECT.md` | Charter + seat table (rendered by `pact_init`). Portable protocol knowledge for any agent. |
| `.pact/STATE.yml` | Rendered projection (human/agent read-only view). |
| `.pact/tasks/<id>.md` | Per-task spec/plan/acceptance (agent-authored). |
| `.pact/log.jsonl` | Append-only event source. |
| `.claude/skills/pact/SKILL.md` | Thin Claude convenience skin; points to `.pact/`. |
| `tests/helpers.bash` | bats helpers: temp-repo setup, seed log, assert helpers. |
| `tests/*.bats` | One bats file per concern. |

**Key conventions used throughout:**
- Shared vars in pact.sh: `PACT_DIR="${PACT_DIR:-.pact}"`, `PACT_LOG="$PACT_DIR/log.jsonl"`, `PACT_STATE="$PACT_DIR/STATE.yml"`, `PACT_TASKS="$PACT_DIR/tasks"`.
- `init` event payload carries `{project, seats:[{id,roles,entry}]}` — the seat registry, so nothing parses PROJECT.md.
- Event shape: `{ts, agent_id, role, event_type, task_id, feature, payload}`. `role` is derived from the verb.
- Verb→role: `init/assign/merge→orchestrator`, `join/checkpoint→worker`, `accept/changes_requested→reviewer`.
- Task state machine: `todo→assigned→in_progress→awaiting_review→(accepted | changes_requested→in_progress)`. `pact_join` moves the joining seat's `assigned` tasks → `in_progress`.
- Feature state machine: `planned→in_progress→awaiting_review→accepted→shipped`.

---

## Task 0: Project setup — bats, dirs, test helpers

**Files:**
- Create: `tests/helpers.bash`
- Create: `tests/smoke.bats`
- Create: `.pact/bin/.gitkeep` (placeholder so dir exists)

- [ ] **Step 1: Install bats-core**

Run: `brew install bats-core`
Expected: `bats` on PATH. Verify: `bats --version` prints `Bats 1.x`.

- [ ] **Step 2: Create test helpers**

Create `tests/helpers.bash`:

```bash
# tests/helpers.bash — shared bats helpers for pact.sh tests

# Absolute path to the pact.sh under test (repo-root relative).
PACT_SH="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)/.pact/bin/pact.sh"

# setup_pact_repo: make a throwaway git repo in BATS_TEST_TMPDIR, cd into it,
# and source pact.sh. All tests operate here, never the real repo.
setup_pact_repo() {
  cd "$BATS_TEST_TMPDIR" || return 1
  rm -rf work && mkdir work && cd work || return 1
  git init -q
  git config user.email t@t.t
  git config user.name t
  # Copy pact.sh into place so pact_init's self-copy has a source.
  mkdir -p .pact/bin
  cp "$PACT_SH" .pact/bin/pact.sh
  # Base commit so later branch ops aren't on an unborn HEAD.
  git add -A && git commit -q -m "base"
  source .pact/bin/pact.sh
}

# log_lines: number of events in the log.
log_lines() { wc -l < .pact/log.jsonl | tr -d ' '; }

# task_status <task_id>: read current status from rendered STATE via grep.
task_status() {
  grep -A4 "id: $1\$" .pact/STATE.yml | grep -m1 'status:' | awk '{print $2}'
}
```

- [ ] **Step 3: Write a smoke test that proves the harness works**

Create `tests/smoke.bats`:

```bash
#!/usr/bin/env bats
load helpers

@test "harness: temp repo + pact.sh sourcing works" {
  setup_pact_repo
  [ -f .pact/bin/pact.sh ]
  declare -f pact_init >/dev/null
}
```

- [ ] **Step 4: Run smoke test — expect FAIL (pact.sh has no pact_init yet)**

Run: `bats tests/smoke.bats`
Expected: FAIL — `pact_init` not defined (pact.sh is empty/placeholder).

- [ ] **Step 5: Create a minimal pact.sh stub so sourcing succeeds**

Create `.pact/bin/pact.sh` (replace the `.gitkeep`):

```bash
#!/usr/bin/env bash
# pact.sh — pact protocol reference implementation (Phase 0).
# Sourced by agents. All state lives under $PACT_DIR.

PACT_DIR="${PACT_DIR:-.pact}"
PACT_LOG="$PACT_DIR/log.jsonl"
PACT_STATE="$PACT_DIR/STATE.yml"
PACT_TASKS="$PACT_DIR/tasks"

pact_init() { echo "stub" >&2; return 1; }
```

- [ ] **Step 6: Run smoke test — expect PASS**

Run: `bats tests/smoke.bats`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add tests/helpers.bash tests/smoke.bats .pact/bin/pact.sh
git commit -m "test: bats harness + pact.sh stub for Phase 0"
```

---

## Task 1: Core private helpers — timestamp, fail-closed id, atomic append

**Files:**
- Modify: `.pact/bin/pact.sh`
- Create: `tests/helpers_core.bats`

- [ ] **Step 1: Write failing tests**

Create `tests/helpers_core.bats`:

```bash
#!/usr/bin/env bats
load helpers

@test "_pact_now emits UTC ISO-8601" {
  setup_pact_repo
  run _pact_now
  [ "$status" -eq 0 ]
  [[ "$output" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]
}

@test "_pact_require_id fails closed when PACT_AGENT_ID unset" {
  setup_pact_repo
  unset PACT_AGENT_ID
  run _pact_require_id
  [ "$status" -eq 1 ]
  [[ "$output" == *"PACT_AGENT_ID not set"* ]]
}

@test "_pact_require_id passes when PACT_AGENT_ID set" {
  setup_pact_repo
  export PACT_AGENT_ID=claude-opus
  run _pact_require_id
  [ "$status" -eq 0 ]
}

@test "_pact_log_append writes one valid JSON line" {
  setup_pact_repo
  export PACT_AGENT_ID=claude-opus
  : > .pact/log.jsonl
  _pact_log_append join worker "" "" '{"roles":["worker"]}'
  [ "$(log_lines)" -eq 1 ]
  run jq -e '.agent_id=="claude-opus" and .event_type=="join" and .role=="worker"' .pact/log.jsonl
  [ "$status" -eq 0 ]
}
```

- [ ] **Step 2: Run — expect FAIL (helpers undefined)**

Run: `bats tests/helpers_core.bats`
Expected: FAIL — `_pact_now` / `_pact_require_id` / `_pact_log_append` not found.

- [ ] **Step 3: Implement the helpers**

In `.pact/bin/pact.sh`, replace the `pact_init` stub line with these helpers (keep the shared vars above them):

```bash
_pact_now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

_pact_require_id() {
  if [ -z "${PACT_AGENT_ID:-}" ]; then
    echo "pact: PACT_AGENT_ID not set; source your entry file (e.g. AGENTS.md)" >&2
    return 1
  fi
}

# _pact_log_append <event_type> <role> <task_id> <feature> <payload_json>
# Appends one event line. Single printf of a pre-built line → atomic under O_APPEND.
_pact_log_append() {
  local et="$1" role="$2" task="$3" feature="$4" payload="${5:-{}}"
  local line
  line=$(jq -nc \
    --arg ts "$(_pact_now)" --arg id "$PACT_AGENT_ID" --arg role "$role" \
    --arg et "$et" --arg task "$task" --arg feature "$feature" \
    --argjson payload "$payload" \
    '{ts:$ts,agent_id:$id,role:$role,event_type:$et,task_id:$task,feature:$feature,payload:$payload}')
  printf '%s\n' "$line" >> "$PACT_LOG"
}

pact_init() { echo "not yet implemented" >&2; return 1; }
```

- [ ] **Step 4: Run — expect PASS**

Run: `bats tests/helpers_core.bats`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add .pact/bin/pact.sh tests/helpers_core.bats
git commit -m "feat: pact.sh core helpers (now/require_id/log_append)"
```

---

## Task 2: Projection — compute state JSON from log, render STATE.yml

**Files:**
- Modify: `.pact/bin/pact.sh`
- Create: `tests/projection.bats`

This is the core of "log is source, STATE is projection." `_pact_project_json` folds events into a state object; `_pact_render_state` writes `STATE.yml` atomically.

- [ ] **Step 1: Write failing tests (seed a synthetic log, assert projection + render)**

Create `tests/projection.bats`:

```bash
#!/usr/bin/env bats
load helpers

seed_log() {
  : > .pact/log.jsonl
  cat >> .pact/log.jsonl <<'EOF'
{"ts":"2026-06-09T10:00:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{"project":"pactify","seats":[{"id":"claude-opus","roles":["orchestrator","reviewer"],"entry":"CLAUDE.md"},{"id":"opencode","roles":["worker"],"entry":"AGENTS.md"}]}}
{"ts":"2026-06-09T10:01:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"assign","task_id":"T1","feature":"CLI-INIT","payload":{"owner":"opencode","reviewer":"claude-opus","branch":"feat/cli-init","spec":".pact/tasks/T1.md"}}
{"ts":"2026-06-09T10:02:00Z","agent_id":"opencode","role":"worker","event_type":"join","task_id":"","feature":"","payload":{"roles":["worker"]}}
{"ts":"2026-06-09T10:03:00Z","agent_id":"opencode","role":"worker","event_type":"checkpoint","task_id":"T1","feature":"CLI-INIT","payload":{"evidence":"237 tests green"}}
EOF
}

@test "projection: project + seats from init event" {
  setup_pact_repo
  seed_log
  run bash -c '_pact_project_json | jq -r ".project"'
  [ "$output" = "pactify" ]
  run bash -c '_pact_project_json | jq -r ".agents | length"'
  [ "$output" = "2" ]
}

@test "projection: join moves owned assigned task to in_progress, checkpoint to awaiting_review" {
  setup_pact_repo
  seed_log
  # After full seed (incl checkpoint) status is awaiting_review with evidence.
  run bash -c '_pact_project_json | jq -r ".features[0].tasks[0].status"'
  [ "$output" = "awaiting_review" ]
  run bash -c '_pact_project_json | jq -r ".features[0].tasks[0].evidence"'
  [ "$output" = "237 tests green" ]
  run bash -c '_pact_project_json | jq -r ".features[0].tasks[0].owner"'
  [ "$output" = "opencode" ]
}

@test "render_state writes valid YAML view that grep helpers can read" {
  setup_pact_repo
  seed_log
  _pact_render_state
  [ -f .pact/STATE.yml ]
  grep -q "project: pactify" .pact/STATE.yml
  [ "$(task_status T1)" = "awaiting_review" ]
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `bats tests/projection.bats`
Expected: FAIL — `_pact_project_json` not defined.

- [ ] **Step 3: Implement projection + render**

In `.pact/bin/pact.sh`, add above the `pact_init` line:

```bash
# _pact_project_json: fold log.jsonl events into the projection object.
_pact_project_json() {
  jq -s '
    def last_of(t): map(select(.event_type==t)) | last;
    (last_of("init")) as $init
    | ($init.payload.seats // []) as $seats
    | reduce .[] as $e (
        {features:{}};
        if $e.event_type=="assign" then
          .features[$e.feature].id = $e.feature
          | .features[$e.feature].branch = ($e.payload.branch // "")
          | (.features[$e.feature].status //= "in_progress")
          | .features[$e.feature].tasks[$e.task_id] = {
              id:$e.task_id, owner:$e.payload.owner, reviewer:$e.payload.reviewer,
              spec:($e.payload.spec // ""), status:"assigned", evidence:null }
        elif $e.event_type=="join" then
          # joining seat picks up its assigned tasks
          reduce (.features | keys[]) as $f (.;
            reduce (.features[$f].tasks | keys[]) as $t (.;
              if .features[$f].tasks[$t].owner==$e.agent_id
                 and .features[$f].tasks[$t].status=="assigned"
              then .features[$f].tasks[$t].status="in_progress" else . end))
        elif $e.event_type=="checkpoint" then
          .features[$e.feature].tasks[$e.task_id].status="awaiting_review"
          | .features[$e.feature].tasks[$e.task_id].evidence=$e.payload.evidence
        elif $e.event_type=="accept" then
          .features[$e.feature].tasks[$e.task_id].status="accepted"
        elif $e.event_type=="changes_requested" then
          .features[$e.feature].tasks[$e.task_id].status="changes_requested"
        elif $e.event_type=="merge" then
          .features[$e.feature].status="shipped"
        else . end
      )
    | {
        project: ($init.payload.project // "unknown"),
        working_tree_holder: null,
        agents: ($seats | map({id, roles})),
        features: (.features | to_entries | map(.value
                   | .tasks = (.tasks | to_entries | map(.value))))
      }
  ' "$PACT_LOG"
}

# _pact_render_state: render STATE.yml from the projection (atomic write).
_pact_render_state() {
  local tmp="$PACT_STATE.tmp"
  {
    local j; j=$(_pact_project_json)
    echo "project: $(jq -r '.project' <<<"$j")"
    echo "working_tree_holder: null"
    echo "agents:"
    jq -r '.agents[] | "  - { id: \(.id), roles: [\(.roles | join(", "))] }"' <<<"$j"
    echo "features:"
    jq -r '
      .features[] |
      "  - id: \(.id)",
      "    branch: \(.branch)",
      "    status: \(.status)",
      "    tasks:",
      ( .tasks[] |
        "      - id: \(.id)",
        "        owner: \(.owner)",
        "        status: \(.status)",
        "        reviewer: \(.reviewer)",
        "        spec: \(.spec)",
        "        evidence: \(.evidence // "null")"
      )' <<<"$j"
  } > "$tmp" && mv "$tmp" "$PACT_STATE"
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `bats tests/projection.bats`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add .pact/bin/pact.sh tests/projection.bats
git commit -m "feat: pact.sh projection (log fold) + STATE.yml render"
```

---

## Task 3: `pact_init` — scaffold .pact/ + bake entry files

**Files:**
- Modify: `.pact/bin/pact.sh`
- Create: `tests/init.bats`

`pact_init` signature: `pact_init --project <name> --seat "<id>:<role1,role2>:<entryfile>" [--seat ...]`

- [ ] **Step 1: Write failing tests**

Create `tests/init.bats`:

```bash
#!/usr/bin/env bats
load helpers

run_init() {
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify \
    --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
}

@test "init creates skeleton dirs and files" {
  setup_pact_repo
  run_init
  [ -f .pact/PROJECT.md ]
  [ -f .pact/log.jsonl ]
  [ -f .pact/STATE.yml ]
  [ -d .pact/tasks ]
}

@test "init writes one init event carrying project + seats" {
  setup_pact_repo
  run_init
  [ "$(log_lines)" -eq 1 ]
  run jq -r '.event_type' .pact/log.jsonl
  [ "$output" = "init" ]
  run jq -r '.payload.seats | length' .pact/log.jsonl
  [ "$output" = "2" ]
}

@test "init bakes entry files with seat id + join command" {
  setup_pact_repo
  run_init
  grep -q "PACT_AGENT_ID=opencode" AGENTS.md
  grep -q "pact_join opencode --roles worker" AGENTS.md
  grep -q "PACT_AGENT_ID=claude-opus" CLAUDE.md
}

@test "init renders STATE with project name and seat roster" {
  setup_pact_repo
  run_init
  grep -q "project: pactify" .pact/STATE.yml
  grep -q "id: opencode" .pact/STATE.yml
}

@test "init fails closed without PACT_AGENT_ID" {
  setup_pact_repo
  unset PACT_AGENT_ID
  run pact_init --project x --seat "a:worker:A.md"
  [ "$status" -eq 1 ]
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `bats tests/init.bats`
Expected: FAIL — `pact_init` is a stub.

- [ ] **Step 3: Implement `pact_init`**

In `.pact/bin/pact.sh`, replace the `pact_init() { echo "not yet implemented"...}` line:

```bash
# pact_init --project <name> --seat "<id>:<roles>:<entry>" [--seat ...]
pact_init() {
  _pact_require_id || return 1
  local project="" ; local -a seats=()
  while [ $# -gt 0 ]; do
    case "$1" in
      --project) project="$2"; shift 2;;
      --seat) seats+=("$2"); shift 2;;
      *) echo "pact_init: unknown arg $1" >&2; return 1;;
    esac
  done
  [ -n "$project" ] || { echo "pact_init: --project required" >&2; return 1; }
  mkdir -p "$PACT_DIR/bin" "$PACT_TASKS"
  # self-copy if not already present (so target repos get the tool)
  [ -f "$PACT_DIR/bin/pact.sh" ] || cp "${BASH_SOURCE[0]}" "$PACT_DIR/bin/pact.sh"
  : > "$PACT_LOG"

  # build seats JSON + bake entry files
  local seats_json="[]" s id roles entry
  for s in "${seats[@]}"; do
    IFS=':' read -r id roles entry <<<"$s"
    seats_json=$(jq -c \
      --arg id "$id" --arg entry "$entry" \
      --argjson roles "$(jq -nc --arg r "$roles" '$r | split(",")')" \
      '. + [{id:$id, roles:$roles, entry:$entry}]' <<<"$seats_json")
    _pact_bake_entry "$id" "$roles" "$entry"
  done

  # PROJECT.md charter
  _pact_render_project "$project" "$seats_json" > "$PACT_DIR/PROJECT.md"

  # init event + render
  local payload
  payload=$(jq -nc --arg p "$project" --argjson seats "$seats_json" \
    '{project:$p, seats:$seats}')
  _pact_log_append init orchestrator "" "" "$payload"
  _pact_render_state
}

# _pact_bake_entry <id> <roles_csv> <entryfile>
_pact_bake_entry() {
  local id="$1" roles="$2" entry="$3"
  cat > "$entry" <<EOF
# Entry: seat \`$id\` — pact protocol

> Auto-baked by pact_init. On session start, run:

\`\`\`bash
export PACT_AGENT_ID=$id
source .pact/bin/pact.sh && pact_join $id --roles $roles
\`\`\`

Then read \`.pact/PROJECT.md\` (protocol + roles + rules) and \`.pact/STATE.yml\` (current state).
EOF
}

# _pact_render_project <name> <seats_json>
_pact_render_project() {
  local name="$1" seats_json="$2"
  cat <<EOF
# $name — Pact Charter

This repo uses the **pact protocol**. Any agent that can read files + run git can participate.

## Roles
- **orchestrator** — split spec→tasks; assign; merge; maintain charter
- **worker** — implement; at checkpoint set awaiting_review + write evidence
- **reviewer** — verify diff+evidence → accept / changes_requested
- **human** — start button + final authority

## The two rules (the pact)
1. A worker cannot self-accept. Only a task's reviewer may accept it (owner != reviewer).
2. A feature cannot merge until all its tasks are accepted.

## Seats
$(jq -r '.[] | "- \`\(.id)\` — roles: \(.roles | join(", ")) — entry: \(.entry)"' <<<"$seats_json")

## Commands (source .pact/bin/pact.sh)
Run \`pact_help\` for the full verb reference.
EOF
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `bats tests/init.bats`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add .pact/bin/pact.sh tests/init.bats
git commit -m "feat: pact_init scaffolds .pact/ and bakes entry files"
```

---

## Task 4: `pact_join` — register seat, pick up assigned tasks

**Files:**
- Modify: `.pact/bin/pact.sh`
- Create: `tests/join.bats`

- [ ] **Step 1: Write failing tests**

Create `tests/join.bats`:

```bash
#!/usr/bin/env bats
load helpers

bootstrap() {
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify \
    --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
}

@test "join appends a join event" {
  setup_pact_repo; bootstrap
  export PACT_AGENT_ID=opencode
  pact_join opencode --roles worker
  run jq -rs '.[-1].event_type' .pact/log.jsonl
  [ "$output" = "join" ]
}

@test "join fails closed without PACT_AGENT_ID" {
  setup_pact_repo; bootstrap
  unset PACT_AGENT_ID
  run pact_join opencode --roles worker
  [ "$status" -eq 1 ]
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `bats tests/join.bats`
Expected: FAIL — `pact_join` not defined.

- [ ] **Step 3: Implement `pact_join`**

In `.pact/bin/pact.sh`, add above `pact_help`/at the verbs section:

```bash
# pact_join <id> [--roles r1,r2]
pact_join() {
  _pact_require_id || return 1
  local id="$1"; shift || true
  local roles=""
  while [ $# -gt 0 ]; do
    case "$1" in --roles) roles="$2"; shift 2;; *) shift;; esac
  done
  local payload; payload=$(jq -nc --arg r "$roles" '{roles:($r|split(","))}')
  _pact_log_append join worker "" "" "$payload"
  _pact_render_state
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `bats tests/join.bats`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add .pact/bin/pact.sh tests/join.bats
git commit -m "feat: pact_join registers seat and renders roster"
```

---

## Task 5: `pact_assign` — create task + enforce owner≠reviewer

**Files:**
- Modify: `.pact/bin/pact.sh`
- Create: `tests/assign.bats`

Signature: `pact_assign <task_id> --feature <f> --branch <b> --owner <id> --reviewer <id> [--spec <path>]`

- [ ] **Step 1: Write failing tests**

Create `tests/assign.bats`:

```bash
#!/usr/bin/env bats
load helpers

bootstrap() {
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify \
    --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
}

@test "assign creates an assigned task in STATE" {
  setup_pact_repo; bootstrap
  pact_assign T1 --feature CLI-INIT --branch feat/cli-init \
    --owner opencode --reviewer claude-opus --spec .pact/tasks/T1.md
  [ "$(task_status T1)" = "assigned" ]
  grep -q "owner: opencode" .pact/STATE.yml
}

@test "assign rejects owner == reviewer (rule 1 at assign time)" {
  setup_pact_repo; bootstrap
  run pact_assign T1 --feature F --branch b --owner opencode --reviewer opencode
  [ "$status" -ne 0 ]
  [[ "$output" == *"owner"* ]]
}

@test "assign fails closed without PACT_AGENT_ID" {
  setup_pact_repo; bootstrap
  unset PACT_AGENT_ID
  run pact_assign T1 --feature F --branch b --owner opencode --reviewer claude-opus
  [ "$status" -eq 1 ]
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `bats tests/assign.bats`
Expected: FAIL — `pact_assign` not defined.

- [ ] **Step 3: Implement `pact_assign`**

In `.pact/bin/pact.sh`, verbs section:

```bash
# pact_assign <task_id> --feature <f> --branch <b> --owner <id> --reviewer <id> [--spec <p>]
pact_assign() {
  _pact_require_id || return 1
  local task="$1"; shift
  local feature="" branch="" owner="" reviewer="" spec=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --feature) feature="$2"; shift 2;;
      --branch) branch="$2"; shift 2;;
      --owner) owner="$2"; shift 2;;
      --reviewer) reviewer="$2"; shift 2;;
      --spec) spec="$2"; shift 2;;
      *) echo "pact_assign: unknown arg $1" >&2; return 1;;
    esac
  done
  [ -n "$owner" ] && [ -n "$reviewer" ] || {
    echo "pact_assign: --owner and --reviewer required" >&2; return 1; }
  if [ "$owner" = "$reviewer" ]; then
    echo "pact_assign: owner ($owner) must differ from reviewer (separation of duties)" >&2
    return 1
  fi
  [ -n "$spec" ] || spec="$PACT_TASKS/$task.md"
  local payload
  payload=$(jq -nc --arg o "$owner" --arg r "$reviewer" --arg b "$branch" --arg s "$spec" \
    '{owner:$o, reviewer:$r, branch:$b, spec:$s}')
  _pact_log_append assign orchestrator "$task" "$feature" "$payload"
  _pact_render_state
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `bats tests/assign.bats`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add .pact/bin/pact.sh tests/assign.bats
git commit -m "feat: pact_assign creates task + enforces owner!=reviewer"
```

---

## Task 6: `pact_checkpoint` — worker submits for review with evidence

**Files:**
- Modify: `.pact/bin/pact.sh`
- Create: `tests/checkpoint.bats`

Signature: `pact_checkpoint <task_id> --evidence "<text>"`. Worker-only: caller must own the task.

- [ ] **Step 1: Write failing tests**

Create `tests/checkpoint.bats`:

```bash
#!/usr/bin/env bats
load helpers

bootstrap_assigned() {
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify \
    --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  pact_assign T1 --feature CLI-INIT --branch feat/cli-init \
    --owner opencode --reviewer claude-opus
}

@test "checkpoint by owner sets awaiting_review + evidence" {
  setup_pact_repo; bootstrap_assigned
  export PACT_AGENT_ID=opencode
  pact_join opencode --roles worker
  pact_checkpoint T1 --evidence "237 tests green, build ok"
  [ "$(task_status T1)" = "awaiting_review" ]
  grep -q "evidence: 237 tests green, build ok" .pact/STATE.yml
}

@test "checkpoint by non-owner is rejected" {
  setup_pact_repo; bootstrap_assigned
  export PACT_AGENT_ID=claude-opus
  run pact_checkpoint T1 --evidence "x"
  [ "$status" -ne 0 ]
  [[ "$output" == *"owner"* ]]
}

@test "checkpoint requires --evidence" {
  setup_pact_repo; bootstrap_assigned
  export PACT_AGENT_ID=opencode
  run pact_checkpoint T1
  [ "$status" -ne 0 ]
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `bats tests/checkpoint.bats`
Expected: FAIL — `pact_checkpoint` not defined.

- [ ] **Step 3: Implement `pact_checkpoint` + task-lookup helper**

In `.pact/bin/pact.sh`, add a lookup helper near the projection helpers:

```bash
# _pact_task_field <task_id> <field> : read a field of a task from the projection.
_pact_task_field() {
  _pact_project_json | jq -r --arg t "$1" --arg f "$2" \
    '.features[].tasks[] | select(.id==$t) | .[$f] // empty'
}
# _pact_task_feature <task_id> : the feature id owning a task.
_pact_task_feature() {
  _pact_project_json | jq -r --arg t "$1" \
    '.features[] | select(.tasks[].id==$t) | .id' | head -1
}
```

Then the verb:

```bash
# pact_checkpoint <task_id> --evidence "<text>"
pact_checkpoint() {
  _pact_require_id || return 1
  local task="$1"; shift
  local evidence=""
  while [ $# -gt 0 ]; do
    case "$1" in --evidence) evidence="$2"; shift 2;; *) shift;; esac
  done
  [ -n "$evidence" ] || { echo "pact_checkpoint: --evidence required" >&2; return 1; }
  local owner; owner=$(_pact_task_field "$task" owner)
  if [ "$owner" != "$PACT_AGENT_ID" ]; then
    echo "pact_checkpoint: $PACT_AGENT_ID is not the owner of $task (owner: $owner)" >&2
    return 1
  fi
  local feature; feature=$(_pact_task_feature "$task")
  local payload; payload=$(jq -nc --arg e "$evidence" '{evidence:$e}')
  _pact_log_append checkpoint worker "$task" "$feature" "$payload"
  _pact_render_state
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `bats tests/checkpoint.bats`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add .pact/bin/pact.sh tests/checkpoint.bats
git commit -m "feat: pact_checkpoint (owner-only) sets awaiting_review + evidence"
```

---

## Task 7: `pact_accept` / `pact_changes` — reviewer verdict + enforce rule 1

**Files:**
- Modify: `.pact/bin/pact.sh`
- Create: `tests/accept.bats`

- [ ] **Step 1: Write failing tests**

Create `tests/accept.bats`:

```bash
#!/usr/bin/env bats
load helpers

to_awaiting() {
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify \
    --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  pact_assign T1 --feature CLI-INIT --branch feat/cli-init \
    --owner opencode --reviewer claude-opus
  export PACT_AGENT_ID=opencode
  pact_join opencode --roles worker
  pact_checkpoint T1 --evidence "tests green"
}

@test "accept by the reviewer sets accepted" {
  setup_pact_repo; to_awaiting
  export PACT_AGENT_ID=claude-opus
  pact_accept T1
  [ "$(task_status T1)" = "accepted" ]
}

@test "accept by the worker (owner) is rejected (rule 1)" {
  setup_pact_repo; to_awaiting
  export PACT_AGENT_ID=opencode
  run pact_accept T1
  [ "$status" -ne 0 ]
  [[ "$output" == *"reviewer"* ]]
}

@test "changes_requested sends task back" {
  setup_pact_repo; to_awaiting
  export PACT_AGENT_ID=claude-opus
  pact_changes T1 --reason "fix lint"
  [ "$(task_status T1)" = "changes_requested" ]
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `bats tests/accept.bats`
Expected: FAIL — `pact_accept` / `pact_changes` not defined.

- [ ] **Step 3: Implement `pact_accept` + `pact_changes`**

In `.pact/bin/pact.sh`, verbs section:

```bash
# pact_accept <task_id> : reviewer-only.
pact_accept() {
  _pact_require_id || return 1
  local task="$1"
  local reviewer; reviewer=$(_pact_task_field "$task" reviewer)
  if [ "$reviewer" != "$PACT_AGENT_ID" ]; then
    echo "pact_accept: only the reviewer ($reviewer) may accept $task; you are $PACT_AGENT_ID" >&2
    return 1
  fi
  local feature; feature=$(_pact_task_feature "$task")
  _pact_log_append accept reviewer "$task" "$feature" '{}'
  _pact_render_state
}

# pact_changes <task_id> --reason "<text>" : reviewer-only; sends task back.
pact_changes() {
  _pact_require_id || return 1
  local task="$1"; shift
  local reason=""
  while [ $# -gt 0 ]; do
    case "$1" in --reason) reason="$2"; shift 2;; *) shift;; esac
  done
  local reviewer; reviewer=$(_pact_task_field "$task" reviewer)
  if [ "$reviewer" != "$PACT_AGENT_ID" ]; then
    echo "pact_changes: only the reviewer ($reviewer) may review $task" >&2
    return 1
  fi
  local feature; feature=$(_pact_task_feature "$task")
  local payload; payload=$(jq -nc --arg r "$reason" '{reason:$r}')
  _pact_log_append changes_requested reviewer "$task" "$feature" "$payload"
  _pact_render_state
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `bats tests/accept.bats`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add .pact/bin/pact.sh tests/accept.bats
git commit -m "feat: pact_accept/pact_changes (reviewer-only) enforce rule 1"
```

---

## Task 8: `pact_merge` — enforce rule 2 + git merge --no-ff

**Files:**
- Modify: `.pact/bin/pact.sh`
- Create: `tests/merge.bats`

- [ ] **Step 1: Write failing tests**

Create `tests/merge.bats`:

```bash
#!/usr/bin/env bats
load helpers

@test "merge rejected when a task is not accepted (rule 2)" {
  setup_pact_repo
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  pact_assign T1 --feature CLI-INIT --branch feat/cli-init \
    --owner opencode --reviewer claude-opus
  run pact_merge CLI-INIT
  [ "$status" -ne 0 ]
  [[ "$output" == *"not accepted"* ]]
}

@test "merge succeeds when all tasks accepted; feature -> shipped" {
  setup_pact_repo
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  git add -A && git commit -q -m "pact init"
  # create a real branch with a commit so --no-ff has something to merge
  git checkout -q -b feat/cli-init
  echo hi > f.txt && git add f.txt && git commit -q -m "work"
  git checkout -q -
  pact_assign T1 --feature CLI-INIT --branch feat/cli-init \
    --owner opencode --reviewer claude-opus
  export PACT_AGENT_ID=opencode; pact_join opencode --roles worker
  pact_checkpoint T1 --evidence "ok"
  export PACT_AGENT_ID=claude-opus; pact_accept T1
  run pact_merge CLI-INIT
  [ "$status" -eq 0 ]
  grep -A2 "id: CLI-INIT" .pact/STATE.yml | grep -q "status: shipped"
  git log --oneline | grep -q "Merge"
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `bats tests/merge.bats`
Expected: FAIL — `pact_merge` not defined.

- [ ] **Step 3: Implement `pact_merge`**

In `.pact/bin/pact.sh`, verbs section:

```bash
# pact_merge <feature_id> : orchestrator-only; requires all tasks accepted.
pact_merge() {
  _pact_require_id || return 1
  local feature="$1"
  local not_accepted
  not_accepted=$(_pact_project_json | jq -r --arg f "$feature" \
    '.features[] | select(.id==$f) | .tasks[] | select(.status!="accepted") | .id')
  if [ -n "$not_accepted" ]; then
    echo "pact_merge: cannot merge $feature; tasks not accepted: $not_accepted" >&2
    return 1
  fi
  local branch
  branch=$(_pact_project_json | jq -r --arg f "$feature" \
    '.features[] | select(.id==$f) | .branch')
  if [ -n "$branch" ] && [ "$branch" != "null" ]; then
    git merge --no-ff -m "Merge $feature ($branch)" "$branch" || {
      echo "pact_merge: git merge failed" >&2; return 1; }
  fi
  _pact_log_append merge orchestrator "" "$feature" '{}'
  _pact_render_state
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `bats tests/merge.bats`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add .pact/bin/pact.sh tests/merge.bats
git commit -m "feat: pact_merge enforces rule 2 + --no-ff merge"
```

---

## Task 9: `pact_status`, `pact_log`, `pact_validate`

**Files:**
- Modify: `.pact/bin/pact.sh`
- Create: `tests/status_validate.bats`

- [ ] **Step 1: Write failing tests**

Create `tests/status_validate.bats`:

```bash
#!/usr/bin/env bats
load helpers

bootstrap() {
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  pact_assign T1 --feature CLI-INIT --branch b --owner opencode --reviewer claude-opus
}

@test "status prints current STATE" {
  setup_pact_repo; bootstrap
  run pact_status
  [ "$status" -eq 0 ]
  [[ "$output" == *"project: pactify"* ]]
}

@test "log --replay rebuilds STATE identically (projection invariant)" {
  setup_pact_repo; bootstrap
  cp .pact/STATE.yml /tmp/before.yml
  echo "corrupted" > .pact/STATE.yml
  pact_log --replay
  diff /tmp/before.yml .pact/STATE.yml
}

@test "validate passes on a consistent repo" {
  setup_pact_repo; bootstrap
  run pact_validate
  [ "$status" -eq 0 ]
}

@test "validate fails when STATE is hand-edited (drift)" {
  setup_pact_repo; bootstrap
  echo "tampered" >> .pact/STATE.yml
  run pact_validate
  [ "$status" -ne 0 ]
  [[ "$output" == *"drift"* ]]
}

@test "validate fails when a log agent_id is not a declared seat" {
  setup_pact_repo; bootstrap
  export PACT_AGENT_ID=ghost
  _pact_log_append join worker "" "" '{"roles":["worker"]}'
  run pact_validate
  [ "$status" -ne 0 ]
  [[ "$output" == *"ghost"* ]]
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `bats tests/status_validate.bats`
Expected: FAIL — verbs not defined.

- [ ] **Step 3: Implement `pact_status`, `pact_log`, `pact_validate`**

In `.pact/bin/pact.sh`, verbs section:

```bash
pact_status() { cat "$PACT_STATE"; }

# pact_log [--replay]
pact_log() {
  if [ "${1:-}" = "--replay" ]; then
    _pact_render_state
  else
    cat "$PACT_LOG"
  fi
}

# pact_validate : check projection invariant + roster membership + slug + rule 1.
pact_validate() {
  local rc=0
  # 1) STATE must equal a fresh render of the log (no drift / hand-edits)
  local fresh; fresh=$(_pact_project_json)
  local current_render="$PACT_STATE.check"
  _pact_render_state_to "$current_render" "$fresh"
  if ! diff -q "$current_render" "$PACT_STATE" >/dev/null; then
    echo "pact_validate: STATE.yml drift vs render(log)" >&2; rc=1
  fi
  rm -f "$current_render"
  # 2) every agent_id in the log must be a declared seat
  local seats ids bad
  seats=$(jq -rs '(map(select(.event_type=="init"))|last).payload.seats[].id' "$PACT_LOG" | sort -u)
  ids=$(jq -r '.agent_id' "$PACT_LOG" | sort -u)
  bad=$(comm -23 <(echo "$ids") <(echo "$seats"))
  if [ -n "$bad" ]; then
    echo "pact_validate: log agent_id(s) not in seat roster: $bad" >&2; rc=1
  fi
  # 3) slug format for seat ids
  while read -r s; do
    [ -z "$s" ] && continue
    [[ "$s" =~ ^[a-z0-9][a-z0-9-]*$ ]] || { echo "pact_validate: bad seat slug: $s" >&2; rc=1; }
  done <<<"$seats"
  # 4) rule 1: no task has owner==reviewer
  local viol
  viol=$(echo "$fresh" | jq -r '.features[].tasks[] | select(.owner==.reviewer) | .id')
  if [ -n "$viol" ]; then
    echo "pact_validate: rule1 violation (owner==reviewer) in tasks: $viol" >&2; rc=1
  fi
  return $rc
}
```

Also refactor `_pact_render_state` to share a render-to-target helper. Replace the existing `_pact_render_state` body with a thin wrapper over `_pact_render_state_to`:

```bash
# _pact_render_state_to <target_file> [projection_json]
_pact_render_state_to() {
  local target="$1"; local j="${2:-$(_pact_project_json)}"
  {
    echo "project: $(jq -r '.project' <<<"$j")"
    echo "working_tree_holder: null"
    echo "agents:"
    jq -r '.agents[] | "  - { id: \(.id), roles: [\(.roles | join(", "))] }"' <<<"$j"
    echo "features:"
    jq -r '
      .features[] |
      "  - id: \(.id)",
      "    branch: \(.branch)",
      "    status: \(.status)",
      "    tasks:",
      ( .tasks[] |
        "      - id: \(.id)",
        "        owner: \(.owner)",
        "        status: \(.status)",
        "        reviewer: \(.reviewer)",
        "        spec: \(.spec)",
        "        evidence: \(.evidence // "null")"
      )' <<<"$j"
  } > "$target"
}

_pact_render_state() {
  local tmp="$PACT_STATE.tmp"
  _pact_render_state_to "$tmp" && mv "$tmp" "$PACT_STATE"
}
```

- [ ] **Step 4: Run — expect PASS (and re-run prior suites to confirm no regression)**

Run: `bats tests/status_validate.bats && bats tests/`
Expected: PASS — all suites green.

- [ ] **Step 5: Commit**

```bash
git add .pact/bin/pact.sh tests/status_validate.bats
git commit -m "feat: pact_status/log/validate + shared render helper"
```

---

## Task 10: `pact_help` — self-documenting protocol reference

**Files:**
- Modify: `.pact/bin/pact.sh`
- Create: `tests/help.bats`

This makes the tool portable: any agent (not just Claude) learns the protocol from `pact_help`.

- [ ] **Step 1: Write failing test**

Create `tests/help.bats`:

```bash
#!/usr/bin/env bats
load helpers

@test "help lists all verbs and the two rules" {
  setup_pact_repo
  run pact_help
  [ "$status" -eq 0 ]
  for v in pact_init pact_assign pact_join pact_checkpoint pact_accept pact_merge pact_status; do
    [[ "$output" == *"$v"* ]]
  done
  [[ "$output" == *"cannot self-accept"* ]]
  [[ "$output" == *"all its tasks are accepted"* ]]
}

@test "pact.sh --help works when executed directly" {
  setup_pact_repo
  run bash .pact/bin/pact.sh --help
  [ "$status" -eq 0 ]
  [[ "$output" == *"pact protocol"* ]]
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `bats tests/help.bats`
Expected: FAIL — `pact_help` not defined.

- [ ] **Step 3: Implement `pact_help` + direct-exec dispatch**

In `.pact/bin/pact.sh`, add the help function and a bottom dispatch block:

```bash
pact_help() {
  cat <<'EOF'
pact protocol — verb reference

Roles: orchestrator | worker | reviewer | human
Identity: export PACT_AGENT_ID=<seat> before any verb (fail-closed if unset).

  pact_init --project <name> --seat "<id>:<roles>:<entry>" ...   scaffold .pact/ + bake entries
  pact_join <id> --roles <r1,r2>                                 worker cold-start; pick up tasks
  pact_assign <task> --feature <f> --branch <b> --owner <id> --reviewer <id> [--spec <p>]
  pact_checkpoint <task> --evidence "<text>"                     worker → awaiting_review
  pact_accept <task>                                             reviewer → accepted
  pact_changes <task> --reason "<text>"                          reviewer → changes_requested
  pact_merge <feature>                                           orchestrator: --no-ff merge → shipped
  pact_status                                                    print STATE.yml
  pact_log [--replay]                                            print log | rebuild STATE from log
  pact_validate                                                  check projection + roster + rules

The two rules (the pact):
  1. A worker cannot self-accept. Only a task's reviewer may accept it (owner != reviewer).
  2. A feature cannot merge until all its tasks are accepted.
EOF
}

# Direct execution: `bash pact.sh --help`. When sourced, this block is skipped.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  case "${1:-}" in
    --help|help) pact_help;;
    *) echo "pact.sh: source me, or run 'bash pact.sh --help'"; exit 0;;
  esac
fi
```

- [ ] **Step 4: Run — expect PASS**

Run: `bats tests/help.bats`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add .pact/bin/pact.sh tests/help.bats
git commit -m "feat: pact_help self-doc + direct --help dispatch"
```

---

## Task 11: Thin Claude skill

**Files:**
- Create: `.claude/skills/pact/SKILL.md`
- Create: `tests/skill.bats`

The skill is a convenience skin. It must NOT contain protocol knowledge that non-Claude agents need — it points to `.pact/`.

- [ ] **Step 1: Write failing test (skill stays thin + points to .pact/)**

Create `tests/skill.bats`:

```bash
#!/usr/bin/env bats

SKILL="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)/.claude/skills/pact/SKILL.md"

@test "skill file exists with frontmatter" {
  [ -f "$SKILL" ]
  head -1 "$SKILL" | grep -q -- "---"
  grep -q "^name:" "$SKILL"
  grep -q "^description:" "$SKILL"
}

@test "skill points to .pact/ as source of truth, stays thin" {
  grep -q ".pact/PROJECT.md" "$SKILL"
  grep -q "pact_help" "$SKILL"
  # thinness guard: skill must be short (<= 60 lines); protocol lives in .pact/
  [ "$(wc -l < "$SKILL")" -le 60 ]
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `bats tests/skill.bats`
Expected: FAIL — SKILL.md missing.

- [ ] **Step 3: Write the skill**

Create `.claude/skills/pact/SKILL.md`:

```markdown
---
name: pact
description: Use when coordinating multi-agent work in a repo that has a .pact/ directory — playing orchestrator, worker, or reviewer roles via the pact protocol.
---

# pact — multi-agent coordination

This repo uses the **pact protocol**. The protocol itself lives in `.pact/` and is
agent-agnostic. This skill is only Claude's convenience layer — the source of truth is
`.pact/PROJECT.md` and `pact_help`.

## On start
```bash
source .pact/bin/pact.sh
pact_help          # verb reference + the two rules
pact_status        # current state
```
Your seat + role are declared in your entry file (CLAUDE.md). Export `PACT_AGENT_ID`
before any verb.

## Your job by role
- **orchestrator**: write `tasks/<id>.md` (spec + acceptance), then
  `pact_assign <id> --feature <f> --branch <b> --owner <w> --reviewer <you>`.
  When all tasks accepted: `pact_merge <feature>`.
- **worker**: `pact_join <seat> --roles worker`, read your assigned task, implement,
  then `pact_checkpoint <id> --evidence "<test/build output>"`.
- **reviewer**: read the diff + evidence, run verification, then `pact_accept <id>`
  or `pact_changes <id> --reason "..."`.

## The two rules (enforced by pact.sh)
1. A worker cannot self-accept — only the task's reviewer accepts.
2. A feature cannot merge until all its tasks are accepted.

## Recovery
If STATE looks wrong: `pact_log --replay` rebuilds it from the log (the source).
If a worker crashed mid-task: re-`pact_join` the same seat and resume.
```

- [ ] **Step 4: Run — expect PASS**

Run: `bats tests/skill.bats`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add .claude/skills/pact/SKILL.md tests/skill.bats
git commit -m "feat: thin Claude pact skill (points to .pact/ source of truth)"
```

---

## Task 12: Full-flow integration test (6 stages, single process)

**Files:**
- Create: `tests/integration_sixstage.bats`

Proves the whole protocol end-to-end by switching `PACT_AGENT_ID` to simulate seats, including the two crash/recovery checks from the Exit Gate. (The real cross-agent dogfood with opencode is the manual Exit Gate in Task 13.)

- [ ] **Step 1: Write the integration test**

Create `tests/integration_sixstage.bats`:

```bash
#!/usr/bin/env bats
load helpers

@test "six-stage flow: init -> assign -> join -> checkpoint -> accept -> merge" {
  setup_pact_repo
  # 1 create
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  # 2 orchestrate
  mkdir -p .pact/tasks; echo "# T1 spec" > .pact/tasks/T1.md
  git add -A && git commit -q -m "pact init + T1 spec"
  git checkout -q -b feat/cli-init
  echo build > out.txt && git add -A && git commit -q -m "impl T1"
  git checkout -q -
  pact_assign T1 --feature CLI-INIT --branch feat/cli-init \
    --owner opencode --reviewer claude-opus --spec .pact/tasks/T1.md
  # 3 cold-start (worker)
  export PACT_AGENT_ID=opencode
  pact_join opencode --roles worker
  [ "$(task_status T1)" = "in_progress" ]
  # 4 implement
  pact_checkpoint T1 --evidence "tests green, build ok"
  [ "$(task_status T1)" = "awaiting_review" ]
  # 5 review
  export PACT_AGENT_ID=claude-opus
  pact_accept T1
  [ "$(task_status T1)" = "accepted" ]
  # 6 merge
  pact_merge CLI-INIT
  grep -A2 "id: CLI-INIT" .pact/STATE.yml | grep -q "status: shipped"
  run pact_validate
  [ "$status" -eq 0 ]
}

@test "exit-gate: worker cannot self-accept" {
  setup_pact_repo
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  pact_assign T1 --feature F --branch b --owner opencode --reviewer claude-opus
  export PACT_AGENT_ID=opencode; pact_join opencode --roles worker
  pact_checkpoint T1 --evidence ok
  run pact_accept T1     # worker attempts self-accept
  [ "$status" -ne 0 ]
}

@test "exit-gate: crash mid-task — resume same seat from log" {
  setup_pact_repo
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  pact_assign T1 --feature F --branch b --owner opencode --reviewer claude-opus
  export PACT_AGENT_ID=opencode; pact_join opencode --roles worker
  # simulate crash: wipe the rendered STATE (projection lost), log survives
  rm -f .pact/STATE.yml
  # resume: re-source + replay rebuilds state, task still in_progress
  source .pact/bin/pact.sh
  pact_log --replay
  [ "$(task_status T1)" = "in_progress" ]
}
```

- [ ] **Step 2: Run — expect PASS (all three)**

Run: `bats tests/integration_sixstage.bats`
Expected: PASS (3 tests).

- [ ] **Step 3: Run the whole suite**

Run: `bats tests/`
Expected: PASS — every suite green.

- [ ] **Step 4: Commit**

```bash
git add tests/integration_sixstage.bats
git commit -m "test: six-stage integration + exit-gate invariants (self-accept, crash recovery)"
```

---

## Task 13: Manual Exit Gate — real cross-agent dogfood with opencode

**Files:**
- Create: `docs/superpowers/plans/phase0-exit-gate-checklist.md`

This is a guided manual validation, not an automated test. It runs the protocol across **two real agents** (Claude + opencode) to validate the actual claim: the human only says "start."

- [ ] **Step 1: Write the Exit Gate checklist**

Create `docs/superpowers/plans/phase0-exit-gate-checklist.md`:

```markdown
# Phase 0 Exit Gate — manual dogfood checklist

Goal: drive a real Phase 1 task (e.g. the Go `pactify init` command) through the pact
protocol with Claude (orchestrator+reviewer) and opencode (worker). Human only says "start."

## Setup (Claude, seat claude-opus)
- [ ] In a fresh feature branch, run `pact_init --project pactify \
      --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" --seat "opencode:worker:AGENTS.md"`
- [ ] Write `.pact/tasks/T1.md` (spec + acceptance) for the first Go CLI command
- [ ] `pact_assign T1 --feature <F> --branch <b> --owner opencode --reviewer claude-opus`

## Worker run (opencode) — the relay test
- [ ] Start opencode in the repo. Say only: "start."
- [ ] CONFIRM: opencode auto-reads AGENTS.md, runs pact_join, reads STATE, identifies T1
      as its task — WITHOUT the human pasting the task/context.
- [ ] opencode implements, then `pact_checkpoint T1 --evidence "<go test output>"`

## Review + merge (Claude)
- [ ] Claude reads the branch diff + evidence, runs verification, `pact_accept T1`
- [ ] `pact_merge <F>` — feature → shipped

## Exit Gate assertions (all must hold)
- [ ] Human never pasted task content / context / diff — only said "start"
- [ ] opencode bootstrapped purely from `git pull` + auto-read entry file
- [ ] Rule 1 held in practice (worker could not self-accept)
- [ ] One induced crash (kill opencode mid-task) recovered via re-join + resume

## Verdict
- [ ] PASS → proceed to Phase 1 (Go CLI). Record learnings into the M1.1 schema.
- [ ] FAIL → capture where the human had to intervene; pivot the protocol.
```

- [ ] **Step 2: Execute the dogfood with the user**

This step is interactive and requires the human to launch opencode. Walk through the checklist with the user. Do not mark Task 13 complete until the user confirms the verdict.

- [ ] **Step 3: Commit the checklist + verdict notes**

```bash
git add docs/superpowers/plans/phase0-exit-gate-checklist.md
git commit -m "docs: Phase 0 exit-gate dogfood checklist"
```

---

## Self-Review Notes

- **Spec coverage:** §1 deliverables → Tasks 3/11/13; §2 STATE/log schema + write-through → Tasks 1/2; §3 seven verbs + rules → Tasks 3–9; §4 seat identity + handshake + fail-closed → Tasks 1/3/4; §5 crash recovery (replay, resume) → Tasks 9/12; §6 knowledge distribution (PROJECT.md + pact_help portable, skill thin) → Tasks 3/10/11; §7 six-stage dogfood → Tasks 12/13; §8 out-of-scope respected (no Go CLI, no MCP/dashboard here).
- **Implementation refinement vs spec §2.3:** write-through is implemented as *re-render STATE from log* after each append (not incremental edit). This is strictly more faithful to "log is source," and validated by the `pact_log --replay` idempotence test (Task 9).
- **Naming consistency:** verbs `pact_init/assign/join/checkpoint/accept/changes/merge/status/log/validate/help`; helpers `_pact_now/_pact_require_id/_pact_log_append/_pact_project_json/_pact_render_state/_pact_render_state_to/_pact_task_field/_pact_task_feature` — used consistently across tasks.
```

#!/usr/bin/env bash
# pact.sh — pact protocol reference implementation (Phase 0).
# Sourced by agents. All state lives under $PACT_DIR.

PACT_DIR="${PACT_DIR:-.pact}"
PACT_LOG="$PACT_DIR/log.jsonl"
PACT_STATE="$PACT_DIR/STATE.yml"
PACT_TASKS="$PACT_DIR/tasks"

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
  local et="$1" role="$2" task="$3" feature="$4" payload
  payload="${5:-}"; [ -z "$payload" ] && payload="{}"
  local line
  line=$(jq -nc \
    --arg ts "$(_pact_now)" --arg id "$PACT_AGENT_ID" --arg role "$role" \
    --arg et "$et" --arg task "$task" --arg feature "$feature" \
    --argjson payload "$payload" \
    '{ts:$ts,agent_id:$id,role:$role,event_type:$et,task_id:$task,feature:$feature,payload:$payload}')
  printf '%s\n' "$line" >> "$PACT_LOG"
}

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
        "        evidence: \((.evidence // "null") | gsub("\n";" ") | gsub("\t";" "))"
      )' <<<"$j"
  } > "$target"
}

_pact_render_state() {
  local tmp="$PACT_STATE.tmp"
  _pact_render_state_to "$tmp" && mv "$tmp" "$PACT_STATE"
}

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

# Export projection helpers + their env so `bash -c` subshells can call them.
export PACT_DIR PACT_LOG PACT_STATE PACT_TASKS
export -f _pact_project_json _pact_render_state_to _pact_render_state

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
  # validate every seat before any side effect (no partial init)
  local vs vid vroles ventry vextra
  for vs in "${seats[@]}"; do
    IFS=':' read -r vid vroles ventry vextra <<<"$vs"
    if [ -z "$vid" ] || [ -z "$vroles" ] || [ -z "$ventry" ] || [ -n "$vextra" ]; then
      echo "pact_init: seat must be 'id:roles:entry' (exactly 3 non-empty fields): $vs" >&2
      return 1
    fi
    case "$ventry" in
      /*|*..*) echo "pact_init: seat entry must be a repo-relative path without '..': $ventry" >&2; return 1;;
    esac
  done
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
  if _pact_project_json | jq -e --arg t "$task" '[.features[].tasks[].id] | index($t) != null' >/dev/null; then
    echo "pact_assign: task $task already exists" >&2; return 1
  fi
  [ -n "$spec" ] || spec="$PACT_TASKS/$task.md"
  local payload
  payload=$(jq -nc --arg o "$owner" --arg r "$reviewer" --arg b "$branch" --arg s "$spec" \
    '{owner:$o, reviewer:$r, branch:$b, spec:$s}')
  _pact_log_append assign orchestrator "$task" "$feature" "$payload"
  _pact_render_state
}

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

# pact_accept <task_id> : reviewer-only.
pact_accept() {
  _pact_require_id || return 1
  local task="$1"
  local reviewer; reviewer=$(_pact_task_field "$task" reviewer)
  if [ "$reviewer" != "$PACT_AGENT_ID" ]; then
    echo "pact_accept: only the reviewer ($reviewer) may accept $task; you are $PACT_AGENT_ID" >&2
    return 1
  fi
  local st; st=$(_pact_task_field "$task" status)
  if [ "$st" != "awaiting_review" ]; then
    echo "pact_accept: $task is not awaiting_review (status: $st)" >&2
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
  local st; st=$(_pact_task_field "$task" status)
  if [ "$st" != "awaiting_review" ]; then
    echo "pact_changes: $task is not awaiting_review (status: $st)" >&2
    return 1
  fi
  local feature; feature=$(_pact_task_feature "$task")
  local payload; payload=$(jq -nc --arg r "$reason" '{reason:$r}')
  _pact_log_append changes_requested reviewer "$task" "$feature" "$payload"
  _pact_render_state
}

# pact_merge <feature_id> : orchestrator-only; requires all tasks accepted.
pact_merge() {
  _pact_require_id || return 1
  local feature="$1"
  if ! _pact_project_json | jq -e --arg f "$feature" '.features[]|select(.id==$f)|.tasks|length>0' >/dev/null; then
    echo "pact_merge: unknown feature $feature (or it has no tasks)" >&2
    return 1
  fi
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
  viol=$(jq -r '.features[].tasks[] | select(.owner==.reviewer) | .id' <<<"$fresh")
  if [ -n "$viol" ]; then
    echo "pact_validate: rule1 violation (owner==reviewer) in tasks: $viol" >&2; rc=1
  fi
  # 5) task ids must be globally unique across features
  local dup
  dup=$(jq -r '[.features[].tasks[].id] | group_by(.) | map(select(length>1)[0]) | .[]' <<<"$fresh")
  if [ -n "$dup" ]; then
    echo "pact_validate: duplicate task id(s) across features: $dup" >&2; rc=1
  fi
  return $rc
}

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

# _pact_render_project <name> <seats_json>
_pact_render_project() {
  local name="$1" seats_json="$2"
  local seats_md
  seats_md=$(jq -r '.[] | "- `\(.id)` — roles: \(.roles | join(", ")) — entry: \(.entry)"' <<<"$seats_json")
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
$seats_md

## Commands (source .pact/bin/pact.sh)
Run \`pact_help\` for the full verb reference.
EOF
}

# Direct execution: `bash pact.sh --help`. When sourced, this block is skipped.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  case "${1:-}" in
    --help|help) pact_help;;
    *) echo "pact.sh: source me, or run 'bash pact.sh --help'"; exit 0;;
  esac
fi

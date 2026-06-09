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

# Export projection helpers + their env so `bash -c` subshells can call them.
export PACT_DIR PACT_LOG PACT_STATE PACT_TASKS
export -f _pact_project_json _pact_render_state

pact_init() { echo "not yet implemented" >&2; return 1; }

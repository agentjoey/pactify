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

pact_init() { echo "not yet implemented" >&2; return 1; }

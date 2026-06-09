#!/usr/bin/env bats
load helpers

boot() {
  export PACT_AGENT_ID=claude-opus
  pact_init --project p --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
}

@test "v1: every event has a non-empty event_id" {
  setup_pact_repo; boot
  pact_assign T1 --feature F --branch b --owner opencode --reviewer claude-opus
  run jq -rs 'all(.[]; (.event_id // "") | length > 0)' .pact/log.jsonl
  [ "$output" = "true" ]
}

@test "v1: event_ids are unique" {
  setup_pact_repo; boot
  pact_assign T1 --feature F --branch b --owner opencode --reviewer claude-opus
  export PACT_AGENT_ID=opencode; pact_join opencode --roles worker
  local total uniq
  total=$(jq -rs 'length' .pact/log.jsonl)
  uniq=$(jq -rs '[.[].event_id] | unique | length' .pact/log.jsonl)
  [ "$total" -eq "$uniq" ]
  [ "$total" -ge 3 ]
}

@test "v1: init event carries protocol_version = 1" {
  setup_pact_repo; boot
  run jq -rs '(map(select(.event_type=="init"))|last).payload.protocol_version' .pact/log.jsonl
  [ "$output" = "1" ]
}

@test "v1: PACT_PROTOCOL_VERSION constant is exposed as 1" {
  setup_pact_repo
  source .pact/bin/pact.sh
  [ "$PACT_PROTOCOL_VERSION" = "1" ]
}

@test "v1: validate passes on a conformant repo" {
  setup_pact_repo; boot
  run pact_validate
  [ "$status" -eq 0 ]
}

@test "v1: validate fails closed when protocol_version exceeds supported" {
  setup_pact_repo; boot
  jq -c 'if .event_type=="init" then .payload.protocol_version=2 else . end' .pact/log.jsonl > .pact/log.tmp
  mv .pact/log.tmp .pact/log.jsonl
  run pact_validate
  [ "$status" -ne 0 ]
  [[ "$output" == *"protocol_version"* ]]
}

@test "v1: validate flags an event missing event_id" {
  setup_pact_repo; boot
  printf '%s\n' '{"ts":"2026-01-01T00:00:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"join","task_id":"","feature":"","payload":{"roles":["orchestrator"]}}' >> .pact/log.jsonl
  run pact_validate
  [ "$status" -ne 0 ]
  [[ "$output" == *"event_id"* ]]
}

@test "v1 forward-compat: unknown event_type is ignored by the projection" {
  setup_pact_repo; boot
  pact_assign T1 --feature F --branch b --owner opencode --reviewer claude-opus
  printf '%s\n' '{"event_id":"x1","ts":"2026-01-01T00:00:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"nudge","task_id":"T1","feature":"F","payload":{},"future_field":42}' >> .pact/log.jsonl
  run bash -c '_pact_project_json | jq -r ".features[0].tasks[0].status"'
  [ "$output" = "assigned" ]
}

@test "v1 forward-compat: log --replay still rebuilds STATE with an unknown event present" {
  setup_pact_repo; boot
  pact_assign T1 --feature F --branch b --owner opencode --reviewer claude-opus
  printf '%s\n' '{"event_id":"x2","ts":"2026-01-01T00:00:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"nudge","task_id":"","feature":"","payload":{}}' >> .pact/log.jsonl
  run pact_log --replay
  [ "$status" -eq 0 ]
  grep -q "project: p" .pact/STATE.yml
}

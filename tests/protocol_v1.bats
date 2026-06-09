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

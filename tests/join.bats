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

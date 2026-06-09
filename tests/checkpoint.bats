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

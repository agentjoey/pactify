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

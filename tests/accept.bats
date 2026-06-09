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

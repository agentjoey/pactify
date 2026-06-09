#!/usr/bin/env bats
load helpers

boot() {
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
}

@test "C1: accept on a non-awaiting task is rejected" {
  setup_pact_repo; boot
  pact_assign T1 --feature F --branch b --owner opencode --reviewer claude-opus
  # T1 is 'assigned' (never checkpointed)
  run pact_accept T1
  [ "$status" -ne 0 ]
  [[ "$output" == *"awaiting_review"* ]]
}

@test "C1: changes on a non-awaiting task is rejected" {
  setup_pact_repo; boot
  pact_assign T1 --feature F --branch b --owner opencode --reviewer claude-opus
  run pact_changes T1 --reason x
  [ "$status" -ne 0 ]
  [[ "$output" == *"awaiting_review"* ]]
}

@test "C2: assign rejects a duplicate task id" {
  setup_pact_repo; boot
  pact_assign T1 --feature A --branch b --owner opencode --reviewer claude-opus
  run pact_assign T1 --feature B --branch b --owner opencode --reviewer claude-opus
  [ "$status" -ne 0 ]
  [[ "$output" == *"already exists"* ]]
}

@test "I3: merge rejects an unknown feature" {
  setup_pact_repo; boot
  run pact_merge NOPE
  [ "$status" -ne 0 ]
  [[ "$output" == *"unknown feature"* ]]
}

@test "I4: init rejects a malformed seat (missing entry) with no partial state" {
  setup_pact_repo
  export PACT_AGENT_ID=claude-opus
  run pact_init --project x --seat "a:worker"
  [ "$status" -ne 0 ]
  [ ! -f .pact/PROJECT.md ]
  [ ! -s .pact/log.jsonl ] || [ ! -f .pact/log.jsonl ]
}

@test "I4: init rejects a seat entry with path traversal" {
  setup_pact_repo
  export PACT_AGENT_ID=claude-opus
  run pact_init --project x --seat "a:worker:../evil.md"
  [ "$status" -ne 0 ]
  [[ "$output" == *".."* ]]
}

@test "regression: full happy path still validates clean after fixes" {
  setup_pact_repo; boot
  pact_assign T1 --feature CLI --branch b --owner opencode --reviewer claude-opus
  export PACT_AGENT_ID=opencode; pact_join opencode --roles worker
  pact_checkpoint T1 --evidence ok
  export PACT_AGENT_ID=claude-opus; pact_accept T1
  run pact_validate
  [ "$status" -eq 0 ]
}

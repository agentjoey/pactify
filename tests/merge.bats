#!/usr/bin/env bats
load helpers

@test "merge rejected when a task is not accepted (rule 2)" {
  setup_pact_repo
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  pact_assign T1 --feature CLI-INIT --branch feat/cli-init \
    --owner opencode --reviewer claude-opus
  run pact_merge CLI-INIT
  [ "$status" -ne 0 ]
  [[ "$output" == *"not accepted"* ]]
}

@test "merge succeeds when all tasks accepted; feature -> shipped" {
  setup_pact_repo
  local base; base=$(git branch --show-current)
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  pact_assign T1 --feature CLI-INIT --branch feat/cli-init \
    --owner opencode --reviewer claude-opus
  # worker: join auto-creates+checkouts feat/cli-init; do work; checkpoint auto-commits
  export PACT_AGENT_ID=opencode
  pact_join opencode --roles worker
  [ "$(git branch --show-current)" = "feat/cli-init" ]
  echo hi > f.txt
  pact_checkpoint T1 --evidence "ok"
  # work is committed on the feature branch
  git cat-file -e "HEAD:f.txt"
  # reviewer accepts; merge auto-returns to base branch and merges
  export PACT_AGENT_ID=claude-opus; pact_accept T1
  run pact_merge CLI-INIT
  [ "$status" -eq 0 ]
  [ "$(git branch --show-current)" = "$base" ]
  grep -A2 "id: CLI-INIT" .pact/STATE.yml | grep -q "status: shipped"
  git log --oneline | grep -q "Merge"
  # the worker's file arrived on base via the merge
  [ -f f.txt ]
}

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
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  git add -A && git commit -q -m "pact init"
  # create a real branch with a commit so --no-ff has something to merge
  git checkout -q -b feat/cli-init
  echo hi > f.txt && git add f.txt && git commit -q -m "work"
  git checkout -q -
  pact_assign T1 --feature CLI-INIT --branch feat/cli-init \
    --owner opencode --reviewer claude-opus
  export PACT_AGENT_ID=opencode; pact_join opencode --roles worker
  pact_checkpoint T1 --evidence "ok"
  export PACT_AGENT_ID=claude-opus; pact_accept T1
  run pact_merge CLI-INIT
  [ "$status" -eq 0 ]
  grep -A2 "id: CLI-INIT" .pact/STATE.yml | grep -q "status: shipped"
  git log --oneline | grep -q "Merge"
}

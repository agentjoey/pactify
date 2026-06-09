#!/usr/bin/env bats
load helpers

@test "six-stage flow: init -> assign -> join -> checkpoint -> accept -> merge" {
  setup_pact_repo
  # 1 create
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  # 2 orchestrate
  mkdir -p .pact/tasks; echo "# T1 spec" > .pact/tasks/T1.md
  git add -A && git commit -q -m "pact init + T1 spec"
  git checkout -q -b feat/cli-init
  echo build > out.txt && git add -A && git commit -q -m "impl T1"
  git checkout -q -
  pact_assign T1 --feature CLI-INIT --branch feat/cli-init \
    --owner opencode --reviewer claude-opus --spec .pact/tasks/T1.md
  # 3 cold-start (worker)
  export PACT_AGENT_ID=opencode
  pact_join opencode --roles worker
  [ "$(task_status T1)" = "in_progress" ]
  # 4 implement
  pact_checkpoint T1 --evidence "tests green, build ok"
  [ "$(task_status T1)" = "awaiting_review" ]
  # 5 review
  export PACT_AGENT_ID=claude-opus
  pact_accept T1
  [ "$(task_status T1)" = "accepted" ]
  # 6 merge
  pact_merge CLI-INIT
  grep -A2 "id: CLI-INIT" .pact/STATE.yml | grep -q "status: shipped"
  run pact_validate
  [ "$status" -eq 0 ]
}

@test "exit-gate: worker cannot self-accept" {
  setup_pact_repo
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  pact_assign T1 --feature F --branch b --owner opencode --reviewer claude-opus
  export PACT_AGENT_ID=opencode; pact_join opencode --roles worker
  pact_checkpoint T1 --evidence ok
  run pact_accept T1     # worker attempts self-accept
  [ "$status" -ne 0 ]
}

@test "exit-gate: crash mid-task — resume same seat from log" {
  setup_pact_repo
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  pact_assign T1 --feature F --branch b --owner opencode --reviewer claude-opus
  export PACT_AGENT_ID=opencode; pact_join opencode --roles worker
  # simulate crash: wipe the rendered STATE (projection lost), log survives
  rm -f .pact/STATE.yml
  # resume: re-source + replay rebuilds state, task still in_progress
  source .pact/bin/pact.sh
  pact_log --replay
  [ "$(task_status T1)" = "in_progress" ]
}

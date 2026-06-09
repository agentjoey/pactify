#!/usr/bin/env bats
# F1: worker branch/commit discipline. pact_join checks out the feature branch;
# pact_checkpoint commits the worker's work; pact_merge returns to base.
load helpers

boot_assigned() {
  export PACT_AGENT_ID=claude-opus
  pact_init --project p --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  pact_assign T1 --feature F --branch feat/x --owner opencode --reviewer claude-opus
}

@test "F1: join creates and checks out the worker's feature branch" {
  setup_pact_repo; boot_assigned
  export PACT_AGENT_ID=opencode
  pact_join opencode --roles worker
  [ "$(git branch --show-current)" = "feat/x" ]
}

@test "F1: checkpoint commits the worker's work (clean tree, file in commit)" {
  setup_pact_repo; boot_assigned
  export PACT_AGENT_ID=opencode
  pact_join opencode --roles worker
  echo code > impl.txt
  pact_checkpoint T1 --evidence "ok"
  [ -z "$(git status --porcelain)" ]      # nothing left uncommitted
  git cat-file -e "HEAD:impl.txt"          # work is in the branch history
  [ "$(task_status T1)" = "awaiting_review" ]
}

@test "F1: re-join is idempotent and stays on the feature branch (crash resume)" {
  setup_pact_repo; boot_assigned
  export PACT_AGENT_ID=opencode
  pact_join opencode --roles worker
  [ "$(git branch --show-current)" = "feat/x" ]
  # a restarted worker re-sources and re-joins; branch already exists -> checkout it
  source .pact/bin/pact.sh
  pact_join opencode --roles worker
  [ "$(git branch --show-current)" = "feat/x" ]
}

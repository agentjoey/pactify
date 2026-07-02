#!/usr/bin/env bats
load helpers

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

setup() {
  BIN="$BATS_TEST_TMPDIR/pactify"
  go build -o "$BIN" "$REPO/cmd/pactify" || return 1
}

# fresh git repo with pact.sh available (for the bash side), cd into it
interop_repo() {
  cd "$BATS_TEST_TMPDIR"; rm -rf w && mkdir w && cd w
  git init -q; git config user.email t@t.t; git config user.name t
  mkdir -p .pact/bin; cp "$REPO/.pact/bin/pact.sh" .pact/bin/pact.sh
  echo x > base.txt; git add -A; git commit -q -m base
}

@test "interop: bash init+assign -> Go join/checkpoint/accept/merge; both validate; STATE byte-identical" {
  interop_repo
  source .pact/bin/pact.sh
  export PACT_AGENT_ID=claude-opus
  pact_init --project p --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" --seat "opencode:worker:AGENTS.md"
  pact_assign t1 --feature f --branch feat/x --owner opencode --reviewer claude-opus --spec .pact/tasks/t1.md
  export PACT_AGENT_ID=opencode; "$BIN" join opencode --roles worker
  echo code > impl.txt
  "$BIN" checkpoint t1 --evidence "ok"
  export PACT_AGENT_ID=claude-opus; "$BIN" accept t1; "$BIN" merge f
  grep -q "status: shipped" .pact/STATE.yml
  "$BIN" validate
  source .pact/bin/pact.sh && pact_validate
  cp .pact/STATE.yml /tmp/go_state.yml
  pact_log --replay
  diff /tmp/go_state.yml .pact/STATE.yml
}

@test "interop: Go init -> bash reads/continues; bash validate passes" {
  interop_repo
  export PACT_AGENT_ID=claude-opus
  "$BIN" init --project p --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" --seat "opencode:worker:AGENTS.md"
  "$BIN" assign t1 --feature f --branch feat/y --owner opencode --reviewer claude-opus --spec .pact/tasks/t1.md
  source .pact/bin/pact.sh
  export PACT_AGENT_ID=opencode; pact_join opencode --roles worker
  echo code > impl.txt
  pact_checkpoint t1 --evidence "ok"
  export PACT_AGENT_ID=claude-opus; pact_accept t1; pact_merge f
  grep -q "status: shipped" .pact/STATE.yml
  pact_validate
  "$BIN" validate
}

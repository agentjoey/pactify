#!/usr/bin/env bats
load helpers

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

setup() {
  BIN="$BATS_TEST_TMPDIR/pactify"
  go build -o "$BIN" "$REPO/cmd/pactify" || return 1
}

run_init() {
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify \
    --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
}

@test "init creates skeleton dirs and files" {
  setup_pact_repo
  run_init
  [ -f .pact/PROJECT.md ]
  [ -f .pact/log.jsonl ]
  [ -f .pact/STATE.yml ]
  [ -d .pact/tasks ]
}

@test "init writes one init event carrying project + seats" {
  setup_pact_repo
  run_init
  [ "$(log_lines)" -eq 1 ]
  run jq -r '.event_type' .pact/log.jsonl
  [ "$output" = "init" ]
  run jq -r '.payload.seats | length' .pact/log.jsonl
  [ "$output" = "2" ]
}

@test "init bakes entry files with seat id + join command" {
  setup_pact_repo
  run_init
  grep -q "PACT_AGENT_ID=opencode" AGENTS.md
  grep -q "pact_join opencode --roles worker" AGENTS.md
  grep -q "PACT_AGENT_ID=claude-opus" CLAUDE.md
}

@test "init renders STATE with project name and seat roster" {
  setup_pact_repo
  run_init
  grep -q "project: pactify" .pact/STATE.yml
  grep -q "id: opencode" .pact/STATE.yml
}

@test "init fails closed without PACT_AGENT_ID" {
  setup_pact_repo
  unset PACT_AGENT_ID
  run pact_init --project x --seat "a:worker:A.md"
  [ "$status" -eq 1 ]
}

@test "init renders PROJECT.md Seats section with each seat" {
  setup_pact_repo
  run_init
  grep -q '## Seats' .pact/PROJECT.md
  grep -q 'claude-opus' .pact/PROJECT.md
  grep -q 'roles: orchestrator, reviewer' .pact/PROJECT.md
  grep -q 'opencode' .pact/PROJECT.md
}

@test "init with 4-field seat wires the MCP kind" {
  cd "$BATS_TEST_TMPDIR"; rm -rf w && mkdir w && cd w
  git init -q; git config user.email t@t.t; git config user.name t
  echo x > base.txt; git add -A; git commit -q -m base
  export PACT_AGENT_ID=opencode
  "$BIN" init --project p --seat "opencode:worker:AGENTS.md:opencode"
  grep -q '"pact"' opencode.json
  grep -q 'seat `opencode`' AGENTS.md
}

@test "init rejects a kinded seat whose entry mismatches the kind default" {
  cd "$BATS_TEST_TMPDIR"; rm -rf w2 && mkdir w2 && cd w2
  git init -q; git config user.email t@t.t; git config user.name t
  echo x > base.txt; git add -A; git commit -q -m base
  export PACT_AGENT_ID=opencode
  run "$BIN" init --project p --seat "opencode:worker:DOCS.md:opencode"
  [ "$status" -ne 0 ]
  [[ "$output" == *"expects entry"* ]]
  [ ! -f DOCS.md ]
  [ ! -f opencode.json ]
}

@test "init rejects a desktop kind seat (use agent add instead)" {
  cd "$BATS_TEST_TMPDIR"; rm -rf w3 && mkdir w3 && cd w3
  git init -q; git config user.email t@t.t; git config user.name t
  echo x > base.txt; git add -A; git commit -q -m base
  export PACT_AGENT_ID=x
  run "$BIN" init --project p --seat "x:worker:SOMETHING.md:claude-desktop"
  [ "$status" -ne 0 ]
  [[ "$output" == *"agent add"* ]]
}

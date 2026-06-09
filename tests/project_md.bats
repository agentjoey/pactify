#!/usr/bin/env bats
load helpers

@test "PROJECT.md shows protocol_version and lists seats" {
  setup_pact_repo
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  grep -q "protocol_version: 1" .pact/PROJECT.md
  grep -q "claude-opus" .pact/PROJECT.md
  grep -q "pact:begin" .pact/PROJECT.md
}

@test "_pact_bake_project preserves user content outside the managed block" {
  setup_pact_repo
  source .pact/bin/pact.sh
  printf '# Charter\n\nMy own notes.\n' > .pact/PROJECT.md
  _pact_bake_project pactify '[{"id":"a","roles":["worker"],"entry":"A.md"}]' 1
  grep -q "My own notes." .pact/PROJECT.md
  grep -q "protocol_version: 1" .pact/PROJECT.md
  [ "$(grep -c 'pact:begin' .pact/PROJECT.md)" -eq 1 ]
  _pact_bake_project pactify '[{"id":"a","roles":["worker"],"entry":"A.md"}]' 1
  [ "$(grep -c 'pact:begin' .pact/PROJECT.md)" -eq 1 ]
  grep -q "My own notes." .pact/PROJECT.md
}

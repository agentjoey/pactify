#!/usr/bin/env bats
load helpers

bootstrap() {
  export PACT_AGENT_ID=claude-opus
  pact_init --project pactify --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  pact_assign T1 --feature CLI-INIT --branch b --owner opencode --reviewer claude-opus
}

@test "status prints current STATE" {
  setup_pact_repo; bootstrap
  run pact_status
  [ "$status" -eq 0 ]
  [[ "$output" == *"project: pactify"* ]]
}

@test "log --replay rebuilds STATE identically (projection invariant)" {
  setup_pact_repo; bootstrap
  cp .pact/STATE.yml /tmp/before.yml
  echo "corrupted" > .pact/STATE.yml
  pact_log --replay
  diff /tmp/before.yml .pact/STATE.yml
}

@test "validate passes on a consistent repo" {
  setup_pact_repo; bootstrap
  run pact_validate
  [ "$status" -eq 0 ]
}

@test "validate fails when STATE is hand-edited (drift)" {
  setup_pact_repo; bootstrap
  echo "tampered" >> .pact/STATE.yml
  run pact_validate
  [ "$status" -ne 0 ]
  [[ "$output" == *"drift"* ]]
}

@test "validate fails when a log agent_id is not a declared seat" {
  setup_pact_repo; bootstrap
  export PACT_AGENT_ID=ghost
  _pact_log_append join worker "" "" '{"roles":["worker"]}'
  run pact_validate
  [ "$status" -ne 0 ]
  [[ "$output" == *"ghost"* ]]
}

#!/usr/bin/env bats
load helpers

@test "_pact_now emits UTC ISO-8601" {
  setup_pact_repo
  run _pact_now
  [ "$status" -eq 0 ]
  [[ "$output" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]
}

@test "_pact_require_id fails closed when PACT_AGENT_ID unset" {
  setup_pact_repo
  unset PACT_AGENT_ID
  run _pact_require_id
  [ "$status" -eq 1 ]
  [[ "$output" == *"PACT_AGENT_ID not set"* ]]
}

@test "_pact_require_id passes when PACT_AGENT_ID set" {
  setup_pact_repo
  export PACT_AGENT_ID=claude-opus
  run _pact_require_id
  [ "$status" -eq 0 ]
}

@test "_pact_log_append writes one valid JSON line" {
  setup_pact_repo
  export PACT_AGENT_ID=claude-opus
  : > .pact/log.jsonl
  _pact_log_append join worker "" "" '{"roles":["worker"]}'
  [ "$(log_lines)" -eq 1 ]
  run jq -e '.agent_id=="claude-opus" and .event_type=="join" and .role=="worker"' .pact/log.jsonl
  [ "$status" -eq 0 ]
}

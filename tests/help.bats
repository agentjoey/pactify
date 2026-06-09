#!/usr/bin/env bats
load helpers

@test "help lists all verbs and the two rules" {
  setup_pact_repo
  run pact_help
  [ "$status" -eq 0 ]
  for v in pact_init pact_assign pact_join pact_checkpoint pact_accept pact_merge pact_status; do
    [[ "$output" == *"$v"* ]]
  done
  [[ "$output" == *"cannot self-accept"* ]]
  [[ "$output" == *"all its tasks are accepted"* ]]
}

@test "pact.sh --help works when executed directly" {
  setup_pact_repo
  run bash .pact/bin/pact.sh --help
  [ "$status" -eq 0 ]
  [[ "$output" == *"pact protocol"* ]]
}

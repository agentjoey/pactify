#!/usr/bin/env bats
load helpers

@test "harness: temp repo + pact.sh sourcing works" {
  setup_pact_repo
  [ -f .pact/bin/pact.sh ]
  declare -f pact_init >/dev/null
}

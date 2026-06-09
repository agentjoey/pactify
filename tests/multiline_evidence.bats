#!/usr/bin/env bats
# Regression: multi-line evidence (e.g. `go test -v` output) must not break
# STATE.yml rendering or pact_validate's jq checks. Found in the Phase 0 dogfood.
load helpers

setup_to_awaiting_multiline() {
  export PACT_AGENT_ID=claude-opus
  pact_init --project p --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  pact_assign T1 --feature F --branch b --owner opencode --reviewer claude-opus
  export PACT_AGENT_ID=opencode
  pact_join opencode --roles worker
  # multi-line evidence with newlines AND a tab (like real `go test -v` output)
  pact_checkpoint T1 --evidence "$(printf '=== RUN TestX\n--- PASS: TestX\nPASS\nok\tmod\t0.7s')"
}

@test "multi-line evidence: STATE.yml evidence stays on a single line" {
  setup_pact_repo; setup_to_awaiting_multiline
  # the evidence line exists and the file is not broken into stray lines
  grep -q "evidence: === RUN TestX" .pact/STATE.yml
  # exactly one line contains 'evidence:'
  [ "$(grep -c 'evidence:' .pact/STATE.yml)" -eq 1 ]
}

@test "multi-line evidence: validate runs cleanly with no jq parse errors" {
  setup_pact_repo; setup_to_awaiting_multiline
  export PACT_AGENT_ID=claude-opus
  run pact_validate
  [ "$status" -eq 0 ]
  # stderr/stdout must NOT contain a jq parse error (checks 4 & 5 silently failed before)
  [[ "$output" != *"parse error"* ]]
  [[ "$output" != *"control characters"* ]]
}

@test "multi-line evidence: validate still detects a real rule-1 violation (check actually runs)" {
  setup_pact_repo
  export PACT_AGENT_ID=claude-opus
  pact_init --project p --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  # craft a log with multi-line evidence AND an owner==reviewer task by hand,
  # to prove check 4 actually executes (not silently skipped) under newlines.
  _pact_log_append assign orchestrator T1 F \
    "$(jq -nc '{owner:"opencode",reviewer:"opencode",branch:"b",spec:"s"}')"
  export PACT_AGENT_ID=opencode
  _pact_log_append checkpoint worker T1 F \
    "$(jq -nc --arg e "$(printf 'line1\nline2')" '{evidence:$e}')"
  _pact_render_state
  export PACT_AGENT_ID=claude-opus
  run pact_validate
  [ "$status" -ne 0 ]
  [[ "$output" == *"rule1"* ]]
}

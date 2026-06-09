#!/usr/bin/env bats
load helpers

seed_log() {
  : > .pact/log.jsonl
  cat >> .pact/log.jsonl <<'EOF'
{"ts":"2026-06-09T10:00:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{"project":"pactify","seats":[{"id":"claude-opus","roles":["orchestrator","reviewer"],"entry":"CLAUDE.md"},{"id":"opencode","roles":["worker"],"entry":"AGENTS.md"}]}}
{"ts":"2026-06-09T10:01:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"assign","task_id":"T1","feature":"CLI-INIT","payload":{"owner":"opencode","reviewer":"claude-opus","branch":"feat/cli-init","spec":".pact/tasks/T1.md"}}
{"ts":"2026-06-09T10:02:00Z","agent_id":"opencode","role":"worker","event_type":"join","task_id":"","feature":"","payload":{"roles":["worker"]}}
{"ts":"2026-06-09T10:03:00Z","agent_id":"opencode","role":"worker","event_type":"checkpoint","task_id":"T1","feature":"CLI-INIT","payload":{"evidence":"237 tests green"}}
EOF
}

@test "projection: project + seats from init event" {
  setup_pact_repo
  seed_log
  run bash -c '_pact_project_json | jq -r ".project"'
  [ "$output" = "pactify" ]
  run bash -c '_pact_project_json | jq -r ".agents | length"'
  [ "$output" = "2" ]
}

@test "projection: join moves owned assigned task to in_progress, checkpoint to awaiting_review" {
  setup_pact_repo
  seed_log
  # After full seed (incl checkpoint) status is awaiting_review with evidence.
  run bash -c '_pact_project_json | jq -r ".features[0].tasks[0].status"'
  [ "$output" = "awaiting_review" ]
  run bash -c '_pact_project_json | jq -r ".features[0].tasks[0].evidence"'
  [ "$output" = "237 tests green" ]
  run bash -c '_pact_project_json | jq -r ".features[0].tasks[0].owner"'
  [ "$output" = "opencode" ]
}

@test "render_state writes valid YAML view that grep helpers can read" {
  setup_pact_repo
  seed_log
  _pact_render_state
  [ -f .pact/STATE.yml ]
  grep -q "project: pactify" .pact/STATE.yml
  [ "$(task_status T1)" = "awaiting_review" ]
}

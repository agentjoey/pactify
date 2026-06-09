#!/usr/bin/env bats

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

setup() {
  BIN="$BATS_TEST_TMPDIR/pactify"
  go build -o "$BIN" "$REPO/cmd/pactify" || return 1
  export PACTIFY_HOME="$BATS_TEST_TMPDIR/home"
  PROJ="$BATS_TEST_TMPDIR/proj"; mkdir -p "$PROJ/.pact"
  printf '%s\n' '{"event_id":"1","ts":"2026-01-01T00:00:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{"project":"demo","protocol_version":1,"seats":[{"id":"claude-opus","roles":["orchestrator"],"entry":"CLAUDE.md"}],"base_branch":"main"}}' > "$PROJ/.pact/log.jsonl"
}

@test "register + serve exposes the project over HTTP" {
  "$BIN" register "$PROJ" --name demo
  "$BIN" list | grep -q "demo"
  "$BIN" serve --addr 127.0.0.1:7799 &
  SERVE_PID=$!
  sleep 1
  run curl -s http://127.0.0.1:7799/api/projects
  kill $SERVE_PID 2>/dev/null || true
  [[ "$output" == *'"name":"demo"'* ]]
  [[ "$output" == *'"project":"demo"'* ]]
}

@test "serve renders the embedded dashboard at /" {
  "$BIN" register "$PROJ" --name demo
  "$BIN" serve --addr 127.0.0.1:7798 &
  SERVE_PID=$!
  sleep 1
  run curl -s http://127.0.0.1:7798/
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:7798/)
  kill $SERVE_PID 2>/dev/null || true
  [ "$STATUS" = "200" ]
  [[ "$output" == *'<div id="root">'* ]]
}

#!/usr/bin/env bats

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

setup() {
  BIN="$BATS_TEST_TMPDIR/pactify"
  go build -o "$BIN" "$REPO/cmd/pactify" || return 1
  PROJ="$BATS_TEST_TMPDIR/proj"; rm -rf "$PROJ"; mkdir -p "$PROJ/.pact"
  printf '%s\n' '{"event_id":"1","ts":"2026-01-01T00:00:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{"project":"demo","protocol_version":1,"seats":[{"id":"claude-opus","roles":["orchestrator"],"entry":"CLAUDE.md"}],"base_branch":"main"}}' > "$PROJ/.pact/log.jsonl"
}

@test "mcp --project roots the server at a repo from a different cwd" {
  cd "$BATS_TEST_TMPDIR"   # NOT inside $PROJ
  export PACT_AGENT_ID=claude-opus
  run bash -c '{ printf "%s\n%s\n%s\n" \
    "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2025-06-18\",\"capabilities\":{},\"clientInfo\":{\"name\":\"smoke\",\"version\":\"0\"}}}" \
    "{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}" \
    "{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"status\",\"arguments\":{}}}"; sleep 5; } \
    | perl -e '"'"'alarm 10; exec @ARGV'"'"' -- "'"$BIN"'" mcp --project "'"$PROJ"'"'
  [[ "$output" == *"project: demo"* ]]
}

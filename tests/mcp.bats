#!/usr/bin/env bats

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

setup() {
  BIN="$BATS_TEST_TMPDIR/pactify"
  go build -o "$BIN" "$REPO/cmd/pactify" || return 1
  WORK="$BATS_TEST_TMPDIR/work"; rm -rf "$WORK"; mkdir -p "$WORK/.pact"; cd "$WORK"
  printf '%s\n' '{"event_id":"1","ts":"2026-01-01T00:00:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{"project":"demo","protocol_version":1,"seats":[{"id":"claude-opus","roles":["orchestrator"],"entry":"CLAUDE.md"}],"base_branch":"main"}}' > .pact/log.jsonl
}

@test "pactify mcp answers initialize and lists pact tools over stdio" {
  export PACT_AGENT_ID=claude-opus
  # Keep stdin open (via sleep) so the server can write responses before EOF causes it to exit.
  # perl alarm enforces a 10s hard timeout in case the server hangs.
  run bash -c '{ printf "%s\n%s\n%s\n" \
    "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2025-06-18\",\"capabilities\":{},\"clientInfo\":{\"name\":\"smoke\",\"version\":\"0\"}}}" \
    "{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}" \
    "{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\",\"params\":{}}"; sleep 5; } \
    | perl -e '"'"'alarm 10; exec @ARGV'"'"' -- "'"$BIN"'" mcp'
  [[ "$output" == *'"pactify"'* ]]
  [[ "$output" == *'"checkpoint"'* ]]
  [[ "$output" == *'"merge"'* ]]
}

#!/usr/bin/env bats

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

setup() {
  BIN="$BATS_TEST_TMPDIR/pactify"
  go build -o "$BIN" "$REPO/cmd/pactify" || return 1
  WORK="$BATS_TEST_TMPDIR/work"; rm -rf "$WORK"; mkdir -p "$WORK"; cd "$WORK"
}

@test "agent add opencode wires opencode.json + AGENTS.md" {
  "$BIN" agent add opencode --id opencode --roles worker
  grep -q '"pact"' opencode.json
  grep -q '"type": "local"' opencode.json
  grep -q 'seat `opencode`' AGENTS.md
}

@test "agent add codex-cli is doc-only (no config written)" {
  "$BIN" agent add codex-cli --id codex --roles worker
  [ ! -f .codex/config.toml ]
  grep -q 'seat `codex`' AGENTS.md
}

@test "agent add --print writes nothing" {
  "$BIN" agent add opencode --id opencode --roles worker --print
  [ ! -f opencode.json ]
}

@test "agent add unknown kind errors" {
  run "$BIN" agent add nope --id x --roles worker
  [ "$status" -ne 0 ]
  [[ "$output" == *"unknown agent kind"* ]]
}

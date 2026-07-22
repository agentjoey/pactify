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
  grep -q 'pact protocol' AGENTS.md && grep -q 'pactify seat use' AGENTS.md
}

@test "agent add codex-cli is doc-only (no config written)" {
  "$BIN" agent add codex-cli --id codex --roles worker
  [ ! -f .codex/config.toml ]
  grep -q 'pact protocol' AGENTS.md && grep -q 'pactify seat use' AGENTS.md
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

@test "agent add errors when --id is missing" {
  run "$BIN" agent add opencode --roles worker
  [ "$status" -ne 0 ]
  [[ "$output" == *"--id is required"* ]]
  [ ! -f opencode.json ]
}

@test "agent add errors when --roles is missing" {
  run "$BIN" agent add opencode --id opencode
  [ "$status" -ne 0 ]
  [[ "$output" == *"--roles is required"* ]]
}

@test "agent add makes a relative --project absolute (desktop --print)" {
  run "$BIN" agent add claude-desktop --id h --roles worker --project ./somerepo --print
  [ "$status" -eq 0 ]
  [[ "$output" == *'"--project"'* ]]
  [[ "$output" != *'"./somerepo"'* ]]
  [[ "$output" == *"/somerepo"* ]]
}

@test "agent add desktop kind writes global config under HOME and confirms" {
  export HOME="$BATS_TEST_TMPDIR/home"; mkdir -p "$HOME"
  run "$BIN" agent add claude-desktop --id h --roles worker
  [ "$status" -eq 0 ]
  [[ "$output" == *"machine-global"* ]]
  [ -f "$HOME/Library/Application Support/Claude/claude_desktop_config.json" ]
  grep -q '"pact"' "$HOME/Library/Application Support/Claude/claude_desktop_config.json"
}

@test "agent add opencode confirms the written config" {
  run "$BIN" agent add opencode --id opencode --roles worker
  [ "$status" -eq 0 ]
  [[ "$output" == *"wrote opencode.json"* ]]
}

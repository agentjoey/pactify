#!/usr/bin/env bats

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

setup() {
  BIN="$BATS_TEST_TMPDIR/pactify"
  go build -o "$BIN" "$REPO/cmd/pactify" || return 1
  export PACTIFY_HOME="$BATS_TEST_TMPDIR/home"
  WORK="$BATS_TEST_TMPDIR/work"; rm -rf "$WORK"; mkdir -p "$WORK"; cd "$WORK"
}

@test "role set/bind survives a round trip and shows in list" {
  "$BIN" role set frontend --kind claude-code --model claude-opus-4-8 --fallback cheap
  "$BIN" role set cheap --kind kimi-cli
  "$BIN" role bind w2 frontend
  run "$BIN" role list
  [ "$status" -eq 0 ]
  [[ "$output" == *"frontend"* ]]
  [[ "$output" == *"claude-code"* ]]
  [[ "$output" == *"w2 -> frontend"* ]]
}

@test "binding an undefined role fails loudly" {
  run "$BIN" role bind w2 nope
  [ "$status" -ne 0 ]
  [[ "$output" == *"unknown role"* ]]
}

@test "two seats of the same kind can carry different models" {
  "$BIN" role set pro --kind opencode --model deepseek/deepseek-v4-pro
  "$BIN" role set cheap --kind opencode --model deepseek/deepseek-v4-flash
  "$BIN" role bind w1 pro
  "$BIN" role bind w2 cheap
  run "$BIN" role list
  [[ "$output" == *"deepseek-v4-pro"* ]]
  [[ "$output" == *"deepseek-v4-flash"* ]]
}

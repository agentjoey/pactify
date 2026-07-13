#!/usr/bin/env bats

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

setup() {
  BIN="$BATS_TEST_TMPDIR/pactify"
  go build -o "$BIN" "$REPO/cmd/pactify" || return 1
  WORK="$BATS_TEST_TMPDIR/work"; rm -rf "$WORK"; mkdir -p "$WORK"; cd "$WORK"
}

@test "doctor passes on a wired+valid repo and reports the mcp server check" {
  git init -q
  git config user.email t@t.t
  git config user.name t
  echo x > base.txt
  git add -A
  git commit -q -m base
  export PACT_AGENT_ID=x
  # BIN is built as $BATS_TEST_TMPDIR/pactify, so its dir on PATH gives checkMCP a
  # resolvable `pactify` binary.
  export PATH="$(dirname "$BIN"):$PATH"
  "$BIN" init --project p --seat 'x:worker:CLAUDE.md'
  run "$BIN" doctor
  # doctor's exit code also folds in per-vendor CLI readiness (claude/codex/…),
  # absent in CI and on any host without the full vendor toolchain — so asserting
  # exit 0 would silently require every vendor CLI to be installed. Assert instead
  # that the checks this test is about — a valid repo, agent wiring, and the mcp
  # server handshake — report healthy. This mirrors the Go doctor test, which
  # likewise tolerates the env-dependent exit and checks output, not status. The
  # empty-dir test below covers the non-zero exit path.
  [[ "$output" == *"✓ .pact/ valid"* ]]
  [[ "$output" == *"✓ agent wiring"* ]]
  [[ "$output" == *"✓ mcp server"* ]]
}

@test "doctor fails in an empty dir" {
  export PATH="$(dirname "$BIN"):$PATH"
  run "$BIN" doctor
  [ "$status" -ne 0 ]
}

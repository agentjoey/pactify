#!/usr/bin/env bats

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

# setup boots a scratch git repo, pact-inits two seats (claude-opus
# orchestrator,reviewer / opencode worker) acting as claude-opus, registers it,
# and serves it on a free port with --seat claude-opus. The scripted event
# sequence is: init (n=1) -> CLI join opencode (n=2) -> API assign t1 (n=3), so
# the comms timeline/replay endpoints have a known, ordered log to fold over.
setup() {
  BIN="$BATS_TEST_TMPDIR/pactify"
  go build -o "$BIN" "$REPO/cmd/pactify" || return 1
  export PACTIFY_HOME="$BATS_TEST_TMPDIR/home"; mkdir -p "$PACTIFY_HOME"
  PROJ="$BATS_TEST_TMPDIR/proj"; mkdir -p "$PROJ"
  (
    cd "$PROJ"
    git init -q
    git config user.email t@t.t
    git config user.name t
    echo base > base.txt
    git add -A && git commit -q -m base
  )
  export PACT_AGENT_ID=claude-opus
  (
    cd "$PROJ"
    "$BIN" init --project demo \
      --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
      --seat "opencode:worker:AGENTS.md" >/dev/null
  )
  "$BIN" register "$PROJ" --name demo
  # n=2: opencode joins over the CLI (a real worker join, appends to the log).
  (
    cd "$PROJ"
    export PACT_AGENT_ID=opencode
    "$BIN" join opencode --roles worker >/dev/null
  )
  PORT=7793
  "$BIN" serve --addr "127.0.0.1:$PORT" --seat claude-opus &
  SERVE_PID=$!
  sleep 1
  BASE="http://127.0.0.1:$PORT"
  # n=3: assign t1 over the API so the log carries a task+feature event.
  curl -s -o /dev/null -X POST "$BASE/api/projects/demo/verbs/assign" \
    -H 'content-type: application/json' \
    -d '{"task":"t1","feature":"F","branch":"feat/x","owner":"opencode","reviewer":"claude-opus","spec":".pact/tasks/t1.md"}'
}

teardown() {
  kill "$SERVE_PID" 2>/dev/null || true
}

@test "comms API: timeline total matches event count with 1-based n ordering" {
  run curl -s "$BASE/api/projects/demo/timeline"
  [ "$status" -eq 0 ]

  # total equals the number of returned events.
  total=$(jq '.total' <<<"$output")
  count=$(jq '.events | length' <<<"$output")
  [ "$total" -eq "$count" ]
  # at least the three scripted events (init, join, assign) are present.
  [ "$total" -ge 3 ]

  # n is 1-based and strictly increasing: first row n==1, last row n==total.
  first_n=$(jq '.events[0].n' <<<"$output")
  last_n=$(jq '.events[-1].n' <<<"$output")
  [ "$first_n" -eq 1 ]
  [ "$last_n" -eq "$total" ]

  # the first event is the init; the assign row carries task + feature.
  [ "$(jq -r '.events[0].type' <<<"$output")" = "init" ]
  assign_task=$(jq -r '.events[] | select(.type=="assign") | .task' <<<"$output")
  assign_feat=$(jq -r '.events[] | select(.type=="assign") | .feature' <<<"$output")
  [ "$assign_task" = "t1" ]
  [ "$assign_feat" = "F" ]
}

@test "comms API: state?at=1 shows only the init fold; plain state equals at=total" {
  # at=1 folds only the init event: project name is set + no features yet (the
  # join/assign that follow have not been applied at this prefix). These hold
  # regardless of the exact later event sequence.
  run curl -s "$BASE/api/projects/demo/state?at=1"
  [ "$status" -eq 0 ]
  [ "$(jq -r '.project' <<<"$output")" = "demo" ]
  [ "$(jq '.features | length' <<<"$output")" -eq 0 ]
  [ "$(jq '.awaiting_count' <<<"$output")" -eq 0 ]

  # the full (live) state, by contrast, has the assigned feature F.
  run curl -s "$BASE/api/projects/demo/state"
  [ "$(jq '.features | length' <<<"$output")" -eq 1 ]
  [ "$(jq -r '.features[0].id' <<<"$output")" = "F" ]

  # plain /state must be byte-identical to /state?at=<total> (clamp == live).
  total=$(curl -s "$BASE/api/projects/demo/timeline" | jq '.total')
  curl -s "$BASE/api/projects/demo/state" | jq -S . > "$BATS_TEST_TMPDIR/full.json"
  curl -s "$BASE/api/projects/demo/state?at=$total" | jq -S . > "$BATS_TEST_TMPDIR/at_total.json"
  diff "$BATS_TEST_TMPDIR/full.json" "$BATS_TEST_TMPDIR/at_total.json"
}

#!/usr/bin/env bats

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

# setup boots a scratch git repo, pact-Inits two seats (claude-opus
# orchestrator,reviewer / opencode worker) acting as claude-opus, registers it,
# and serves it on a free port with --seat claude-opus.
setup() {
  BIN="$BATS_TEST_TMPDIR/pactify"
  go build -o "$BIN" "$REPO/cmd/pactify" || return 1
  export PACTIFY_HOME="$BATS_TEST_TMPDIR/home"
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
  PORT=7791
  "$BIN" serve --addr "127.0.0.1:$PORT" --seat claude-opus &
  SERVE_PID=$!
  sleep 1
  BASE="http://127.0.0.1:$PORT"
}

teardown() {
  kill "$SERVE_PID" 2>/dev/null || true
}

@test "author API: assign -> checkpoint -> accept -> merge ships the feature" {
  # author the task spec + assign over the API.
  curl -s -X POST "$BASE/api/projects/demo/tasks" \
    -H 'content-type: application/json' \
    -d '{"id":"t1","spec_md":"# T1"}' >/dev/null
  run curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/api/projects/demo/verbs/assign" \
    -H 'content-type: application/json' \
    -d '{"task":"t1","feature":"f","branch":"feat/x","owner":"opencode","reviewer":"claude-opus","spec":".pact/tasks/t1.md"}'
  [ "$output" = "200" ]

  # worker joins + checkpoints directly on disk (the worker is a separate seat).
  (
    cd "$PROJ"
    export PACT_AGENT_ID=opencode
    "$BIN" join opencode --roles worker
    echo work > work.txt
    "$BIN" checkpoint t1 --evidence ok
  )

  # reviewer accepts + merges over the API.
  run curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/api/projects/demo/verbs/accept" \
    -H 'content-type: application/json' -d '{"task":"t1"}'
  [ "$output" = "200" ]
  run curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/api/projects/demo/verbs/merge" \
    -H 'content-type: application/json' -d '{"feature":"f"}'
  [ "$output" = "200" ]

  grep -q "status: shipped" "$PROJ/.pact/STATE.yml"
}

@test "author API: accept before checkpoint -> 422 verbatim engine error" {
  curl -s -X POST "$BASE/api/projects/demo/tasks" \
    -H 'content-type: application/json' -d '{"id":"t1","spec_md":"# T1"}' >/dev/null
  curl -s -X POST "$BASE/api/projects/demo/verbs/assign" \
    -H 'content-type: application/json' \
    -d '{"task":"t1","feature":"f","branch":"feat/x","owner":"opencode","reviewer":"claude-opus","spec":".pact/tasks/t1.md"}' >/dev/null

  # accept while the task is still assigned (not awaiting_review) -> engine rejects.
  STATUS=$(curl -s -o /tmp/serve_author_body -w "%{http_code}" -X POST "$BASE/api/projects/demo/verbs/accept" \
    -H 'content-type: application/json' -d '{"task":"t1"}')
  [ "$STATUS" = "422" ]
  run cat /tmp/serve_author_body
  [[ "$output" == *"awaiting_review"* ]]
}

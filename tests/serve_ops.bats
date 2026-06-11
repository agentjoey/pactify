#!/usr/bin/env bats

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

# setup boots a scratch git repo + pact init (two seats: claude-opus
# orchestrator,reviewer / opencode worker) acting as claude-opus, but does NOT
# pre-register it. The server boots with --seat claude-opus and an empty
# registry; PACTIFY_HOME points at a temp dir so the real ~/.pactify registry
# file is untouched (the registry package honors PACTIFY_HOME). The ops flow
# then registers the project + wires + joins entirely over the API/CLI.
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
  # NOT pre-registered — the registry starts empty and is populated over the API.
  PORT=7792
  "$BIN" serve --addr "127.0.0.1:$PORT" --seat claude-opus &
  SERVE_PID=$!
  sleep 1
  BASE="http://127.0.0.1:$PORT"
}

teardown() {
  kill "$SERVE_PID" 2>/dev/null || true
}

# registerDemo adds $PROJ to the live server over the API. A helper so each
# @test that needs a registered project can populate the empty registry.
registerDemo() {
  curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/api/registry" \
    -H 'content-type: application/json' \
    -d "{\"path\":\"$PROJ\",\"name\":\"demo\"}"
}

@test "ops API: runtime register -> wiring probe -> wire flips wired true" {
  # register the project at runtime (no server restart).
  run registerDemo
  [ "$output" = "200" ]

  # the registry now lists demo as valid.
  run curl -s "$BASE/api/registry"
  [[ "$output" == *'"name":"demo"'* ]]
  [[ "$output" == *'"valid":true'* ]]

  # wiring probe (shared with doctor): opencode is not wired yet.
  run curl -s "$BASE/api/projects/demo/wiring"
  [[ "$output" == *'"kind":"opencode"'* ]]
  [[ "$output" == *'"wired":false'* ]]

  # wire opencode over the API.
  run curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/api/projects/demo/wiring/opencode" \
    -H 'content-type: application/json' \
    -d '{"seat":"opencode","roles":"worker"}'
  [ "$output" = "200" ]

  # the probe now reports opencode wired. Pull the opencode row out so an
  # unrelated kind's wired:false can't satisfy the assertion.
  run curl -s "$BASE/api/projects/demo/wiring"
  python3 - "$output" <<'PY'
import json, sys
rows = json.loads(sys.argv[1])
oc = next(r for r in rows if r["kind"] == "opencode")
assert oc["wired"] is True, oc
PY
}

@test "ops API: CLI join surfaces pactify-cli client provenance on seats" {
  run registerDemo
  [ "$output" = "200" ]

  # CLI join stamps the worker seat with pactify-cli client provenance.
  (
    cd "$PROJ"
    export PACT_AGENT_ID=opencode
    "$BIN" join opencode --roles worker
  )

  # the seats endpoint surfaces the CLI client provenance for opencode.
  run curl -s "$BASE/api/projects/demo/seats"
  [[ "$output" == *'pactify-cli'* ]]
}

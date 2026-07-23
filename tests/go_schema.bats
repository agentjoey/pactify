#!/usr/bin/env bats

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

setup() {
  BIN="$BATS_TEST_TMPDIR/pactify"
  go build -o "$BIN" "$REPO/cmd/pactify" || return 1
  export PACTIFY_HOME="$BATS_TEST_TMPDIR/pactify-home"
  WORK="$BATS_TEST_TMPDIR/work"; rm -rf "$WORK"; mkdir -p "$WORK"; cd "$WORK"
  git init -q; git config user.email t@t.t; git config user.name t
  echo x > base.txt; git add -A; git commit -q -m base
}

@test "go-emitted log validates against schemas/event.schema.json" {
  export PACT_AGENT_ID=claude-opus
  "$BIN" init --project p --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" --seat "opencode:worker:AGENTS.md"
  "$BIN" assign t1 --feature f --branch feat/x --owner opencode --reviewer claude-opus --spec .pact/tasks/t1.md
  export PACT_AGENT_ID=opencode; "$BIN" join opencode --roles worker
  echo code > impl.txt
  "$BIN" checkpoint t1 --evidence "$(printf 'PASS\nok')"
  export PACT_AGENT_ID=claude-opus; "$BIN" accept t1; "$BIN" merge f
  run python3 - "$REPO/schemas/event.schema.json" .pact/log.jsonl <<'PY'
import json, sys, jsonschema
schema = json.load(open(sys.argv[1]))
errs = 0
for line in open(sys.argv[2]):
    line = line.strip()
    if not line: continue
    try: jsonschema.validate(json.loads(line), schema)
    except jsonschema.ValidationError as e: errs += 1; print("FAIL:", e.message)
sys.exit(1 if errs else 0)
PY
  [ "$status" -eq 0 ]
}

#!/usr/bin/env bats
load helpers

SCHEMAS="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)/schemas"

full_flow_log() {
  export PACT_AGENT_ID=claude-opus
  pact_init --project p --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  pact_assign T1 --feature F --branch feat/x --owner opencode --reviewer claude-opus
  export PACT_AGENT_ID=opencode; pact_join opencode --roles worker
  echo x > w.txt
  pact_checkpoint T1 --evidence "$(printf 'PASS\nok')"
  export PACT_AGENT_ID=claude-opus; pact_accept T1
  pact_merge F
}

@test "schema files are valid JSON" {
  for f in event seat task; do
    run jq -e . "$SCHEMAS/$f.schema.json"
    [ "$status" -eq 0 ]
  done
}

@test "every emitted event validates against event.schema.json" {
  setup_pact_repo; full_flow_log
  run python3 - "$SCHEMAS/event.schema.json" .pact/log.jsonl <<'PY'
import json, sys, jsonschema
schema = json.load(open(sys.argv[1]))
errs = 0
for line in open(sys.argv[2]):
    line = line.strip()
    if not line:
        continue
    try:
        jsonschema.validate(json.loads(line), schema)
    except jsonschema.ValidationError as e:
        errs += 1
        print("FAIL:", e.message)
sys.exit(1 if errs else 0)
PY
  [ "$status" -eq 0 ]
}

@test "init seats validate against seat.schema.json" {
  setup_pact_repo; full_flow_log
  run python3 - "$SCHEMAS/seat.schema.json" .pact/log.jsonl <<'PY'
import json, sys, jsonschema
schema = json.load(open(sys.argv[1]))
init = [json.loads(l) for l in open(sys.argv[2]) if l.strip() and json.loads(l)["event_type"]=="init"][-1]
errs = 0
for seat in init["payload"]["seats"]:
    try:
        jsonschema.validate(seat, schema)
    except jsonschema.ValidationError as e:
        errs += 1; print("FAIL:", e.message)
sys.exit(1 if errs else 0)
PY
  [ "$status" -eq 0 ]
}

@test "a task frontmatter object validates against task.schema.json" {
  setup_pact_repo
  run python3 - "$SCHEMAS/task.schema.json" <<'PY'
import json, sys, jsonschema
schema = json.load(open(sys.argv[1]))
good = {"id":"T1","feature":"F","owner":"opencode","reviewer":"claude-opus"}
jsonschema.validate(good, schema)
try:
    jsonschema.validate({"id":"T1"}, schema)
    print("FAIL: missing fields accepted"); sys.exit(1)
except jsonschema.ValidationError:
    pass
PY
  [ "$status" -eq 0 ]
}

@test "event.schema.json accepts an assign with optional payload.deps (string array)" {
  setup_pact_repo
  SCHEMA="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)/schemas/event.schema.json"
  run python3 - "$SCHEMA" <<'PY'
import json, sys, jsonschema
schema = json.load(open(sys.argv[1]))
good = {"event_id":"e1","ts":"2026-01-01T00:00:00Z","agent_id":"orch","role":"orchestrator",
        "event_type":"assign","task_id":"T2","feature":"F",
        "payload":{"owner":"w","reviewer":"rev","branch":"feat/x","spec":"s","deps":["T1"]}}
jsonschema.validate(good, schema)
PY
  [ "$status" -eq 0 ]
}

@test "event.schema.json rejects an assign whose payload.deps is not an array" {
  setup_pact_repo
  SCHEMA="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)/schemas/event.schema.json"
  run python3 - "$SCHEMA" <<'PY'
import json, sys, jsonschema
schema = json.load(open(sys.argv[1]))
bad = {"event_id":"e1","ts":"2026-01-01T00:00:00Z","agent_id":"orch","role":"orchestrator",
       "event_type":"assign","task_id":"T2","feature":"F",
       "payload":{"owner":"w","reviewer":"rev","branch":"feat/x","spec":"s","deps":"T1"}}
try:
    jsonschema.validate(bad, schema)
    print("FAIL: deps as string accepted"); sys.exit(1)
except jsonschema.ValidationError:
    sys.exit(0)
PY
  [ "$status" -eq 0 ]
}

@test "event.schema.json rejects a checkpoint missing required payload.evidence" {
  setup_pact_repo
  SCHEMA="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)/schemas/event.schema.json"
  run python3 - "$SCHEMA" <<'PY'
import json, sys, jsonschema
schema = json.load(open(sys.argv[1]))
bad = {"event_id":"e1","ts":"2026-01-01T00:00:00Z","agent_id":"opencode","role":"worker",
       "event_type":"checkpoint","task_id":"T1","feature":"F","payload":{}}
try:
    jsonschema.validate(bad, schema)
    print("FAIL: missing evidence accepted"); sys.exit(1)
except jsonschema.ValidationError:
    sys.exit(0)
PY
  [ "$status" -eq 0 ]
}

#!/usr/bin/env bats

SPEC="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)/docs/specs/pact-protocol.md"

@test "protocol spec exists and covers all v1 contract sections" {
  [ -f "$SPEC" ]
  grep -qi "protocol_version" "$SPEC"
  grep -q "event_id" "$SPEC"
  for et in init assign join checkpoint accept changes_requested merge; do
    grep -q "$et" "$SPEC"
  done
  grep -qi "cannot self-accept\|self-accept" "$SPEC"
  grep -qi "all its tasks are accepted\|all tasks.*accepted" "$SPEC"
  grep -qi "awaiting_review" "$SPEC"
  grep -qi "seat" "$SPEC"
  grep -qi "ignore unknown\|unknown field" "$SPEC"
  grep -q "schemas/event.schema.json" "$SPEC"
}

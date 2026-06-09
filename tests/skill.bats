#!/usr/bin/env bats

SKILL="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)/.claude/skills/pact/SKILL.md"

@test "skill file exists with frontmatter" {
  [ -f "$SKILL" ]
  head -1 "$SKILL" | grep -q -- "---"
  grep -q "^name:" "$SKILL"
  grep -q "^description:" "$SKILL"
}

@test "skill points to .pact/ as source of truth, stays thin" {
  grep -q ".pact/PROJECT.md" "$SKILL"
  grep -q "pact_help" "$SKILL"
  # thinness guard: skill must be short (<= 60 lines); protocol lives in .pact/
  [ "$(wc -l < "$SKILL")" -le 60 ]
}

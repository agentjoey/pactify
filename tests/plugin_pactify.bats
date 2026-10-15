#!/usr/bin/env bats

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"
P="$REPO/plugins/pactify"

setup() {
  export PACTIFY_HOME="$BATS_TEST_TMPDIR/pactify-home"
  export KIMI_CODE_HOME="$BATS_TEST_TMPDIR/kimi-home"
}

@test "marketplace lists the pactify plugin" {
  run python3 -c "import json;d=json.load(open('$REPO/.claude-plugin/marketplace.json'));p=[p for p in d['plugins'] if p['name']=='pactify'];assert len(p)==1;assert p[0]['source']=='./plugins/pactify'"
  [ "$status" -eq 0 ]
}

@test "plugin.json is valid and named pactify" {
  run python3 -c "import json;print(json.load(open('$P/.claude-plugin/plugin.json'))['name'])"
  [ "$status" -eq 0 ]
  [ "$output" = "pactify" ]
}

@test "forwarding agents use haiku and only Bash" {
  for h in codex kimi; do
    grep -q '^model: haiku$' "$P/agents/$h.md"
    grep -q '^tools: Bash$' "$P/agents/$h.md"
    grep -q "pactify sub run $h" "$P/agents/$h.md"
  done
}

@test "skills route large tasks and wait on the main thread" {
  for h in codex kimi; do
    grep -q '^name: '"$h"'$' "$P/skills/$h/SKILL.md"
    grep -q 'run_in_background' "$P/skills/$h/SKILL.md"
    grep -q 'pactify sub wait' "$P/skills/$h/SKILL.md"
  done
}

@test "session-start hook reports a missing pactify without failing the session" {
  run env PATH=/usr/bin:/bin bash "$P/hooks/check-pactify-sub.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"pactify CLI is not on PATH"* ]]
}

@test "session-start hook configuration invokes an executable script with a quoted plugin path" {
  run python3 -c "import json;h=json.load(open('$P/hooks/hooks.json'));s=h['hooks']['SessionStart'][0]['hooks'][0];assert s['type']=='command';assert s['command']=='\"\u0024{CLAUDE_PLUGIN_ROOT}/hooks/check-pactify-sub.sh\"'"
  [ "$status" -eq 0 ]
  [ -x "$P/hooks/check-pactify-sub.sh" ]
}

@test "session-start hook reports a CLI without sub and exits successfully" {
  mkdir "$BATS_TEST_TMPDIR/bin"
  cat > "$BATS_TEST_TMPDIR/bin/pactify" <<'CLI'
#!/usr/bin/env bash
printf '%s\n' "$*" > "$PACTIFY_HOOK_CALL"
exit 1
CLI
  chmod +x "$BATS_TEST_TMPDIR/bin/pactify"
  export PACTIFY_HOOK_CALL="$BATS_TEST_TMPDIR/call"
  run env PATH="$BATS_TEST_TMPDIR/bin:/usr/bin:/bin" "$P/hooks/check-pactify-sub.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"has no 'sub' command; upgrade pactify"* ]]
  [ "$(cat "$PACTIFY_HOOK_CALL")" = "sub --help" ]
}

@test "session-start hook silently accepts a CLI with sub" {
  mkdir "$BATS_TEST_TMPDIR/bin"
  cat > "$BATS_TEST_TMPDIR/bin/pactify" <<'CLI'
#!/usr/bin/env bash
printf '%s\n' "$*" > "$PACTIFY_HOOK_CALL"
echo 'help output must not leak into the session'
exit 0
CLI
  chmod +x "$BATS_TEST_TMPDIR/bin/pactify"
  export PACTIFY_HOOK_CALL="$BATS_TEST_TMPDIR/call"
  run env PATH="$BATS_TEST_TMPDIR/bin:/usr/bin:/bin" "$P/hooks/check-pactify-sub.sh"
  [ "$status" -eq 0 ]
  [ -z "$output" ]
  [ "$(cat "$PACTIFY_HOOK_CALL")" = "sub --help" ]
}

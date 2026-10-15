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
    # An explicit /<harness> invocation must delegate regardless of task size (Phase 0 smoke, 2026-10-15).
    grep -q 'invoked this skill explicitly' "$P/skills/$h/SKILL.md"
    # The background wait must outlive the default ~10-minute background limit (Phase 0: 5 of 26 runs).
    grep -q 'timeout: 7200000' "$P/skills/$h/SKILL.md"
    grep -q 'full\*\* test command' "$P/skills/$h/SKILL.md"
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

@test "kimi skill does not offer read-only (Phase 0 K1)" {
  grep -q 'not offered for kimi' "$P/skills/kimi/SKILL.md"
  ! grep -q 'run kimi --read-only --wait' "$P/skills/kimi/SKILL.md"
}

@test "delegate skill is self-contained and uses the default harness" {
  skill="$P/skills/delegate/SKILL.md"
  grep -q '^name: delegate$' "$skill"
  grep -q 'default.*codex' "$skill"
  grep -q '/delegate' "$skill"
  grep -q '/pactify:delegate' "$skill"
  grep -q 'invoked this skill explicitly' "$skill"
  grep -q 'always delegate' "$skill"
  grep -q 'pactify sub run --write --worktree --wait 0s --json' "$skill"
  ! grep -q 'pactify sub run codex' "$skill"
  grep -q 'run_in_background' "$skill"
  grep -q 'timeout: 7200000' "$skill"
  grep -q 'pactify sub wait' "$skill"
  # Brief-writing and result-review rules must stay identical to codex's.
  run python3 - "$P/skills/codex/SKILL.md" "$skill" <<'PY'
import pathlib, sys
codex, delegate = [pathlib.Path(p).read_text() for p in sys.argv[1:]]
assert codex.split('1. Write', 1)[1].split('2. Start', 1)[0] == delegate.split('1. Write', 1)[1].split('2. Start', 1)[0]
assert codex.split('3. When', 1)[1].split('4. To', 1)[0] == delegate.split('3. When', 1)[1].split('4. To', 1)[0]
PY
  [ "$status" -eq 0 ]
}

@test "kimi skill labels its experimental status and Phase 0 limitations" {
  run python3 - "$P/skills/kimi/SKILL.md" <<'PY'
import pathlib, sys
text = pathlib.Path(sys.argv[1]).read_text()
frontmatter, body = text.split('---')[1:3]
description = next(line for line in frontmatter.splitlines() if line.startswith('description:'))
for part in (description, body):
    for term in ('experimental', '6/10', 'read-only', 'never converged'):
        assert term in part.lower(), (term, part)
PY
  [ "$status" -eq 0 ]
}

@test "README documents default delegation and experimental kimi" {
  grep -q '/delegate' "$P/README.md"
  grep -q '/pactify:delegate' "$P/README.md"
  grep -q '默认.*codex' "$P/README.md"
  grep -q 'kimi.*实验性' "$P/README.md"
  grep -q '6/10' "$P/README.md"
}

@test "skills isolate large tasks and share the review apply discard resume workflow" {
  run python3 - "$P" <<'PY'
import pathlib, re, sys
root = pathlib.Path(sys.argv[1])
sections = []
for name in ('delegate', 'codex', 'kimi'):
    text = (root / 'skills' / name / 'SKILL.md').read_text()
    command = next(line.strip() for line in text.splitlines() if "<<'BRIEF'" in line)
    assert '--worktree' in command, (name, command)
    review = text.split('3. When', 1)[1].split('\nFor a quick', 1)[0]
    for term in ('git -C', 'diff', 'ls-files', 'pactify sub apply <jobId>', 'pactify sub discard <jobId>', '--resume', 'same worktree', 'HEAD'):
        assert term in review, (name, term)
    sections.append(review)
assert sections[0] == sections[1] == sections[2], 'review and rework rules differ'
PY
  [ "$status" -eq 0 ]
}

@test "all three skills embed the same complete delegation guide" {
  run python3 - "$P" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
blocks = []
for name in ('delegate', 'codex', 'kimi'):
    text = (root / 'skills' / name / 'SKILL.md').read_text()
    start, end = '<!-- BEGIN SHARED-DELEGATION -->', '<!-- END SHARED-DELEGATION -->'
    assert text.count(start) == text.count(end) == 1, name
    block = text.split(start)[1].split(end)[0]
    for term in ('1. Write', '2. Start', '3. When', '4. To', 'conversation context', 'frequent user interaction'):
        assert term in block, (name, term)
    blocks.append(block)
assert blocks[0] == blocks[1] == blocks[2], 'shared delegation guides differ'
PY
  [ "$status" -eq 0 ]
}

@test "skills provide a complete brief example within 25 lines" {
  run python3 - "$P" <<'PY'
import pathlib, re, sys, textwrap
root = pathlib.Path(sys.argv[1])
for name in ('delegate', 'codex', 'kimi'):
    text = (root / 'skills' / name / 'SKILL.md').read_text()
    brief = text.split('1. Write', 1)[1].split('2. Start', 1)[0]
    example = re.search(r'```text\n(.*?)\n *```', brief, re.S)
    assert example, (name, 'missing complete example')
    example = textwrap.dedent(example[1])
    assert len(example.splitlines()) <= 25, name
    headings = ('Background', 'Goals', 'Known constraints and special cases', 'General constraints', 'Acceptance commands')
    assert all(heading + ':' in example for heading in headings), name
    assert re.search(r'^1\. .+\n2\. .+', example, re.M), name
    for term in ('src/cache/', 'Do not edit existing tests to fit the new behavior', 'cannot infer',
                 '**full** test command', 'Do not spawn subagents or write report files',
                 'PACTIFY_HOME="$(mktemp -d)" go test ./... -count=1'):
        assert term in brief, (name, term)
PY
  [ "$status" -eq 0 ]
}

@test "skills require host validation before apply and give actionable rework instructions" {
  run python3 - "$P" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
for name in ('delegate', 'codex', 'kimi'):
    text = (root / 'skills' / name / 'SKILL.md').read_text()
    review = text.split('3. When', 1)[1].split('4. To', 1)[0]
    gates = ('diff <baseCommit>', 'main thread must personally rerun', 'full test suite',
             'real environment', 'independent reviewer', 'concurrency, permissions, or data deletion')
    for term in gates:
        assert term in review, (name, term)
        assert review.index(term) < review.index('pactify sub apply <jobId>'), (name, term)
    rework = text.split('4. To', 1)[1]
    for term in ('--resume --worktree', 'same worktree', 'Review findings:', 'Required changes:',
                 'Tests to add:', 'Rerun acceptance:', 'Repeat step 3', 'do not apply or discard'):
        assert term in rework, (name, term)
PY
  [ "$status" -eq 0 ]
}

@test "forwarders preserve isolation and leave validation and rework decisions to the host" {
  run python3 - "$P" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
for name in ('codex', 'kimi'):
    text = (root / 'agents' / (name + '.md')).read_text()
    for term in ('--worktree', '--cwd', 'same worktree', 'full test', 'real environment',
                 'independent review', 'apply', 'discard', 'Review findings', 'Tests to add'):
        assert term in text, (name, term)
readme = (root / 'README.md').read_text()
review = readme.split('## Reviewing a delegation', 1)[1]
for term in ('diff', '全量测试', '真实环境', '独立评审', '--resume --worktree', '同一个 worktree',
             'pactify sub apply <jobId>', 'pactify sub discard <jobId>', '评审发现', '补充测试'):
    assert term in review, term
for term in ('背景', '编号目标', '已知约束与特例', '通用约束', '验收命令', '口头上下文', '频繁交互', 'timeout: 7200000'):
    assert term in readme, term
PY
  [ "$status" -eq 0 ]
}

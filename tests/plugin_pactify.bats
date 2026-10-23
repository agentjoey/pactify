#!/usr/bin/env bats

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"
P="$REPO/plugins/pactify"

setup() {
  export PACTIFY_HOME="$BATS_TEST_TMPDIR/pactify-home"
  export KIMI_CODE_HOME="$BATS_TEST_TMPDIR/kimi-home"
}

@test "Claude Code only plugin ships readable executable POSIX shell checks without launchers" {
  [ -r "$P/bin/pactify-check" ]
  [ -x "$P/bin/pactify-check" ]
  run python3 - "$P" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
for path in root.rglob('*'):
    assert not path.is_symlink(), path
    if path.is_file():
        assert '\0' not in path.read_text(), path
for path in (root / 'bin/pactify-check', root / 'hooks/check-pactify-sub.sh'):
    text = path.read_text()
    assert text.startswith('#!/bin/sh\n'), path
    for forbidden in ('$(', '`', 'npx ', 'npm ', 'uvx ', 'eval '):
        assert forbidden not in text, (path, forbidden)
hook = (root / 'hooks/check-pactify-sub.sh').read_text()
assert '"${CLAUDE_PLUGIN_ROOT}/bin/pactify-check"' in hook
assert 'Claude Code only' in (root / 'README.md').read_text()
PY
  [ "$status" -eq 0 ]
  for script in "$P/bin/pactify-check" "$P/hooks/check-pactify-sub.sh"; do
    /bin/sh -n "$script"
  done
}

@test "pactify-check reports missing CLI and prints both installers without executing them" {
  mkdir "$BATS_TEST_TMPDIR/bin"
  for command in curl go sh; do
    cat > "$BATS_TEST_TMPDIR/bin/$command" <<'CLI'
#!/bin/sh
printf '%s\n' installer-executed >> "$PACTIFY_HOOK_CALL"
exit 99
CLI
    chmod +x "$BATS_TEST_TMPDIR/bin/$command"
  done
  export PACTIFY_HOOK_CALL="$BATS_TEST_TMPDIR/call"
  run env PATH="$BATS_TEST_TMPDIR/bin" "$P/bin/pactify-check"
  [ "$status" -eq 1 ]
  [[ "$output" == *"pactify CLI is not on PATH"* ]]
  [[ "$output" == *"curl -fsSL https://pactify.dev/install.sh | sh"* ]]
  [[ "$output" == *"go install github.com/agentjoey/pactify/cmd/pactify@latest"* ]]
  [ ! -e "$PACTIFY_HOOK_CALL" ]
}

@test "pactify-check prints version and checks sub without exposing help output" {
  mkdir "$BATS_TEST_TMPDIR/bin"
  cat > "$BATS_TEST_TMPDIR/bin/pactify" <<'CLI'
#!/bin/sh
printf '%s\n' "$*" >> "$PACTIFY_HOOK_CALL"
case "$*" in
  version) printf '%s\n' 'pactify test-version'; exit "${VERSION_EXIT:-0}" ;;
  'sub --help') printf '%s\n' help-must-not-leak; exit "${SUB_EXIT:-0}" ;;
  *) exit 99 ;;
esac
CLI
  chmod +x "$BATS_TEST_TMPDIR/bin/pactify"
  export PACTIFY_HOOK_CALL="$BATS_TEST_TMPDIR/call"
  run env PATH="$BATS_TEST_TMPDIR/bin" "$P/bin/pactify-check"
  [ "$status" -eq 0 ]
  [[ "$output" == *"pactify is on PATH"* ]]
  [[ "$output" == *"pactify test-version"* ]]
  [[ "$output" == *"pactify sub is available"* ]]
  [[ "$output" != *"help-must-not-leak"* ]]
  [ "$(cat "$PACTIFY_HOOK_CALL")" = "$(printf 'version\nsub --help')" ]

  run env PATH="$BATS_TEST_TMPDIR/bin" SUB_EXIT=1 "$P/bin/pactify-check"
  [ "$status" -eq 1 ]
  [[ "$output" == *"has no 'sub' command; upgrade pactify"* ]]

  run env PATH="$BATS_TEST_TMPDIR/bin" VERSION_EXIT=1 "$P/bin/pactify-check"
  [ "$status" -eq 1 ]
  [[ "$output" == *"pactify version failed"* ]]
}

@test "harness skills are renamed and delegation requires an explicit user request in all five identical guides" {
  run python3 - "$P" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
names = ('delegate', 'delegate-codex', 'delegate-kimi', 'delegate-agy', 'delegate-opencode', 'jobs')
assert {p.name for p in (root / 'skills').iterdir()} == set(names)
blocks = []
for name in names:
    text = (root / 'skills' / name / 'SKILL.md').read_text()
    frontmatter = text.split('---')[1]
    assert f'name: {name}\n' in frontmatter, name
    description = next(line for line in frontmatter.splitlines() if line.startswith('description:'))
    assert 'user explicitly asks' in description, (name, description)
    if name == 'jobs':
        continue
    assert 'user explicitly asks to delegate' in description, name
    block = text.split('<!-- BEGIN SHARED-DELEGATION -->')[1].split('<!-- END SHARED-DELEGATION -->')[0]
    for term in ('only when the user explicitly asks', 'pactify slash command', 'clear request to delegate', 'otherwise do the work directly'):
        assert term in block, (name, term)
    assert 'When deciding on your own' not in text, name
    blocks.append(block)
assert len(blocks) == 5 and len(set(blocks)) == 1
for path in (root / 'agents').glob('*.md'):
    assert 'user explicitly' in path.read_text(), path
PY
  [ "$status" -eq 0 ]
}

@test "agy is slash-command only and supported skills document actual permission boundaries" {
  run python3 - "$P" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
for name in ('agy', 'opencode'):
    skill = (root / 'skills' / f'delegate-{name}' / 'SKILL.md').read_text()
    assert ('disable-model-invocation: true\n' in skill.split('---')[1]) == (name == 'agy'), name
    for path in (root / 'skills' / f'delegate-{name}' / 'SKILL.md', root / 'agents' / f'{name}.md', root / 'README.md'):
        text = path.read_text().lower()
        if path.name != 'README.md':
            assert ('experimental' in text) == (name == 'agy'), path
        for term in ('no os sandbox', 'permission requests are approved by pactify', f'/delegate-{name}', 'only'):
            assert term in text, (path, term)
        if name == 'opencode':
            for term in ('out-of-workspace directory access for file tools is denied', 'shell is not confined'):
                assert term in text, (path, term)
for name in ('delegate', 'delegate-codex', 'delegate-kimi', 'delegate-opencode'):
    frontmatter = (root / 'skills' / name / 'SKILL.md').read_text().split('---')[1]
    assert 'disable-model-invocation: true' not in frontmatter, name
PY
  [ "$status" -eq 0 ]
}

@test "delegation guides recommend selecting the resume job explicitly" {
  run python3 - "$P" "$REPO/README.md" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
blocks = []
for path in (root / 'skills').glob('*/SKILL.md'):
    text = path.read_text()
    if '<!-- BEGIN SHARED-DELEGATION -->' in text:
        block = text.split('<!-- BEGIN SHARED-DELEGATION -->')[1].split('<!-- END SHARED-DELEGATION -->')[0]
        assert '--resume <job-id>' in block, path
        blocks.append(block)
assert len(blocks) == 5 and len(set(blocks)) == 1
jobs = (root / 'skills/jobs/SKILL.md').read_text()
assert '--resume <id>' in jobs
assert 'Resume is not' not in jobs
for path in (root / 'agents').glob('*.md'):
    assert '--resume <job-id>' in path.read_text(), path
for path in (root / 'README.md', pathlib.Path(sys.argv[2])):
    text = path.read_text()
    assert '--resume <job-id>' in text, path
    assert 'prefix' in text, path
PY
  [ "$status" -eq 0 ]
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

@test "renamed stable skills delegate explicit requests and wait on the main thread" {
  for h in codex kimi; do
    grep -q '^name: delegate-'"$h"'$' "$P/skills/delegate-$h/SKILL.md"
    grep -q 'run_in_background' "$P/skills/delegate-$h/SKILL.md"
    grep -q 'pactify sub wait' "$P/skills/delegate-$h/SKILL.md"
    # An explicit /delegate-<harness> invocation must delegate regardless of task size.
    grep -q 'invoked this skill explicitly' "$P/skills/delegate-$h/SKILL.md"
    # The background wait must outlive the default ~10-minute background limit so long-running jobs remain collectible.
    grep -q 'timeout: 7200000' "$P/skills/delegate-$h/SKILL.md"
    grep -q 'full\*\* test command' "$P/skills/delegate-$h/SKILL.md"
  done
}

@test "session-start hook reports a missing pactify without failing the session" {
  run env CLAUDE_PLUGIN_ROOT="$P" PATH=/usr/bin:/bin /bin/sh "$P/hooks/check-pactify-sub.sh"
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
printf '%s\n' "$*" >> "$PACTIFY_HOOK_CALL"
exit 1
CLI
  chmod +x "$BATS_TEST_TMPDIR/bin/pactify"
  export PACTIFY_HOOK_CALL="$BATS_TEST_TMPDIR/call"
  run env CLAUDE_PLUGIN_ROOT="$P" PATH="$BATS_TEST_TMPDIR/bin:/usr/bin:/bin" "$P/hooks/check-pactify-sub.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"has no 'sub' command; upgrade pactify"* ]]
  [ "$(cat "$PACTIFY_HOOK_CALL")" = "$(printf 'version\nsub --help')" ]
}

@test "session-start hook silently accepts a CLI with sub" {
  mkdir "$BATS_TEST_TMPDIR/bin"
  cat > "$BATS_TEST_TMPDIR/bin/pactify" <<'CLI'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$PACTIFY_HOOK_CALL"
case "$*" in
  version) echo 'pactify 0.13.0 (test, test)' ;;
  *) echo 'help output must not leak into the session' ;;
esac
exit 0
CLI
  chmod +x "$BATS_TEST_TMPDIR/bin/pactify"
  export PACTIFY_HOOK_CALL="$BATS_TEST_TMPDIR/call"
  run env CLAUDE_PLUGIN_ROOT="$P" PATH="$BATS_TEST_TMPDIR/bin:/usr/bin:/bin" "$P/hooks/check-pactify-sub.sh"
  [ "$status" -eq 0 ]
  [ -z "$output" ]
  [ "$(cat "$PACTIFY_HOOK_CALL")" = "$(printf 'version\nsub --help')" ]
}

@test "kimi skill offers read-only with a generated private policy" {
  grep -q 'run kimi --read-only --wait' "$P/skills/delegate-kimi/SKILL.md"
  grep -q 'tools_snapshot' "$P/skills/delegate-kimi/SKILL.md"
  grep -q -- '--with-user-config' "$P/skills/delegate-kimi/SKILL.md"
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
  run python3 - "$P/skills/delegate-codex/SKILL.md" "$skill" <<'PY'
import pathlib, sys
codex, delegate = [pathlib.Path(p).read_text() for p in sys.argv[1:]]
assert codex.split('1. Write', 1)[1].split('2. Start', 1)[0] == delegate.split('1. Write', 1)[1].split('2. Start', 1)[0]
assert codex.split('3. When', 1)[1].split('4. To', 1)[0] == delegate.split('3. When', 1)[1].split('4. To', 1)[0]
PY
  [ "$status" -eq 0 ]
}

@test "kimi skill documents access limitations without an experimental label" {
  run python3 - "$P/skills/delegate-kimi/SKILL.md" <<'PY'
import pathlib, sys
text = pathlib.Path(sys.argv[1]).read_text()
frontmatter, body = text.split('---')[1:3]
description = next(line for line in frontmatter.splitlines() if line.startswith('description:'))
for part in (description, body):
    assert 'experimental' not in part.lower(), part
    for term in ('no os sandbox for writes', 'permission requests are approved by pactify', 'read-only', 'tool allowlist'):
        assert term in part.lower(), (term, part)
PY
  [ "$status" -eq 0 ]
}

@test "README documents default delegation and kimi access limitations" {
  grep -q '/delegate' "$P/README.md"
  grep -q '/pactify:delegate' "$P/README.md"
  grep -q 'default.*codex' "$P/README.md"
  run python3 - "$P" <<'PYCODE'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
for path in (root / 'README.md', root / 'agents/kimi.md'):
    text = path.read_text().lower()
    assert 'kimi is experimental' not in text, path
    for term in ('kimi', 'read-only', 'no os sandbox for writes', 'permission requests are approved by pactify'):
        assert term in text, (path, term)
PYCODE
  [ "$status" -eq 0 ]
  grep -q 'results vary by model' "$P/README.md"
}

@test "skills isolate large tasks and share the review apply discard resume workflow" {
  run python3 - "$P" <<'PY'
import pathlib, re, sys
root = pathlib.Path(sys.argv[1])
sections = []
for name in ('delegate', 'delegate-codex', 'delegate-kimi'):
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
for name in ('delegate', 'delegate-codex', 'delegate-kimi'):
    text = (root / 'skills' / name / 'SKILL.md').read_text()
    start, end = '<!-- BEGIN SHARED-DELEGATION -->', '<!-- END SHARED-DELEGATION -->'
    assert text.count(start) == text.count(end) == 1, name
    block = text.split(start)[1].split(end)[0]
    for term in ('1. Write', '2. Start', '3. When', '4. To', 'explicitly asks', 'otherwise do the work directly'):
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
for name in ('delegate', 'delegate-codex', 'delegate-kimi'):
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
                 'go test ./...'):
        assert term in brief, (name, term)
PY
  [ "$status" -eq 0 ]
}

@test "skills require host validation before apply and give actionable rework instructions" {
  run python3 - "$P" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
for name in ('delegate', 'delegate-codex', 'delegate-kimi'):
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
for term in ('diff', 'full test suite', 'real environment', 'independent review', '--resume --worktree', 'same worktree',
             'pactify sub apply <jobId>', 'pactify sub discard <jobId>', 'Review findings', 'Tests to add'):
    assert term in review, term
for term in ('Background', 'numbered goals', 'Known constraints and special cases', 'General constraints', 'Acceptance commands', 'explicitly asks', 'otherwise do the work directly', 'timeout: 7200000'):
    assert term in readme, term
PY
  [ "$status" -eq 0 ]
}

@test "agy skill and forwarder preserve the shared guide and explicit permissions" {
  run python3 - "$P" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
text = (root / 'skills/delegate-agy/SKILL.md').read_text()
original = (root / 'skills/delegate-kimi/SKILL.md').read_text()
start, end = '<!-- BEGIN SHARED-DELEGATION -->', '<!-- END SHARED-DELEGATION -->'
assert text.split(start)[1].split(end)[0] == original.split(start)[1].split(end)[0]
for term in ('name: delegate-agy', 'experimental', 'results vary by model', 'invoked this skill explicitly', 'pactify sub run agy', '--read-only', '--write', '--worktree'):
    assert term in text, term
agent = (root / 'agents/agy.md').read_text()
for term in ('model: haiku', 'tools: Bash', 'pactify sub run agy', '--read-only', '--write', '--resume', '--worktree', '--cwd'):
    assert term in agent, term
for path in (root / 'README.md', root.parent.parent / 'README.md'):
    assert '/delegate-agy' in path.read_text(), path
PY
  [ "$status" -eq 0 ]
}

@test "agy documents restored read-only access and persistent protocol" {
  run python3 - "$P" <<'PYTEST'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
skill = (root / 'skills/delegate-agy/SKILL.md').read_text()
for term in ('--read-only', '--mode plan', 'PreToolUse', 'sandboxed=false', 'BypassSandbox', '--completion-followups'):
    assert term in skill, term
assert 'Always pass `--write`' not in skill
readme = (root / 'README.md').read_text()
for term in ('1.4.0', 'persistent stream-json', 'ignored files', 'quota.json', 'Network'):
    assert term in readme, term
PYTEST
  [ "$status" -eq 0 ]
}

@test "jobs skill exists with valid frontmatter" {
  run python3 - "$P/skills/jobs/SKILL.md" <<'PY'
import pathlib, re, sys
text = pathlib.Path(sys.argv[1]).read_text()
match = re.match(r'\A---\n(.*?)\n---\n', text, re.S)
assert match, 'missing YAML frontmatter'
fields = {}
for line in match[1].splitlines():
    key, value = line.split(':', 1)
    assert key not in fields, key
    fields[key] = value.strip()
assert fields['name'] == 'jobs'
# A plain single-line YAML scalar; reject syntax that needs quoting.
assert re.fullmatch(r'[A-Za-z][A-Za-z0-9 ,()./-]+', fields['description'])
PY
  [ "$status" -eq 0 ]
}

@test "jobs is read-only and never executes lifecycle commands" {
  skill="$P/skills/jobs/SKILL.md"
  grep -q 'read-only' "$skill"
  grep -q 'Never execute.*apply.*discard.*cancel' "$skill"
  grep -q 'suggestions only' "$skill"
  grep -q 'user or host' "$skill"
}

@test "jobs documents default all and single job invocations" {
  skill="$P/skills/jobs/SKILL.md"
  for invocation in '/jobs' '/pactify:jobs' '/jobs all' '/jobs <id>'; do
    grep -Fq "$invocation" "$skill"
  done
  for command in 'pactify sub status --json' 'pactify sub status --all --json' 'pactify sub status <id> --json'; do
    grep -Fq "$command" "$skill"
  done
}

@test "jobs bounds list output and preserves summary field semantics" {
  run python3 - "$P/skills/jobs/SKILL.md" <<'PY'
import pathlib, sys
text = pathlib.Path(sys.argv[1]).read_text()
for term in ('10 rows', '20 rows', '30 lines', 'running', 'queued', 'Markdown table',
             'Title', 'Harness', 'Status', 'Repo / mode', 'Tokens', 'Activity / attention',
             'activity.usagePartial', 'unknown', 'repo', 'runCwd', 'cwd', 'worktree.status',
             'attention.kind', 'createdAt', 'before returning tool output', 'Never paste raw JSON',
             'omitted', 'counts', 'No delegation jobs'):
    assert term in text, term
PY
  [ "$status" -eq 0 ]
}

@test "all delegation skills share the short title instruction and jobs uses titles without extraction" {
  run python3 - "$P" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
blocks = []
for name in ('delegate', 'delegate-codex', 'delegate-kimi', 'delegate-agy', 'delegate-opencode'):
    text = (root / 'skills' / name / 'SKILL.md').read_text()
    block = text.split('<!-- BEGIN SHARED-DELEGATION -->')[1].split('<!-- END SHARED-DELEGATION -->')[0]
    assert 'Write a short title on the first line' in block, name
    assert 'issue tracker' in block, name
    blocks.append(block)
assert len(set(blocks)) == 1, 'shared delegation guides differ'
jobs = (root / 'skills/jobs/SKILL.md').read_text()
assert "Title: the brief's first line, otherwise the id" in jobs
assert 'Title / issue' not in jobs and 'prefer `issue`' not in jobs
assert '--grep <issue id>' not in jobs
PY
  [ "$status" -eq 0 ]
}

@test "jobs next steps require review and host acceptance before apply" {
  run python3 - "$P/skills/jobs/SKILL.md" <<'PY'
import pathlib, sys
text = pathlib.Path(sys.argv[1]).read_text()
for term in ('SHARED-DELEGATION', 'worktree_pending', 'failed', 'warnings', 'worktree.owner',
             'complete diff', 'untracked files', 'personally rerun', 'full test suite',
             'independent review', 'pactify sub apply <id>', 'pactify sub discard <id>',
             'pactify sub cancel <id>', '--resume --worktree', 'original --cwd',
             'most recent', 'same worktree', 'Do not start concurrent', 'discarded or missing'):
    assert term in text, term
assert text.index('complete diff') < text.index('pactify sub apply <id>')
assert text.index('personally rerun') < text.index('pactify sub apply <id>')
PY
  [ "$status" -eq 0 ]
}

@test "jobs details include bounded result logs diff and resume links" {
  skill="$P/skills/jobs/SKILL.md"
  for term in 'pactify sub result <id>' 'pactify sub logs <id> --tail 20' 'changedFiles' 'addedLines' 'deletedLines' 'resumeOf' 'resumedBy' 'baseCommit' 'sessionId'; do
    grep -Fq "$term" "$skill"
  done
  grep -q 'Do not recursively fetch' "$skill"
}

@test "READMEs and plugin description advertise jobs and all harnesses" {
  for readme in "$P/README.md" "$REPO/README.md"; do
    for term in '/jobs' '/pactify:jobs' '/jobs all' '/jobs <id>' 'read-only'; do
      grep -Fq "$term" "$readme"
    done
  done
  run python3 - "$P/.claude-plugin/plugin.json" <<'PY'
import json, sys
description = json.load(open(sys.argv[1]))['description']
for term in ('codex', 'kimi', 'agy', 'opencode', '/jobs'):
    assert term in description, term
PY
  [ "$status" -eq 0 ]
}

@test "opencode plugin is supported and shares the explicit-request delegation contract" {
  run python3 - "$P" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
skill = (root / 'skills/delegate-opencode/SKILL.md').read_text()
agent = (root / 'agents/opencode.md').read_text()
reference = (root / 'skills/delegate-codex/SKILL.md').read_text()
def shared(text):
    return text.split('<!-- BEGIN SHARED-DELEGATION -->', 1)[1].split('<!-- END SHARED-DELEGATION -->', 1)[0]
assert shared(skill) == shared(reference)
assert 'agy and opencode run only' not in shared(skill)
assert 'agy runs only' in shared(skill)
for text in (skill, agent):
    assert 'experimental' not in text.lower()
    assert 'Run only via' not in text
    for term in ('no OS sandbox', 'pactify sub run opencode', '--resume', '--worktree', '--read-only', '--write', 'read-only'):
        assert term in text, (term, text)
assert 'model: haiku' in agent and 'tools: Bash' in agent
for path in (root / 'README.md', root.parents[1] / 'README.md', root.parents[1] / 'docs/sub-doctor.md'):
    assert 'opencode' in path.read_text()
PY
  [ "$status" -eq 0 ]
}

@test "agy read-only supports explicit write permission" {
  run python3 - "$P" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
skill = (root / 'skills/delegate-agy/SKILL.md').read_text()
assert 'Use `--read-only`' in skill
assert 'Use `--write` only with explicit permission' in skill
agent = (root / 'agents/agy.md').read_text()
assert 'use `--read-only`' in agent
assert 'Flags: always `--write`' not in agent
for path in (root / 'README.md', root.parent.parent / 'README.md'):
    text = path.read_text()
    assert 'agy --read-only' in text or 'Read-only access uses `--mode plan`' in text
    assert 'close read-only access and stop testing' not in text
PY
  [ "$status" -eq 0 ]
}

@test "opencode read-only uses plan and declares its sandbox boundary" {
  run python3 - "$P" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
skill = (root / 'skills/delegate-opencode/SKILL.md').read_text()
assert 'Use `--read-only` instead' in skill
assert 'readonly_violation' in skill and 'sandboxed=false' in skill
assert 'Always pass `--write`' not in skill
agent = (root / 'agents/opencode.md').read_text()
assert 'use `--read-only`' in agent
assert 'sandboxed=false' in agent and 'Flags: always `--write`' not in agent
for path in (root / 'README.md', root.parents[1] / 'README.md'):
    text = path.read_text()
    assert 'opencode serve' in text and 'sandboxed=false' in text
    assert 'readonly_violation' in text
PY
  [ "$status" -eq 0 ]
}

@test "agy documents managed hook opt-out and renamed plugin has a minor version bump" {
  run python3 - "$P" <<'PY'
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
assert tuple(map(int, json.loads((root / '.claude-plugin/plugin.json').read_text())['version'].split('.'))) >= (0, 3, 0)
for path in (root / 'agents/agy.md', root / 'skills/delegate-agy/SKILL.md'):
    text = path.read_text()
    assert 'managed hooks are not installed with --with-user-config' in text
    assert 'workspace hooks' in text
PY
  [ "$status" -eq 0 ]
}

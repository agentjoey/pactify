#!/usr/bin/env bats

@test "delegate supports aliases and shared guidance describes quota fallback" {
  run python3 - "$BATS_TEST_DIRNAME/../plugins/pactify" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
delegate = (root / 'skills/delegate/SKILL.md').read_text()
assert '/delegate @alias' in delegate
assert 'sub run @alias' in delegate
blocks = []
for path in (root / 'skills').glob('*/SKILL.md'):
    text = path.read_text()
    if '<!-- BEGIN SHARED-DELEGATION -->' in text:
        block = text.split('<!-- BEGIN SHARED-DELEGATION -->')[1].split('<!-- END SHARED-DELEGATION -->')[0]
        assert '/delegate @alias <task>' in block, path
        assert '--worktree-from' in block and 'fallback' in block, path
        blocks.append(block)
assert len(blocks) == 5 and len(set(blocks)) == 1
for path in (root / 'README.md', root.parent.parent / 'README.md'):
    assert 'docs/router.md' in path.read_text(), path
PY
  [ "$status" -eq 0 ]
}

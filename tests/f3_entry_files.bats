#!/usr/bin/env bats
# F3: pact_init must not clobber existing entry files, and must never write
# *through* a symlink (e.g. AGENTS.md -> CLAUDE.md).
load helpers

@test "F3: baking into an existing file preserves its content (managed block appended)" {
  setup_pact_repo
  printf '# My Project\nimportant content\n' > X.md
  source .pact/bin/pact.sh
  _pact_bake_entry myid worker X.md
  grep -q "important content" X.md
  grep -q "pact:begin" X.md
  grep -q "PACT_AGENT_ID=myid" X.md
}

@test "F3: re-baking replaces the block (idempotent, no duplication)" {
  setup_pact_repo
  printf 'pre-existing\n' > X.md
  source .pact/bin/pact.sh
  _pact_bake_entry myid worker X.md
  _pact_bake_entry myid worker X.md
  [ "$(grep -c 'pact:begin' X.md)" -eq 1 ]
  grep -q "pre-existing" X.md
}

@test "F3: pact_init does not write through a symlink or clobber its target" {
  setup_pact_repo
  printf '# Real CLAUDE\nhand-written context\n' > CLAUDE.md
  ln -s CLAUDE.md AGENTS.md           # AGENTS.md is a symlink -> CLAUDE.md
  export PACT_AGENT_ID=claude-opus
  pact_init --project p \
    --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" \
    --seat "opencode:worker:AGENTS.md"
  # CLAUDE.md kept its content + got the claude-opus block
  grep -q "hand-written context" CLAUDE.md
  grep -q "PACT_AGENT_ID=claude-opus" CLAUDE.md
  # AGENTS.md is now a real file (symlink replaced), with opencode's block
  [ ! -L AGENTS.md ]
  grep -q "PACT_AGENT_ID=opencode" AGENTS.md
  # the symlink write did NOT leak opencode's block into CLAUDE.md
  ! grep -q "PACT_AGENT_ID=opencode" CLAUDE.md
}

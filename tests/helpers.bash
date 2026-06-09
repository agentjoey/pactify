# tests/helpers.bash — shared bats helpers for pact.sh tests

# Absolute path to the pact.sh under test (repo-root relative).
PACT_SH="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)/.pact/bin/pact.sh"

# setup_pact_repo: make a throwaway git repo in BATS_TEST_TMPDIR, cd into it,
# and source pact.sh. All tests operate here, never the real repo.
setup_pact_repo() {
  cd "$BATS_TEST_TMPDIR" || return 1
  rm -rf work && mkdir work && cd work || return 1
  git init -q
  git config user.email t@t.t
  git config user.name t
  # Copy pact.sh into place so pact_init's self-copy has a source.
  mkdir -p .pact/bin
  cp "$PACT_SH" .pact/bin/pact.sh
  # Base commit so later branch ops aren't on an unborn HEAD.
  git add -A && git commit -q -m "base"
  source .pact/bin/pact.sh
}

# log_lines: number of events in the log.
log_lines() { wc -l < .pact/log.jsonl | tr -d ' '; }

# task_status <task_id>: read current status from rendered STATE via grep.
task_status() {
  grep -A4 "id: $1\$" .pact/STATE.yml | grep -m1 'status:' | awk '{print $2}'
}

#!/usr/bin/env bats

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

setup_file() {
  export PACTIFY_HELP_BIN="$BATS_FILE_TMPDIR/pactify"
  (cd "$REPO" && go build -o "$PACTIFY_HELP_BIN" ./cmd/pactify)
}

@test "help lists only router commands" {
  run "$PACTIFY_HELP_BIN" --help
  [ "$status" -eq 0 ]
  commands="$(printf '%s\n' "$output" | sed -n '/^Available Commands:/,/^Flags:/p' | awk '/^  [a-z]/ {print $1}')"
  [ "$commands" = "completion
help
serve
sub
version" ]
}

@test "retired join command and serve seat flag are rejected" {
  run "$PACTIFY_HELP_BIN" join
  [ "$status" -ne 0 ]
  [[ "$output" == *'unknown command "join"'* ]]
  run "$PACTIFY_HELP_BIN" serve --seat x
  [ "$status" -ne 0 ]
  [[ "$output" == *'unknown flag: --seat'* ]]
}

@test "version and sub help work" {
  run "$PACTIFY_HELP_BIN" --version
  [ "$status" -eq 0 ]
  [[ "$output" == pactify* ]]
  run "$PACTIFY_HELP_BIN" sub --help
  [ "$status" -eq 0 ]
  [[ "$output" == *run* ]]
  [[ "$output" == *doctor* ]]
  [[ "$output" == *dismiss* ]]
}

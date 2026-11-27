#!/usr/bin/env bats

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

@test "build-local builds a runnable binary and signs it on macOS" {
  out="$BATS_TEST_TMPDIR/pactify"
  run "$REPO/scripts/build-local.sh" --no-web "$out"
  [ "$status" -eq 0 ]
  [[ "$output" == *"built $out" ]]
  run "$out" version
  [ "$status" -eq 0 ]
  if [ "$(uname -s)" = Darwin ]; then
    run codesign --verify "$out"
    [ "$status" -eq 0 ]
    # go build leaves only a provisional linker signature; require a real one.
    run codesign -dv "$out"
    [[ "$output" == *"flags=0x2(adhoc)"* ]]
  fi
}

@test "build-local resolves a relative output path" {
  cd "$BATS_TEST_TMPDIR"
  run "$REPO/scripts/build-local.sh" --no-web rel-pactify
  [ "$status" -eq 0 ]
  [ -x "$BATS_TEST_TMPDIR/rel-pactify" ]
}

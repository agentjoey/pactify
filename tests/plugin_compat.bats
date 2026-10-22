#!/usr/bin/env bats

setup() {
  REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  mkdir "$BATS_TEST_TMPDIR/bin"
  cat > "$BATS_TEST_TMPDIR/bin/pactify" <<'CLI'
#!/bin/sh
case "$*" in
  version) printf '%s\n' "pactify $STUB_VERSION"; exit "${STUB_EXIT:-0}" ;;
  'sub --help') exit 0 ;;
  *) exit 99 ;;
esac
CLI
  chmod +x "$BATS_TEST_TMPDIR/bin/pactify"
}

check_version() {
  for checker in "$REPO/plugins/pactify/bin/pactify-check" "$REPO/plugins/agents-skills/delegate/scripts/pactify-check"; do
    run env PATH="$BATS_TEST_TMPDIR/bin" STUB_VERSION="$1" /bin/sh "$checker" --quiet
    [ "$status" -eq "$2" ]
    if [ -n "$3" ]; then
      [[ "$output" == *"$3"* ]]
    else
      [ -z "$output" ]
    fi
  done
}

@test "plugin check rejects CLI older than its minimum with an upgrade hint" {
  check_version 0.12.9 1 'upgrade pactify to >= 0.13.0'
}

@test "plugin check accepts equal version and optional leading v or build metadata" {
  check_version 0.13.0 0 ''
  check_version v0.13.0+build.42 0 ''
}

@test "plugin check compares numeric components for newer CLI versions" {
  check_version 0.14.0 0 ''
  check_version 0.100.0 0 ''
  check_version 1.0.0 0 ''
}

@test "plugin check warns for dev and unrecognized versions without failing" {
  check_version dev 0 'version is unknown'
  check_version broken 0 'version is unknown'
}

@test "plugin check preserves version command failures even with valid version output" {
  for checker in "$REPO/plugins/pactify/bin/pactify-check" "$REPO/plugins/agents-skills/delegate/scripts/pactify-check"; do
    run env PATH="$BATS_TEST_TMPDIR/bin" STUB_VERSION=0.13.0 STUB_EXIT=1 /bin/sh "$checker" --quiet
    [ "$status" -eq 1 ]
    [[ "$output" == *'pactify version failed'* ]]
  done
}

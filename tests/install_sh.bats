#!/usr/bin/env bats

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

@test "install.sh detect_arch maps common arches" {
  run sh -c ". '$REPO/install.sh' --source-only; detect_arch x86_64"
  [ "$status" -eq 0 ]; [ "$output" = "amd64" ]
  run sh -c ". '$REPO/install.sh' --source-only; detect_arch aarch64"
  [ "$status" -eq 0 ]; [ "$output" = "arm64" ]
  run sh -c ". '$REPO/install.sh' --source-only; detect_arch arm64"
  [ "$status" -eq 0 ]; [ "$output" = "arm64" ]
}

@test "install.sh installs from a local download base into a temp bindir" {
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"; case "$arch" in x86_64) arch=amd64;; aarch64|arm64) arch=arm64;; esac
  base="$BATS_TEST_TMPDIR/dl"; mkdir -p "$base"
  printf '#!/bin/sh\necho fake-pactify\n' > "$BATS_TEST_TMPDIR/pactify"; chmod +x "$BATS_TEST_TMPDIR/pactify"
  ( cd "$BATS_TEST_TMPDIR" && tar czf "$base/pactify_${os}_${arch}.tar.gz" pactify )
  ( cd "$base" && shasum -a 256 "pactify_${os}_${arch}.tar.gz" > checksums.txt )
  bindir="$BATS_TEST_TMPDIR/bin"; mkdir -p "$bindir"
  run env PACTIFY_DOWNLOAD_BASE="file://$base" PACTIFY_BINDIR="$bindir" sh "$REPO/install.sh"
  [ "$status" -eq 0 ]
  [ -x "$bindir/pactify" ]
  run "$bindir/pactify"
  [ "$output" = "fake-pactify" ]
}

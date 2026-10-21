#!/usr/bin/env bats

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

@test "install.sh detect_arch maps common arches" {
  # env-var form: POSIX `.` does not portably pass args (dash ignores them)
  run sh -c "PACTIFY_SOURCE_ONLY=1; export PACTIFY_SOURCE_ONLY; . '$REPO/install.sh'; detect_arch x86_64"
  [ "$status" -eq 0 ]; [ "$output" = "amd64" ]
  run sh -c "PACTIFY_SOURCE_ONLY=1; export PACTIFY_SOURCE_ONLY; . '$REPO/install.sh'; detect_arch aarch64"
  [ "$status" -eq 0 ]; [ "$output" = "arm64" ]
  run sh -c "PACTIFY_SOURCE_ONLY=1; export PACTIFY_SOURCE_ONLY; . '$REPO/install.sh'; detect_arch arm64"
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
  [[ "$output" == *'pactify sub --help'* ]]
  [ -x "$bindir/pactify" ]
  run "$bindir/pactify"
  [ "$output" = "fake-pactify" ]
}

@test "install.sh rejects a checksum mismatch and installs nothing" {
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"; case "$arch" in x86_64) arch=amd64;; aarch64|arm64) arch=arm64;; esac
  base="$BATS_TEST_TMPDIR/dl2"; mkdir -p "$base"
  printf '#!/bin/sh\necho fake-pactify\n' > "$BATS_TEST_TMPDIR/pactify"; chmod +x "$BATS_TEST_TMPDIR/pactify"
  ( cd "$BATS_TEST_TMPDIR" && tar czf "$base/pactify_${os}_${arch}.tar.gz" pactify )
  bogus="$(printf '0%.0s' 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30 31 32 33 34 35 36 37 38 39 40 41 42 43 44 45 46 47 48 49 50 51 52 53 54 55 56 57 58 59 60 61 62 63 64)"
  printf '%s  %s\n' "$bogus" "pactify_${os}_${arch}.tar.gz" > "$base/checksums.txt"
  bindir="$BATS_TEST_TMPDIR/bin2"; mkdir -p "$bindir"
  run env PACTIFY_DOWNLOAD_BASE="file://$base" PACTIFY_BINDIR="$bindir" sh "$REPO/install.sh"
  [ "$status" -ne 0 ]
  [ ! -f "$bindir/pactify" ]
}

stub_release_downloads() {
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"; case "$arch" in x86_64) arch=amd64;; aarch64|arm64) arch=arm64;; esac
  export DOWNLOAD_FIXTURE="$BATS_TEST_TMPDIR/release"
  export CURL_LOG="$BATS_TEST_TMPDIR/curl.log"
  export PACTIFY_BINDIR="$BATS_TEST_TMPDIR/installed"
  mkdir -p "$DOWNLOAD_FIXTURE" "$BATS_TEST_TMPDIR/stubs"
  printf '#!/bin/sh\necho fake-pactify\n' > "$BATS_TEST_TMPDIR/pactify"
  (cd "$BATS_TEST_TMPDIR" && tar czf "$DOWNLOAD_FIXTURE/pactify_${os}_${arch}.tar.gz" pactify)
  (cd "$DOWNLOAD_FIXTURE" && shasum -a 256 "pactify_${os}_${arch}.tar.gz" > checksums.txt)
  cat > "$BATS_TEST_TMPDIR/stubs/curl" <<'STUB'
#!/bin/sh
set -eu
[ "$#" -eq 4 ] && [ "$1" = -fsSL ] && [ "$3" = -o ] || exit 90
printf '%s\n' "$2" >> "$CURL_LOG"
cp "$DOWNLOAD_FIXTURE/${2##*/}" "$4"
STUB
  chmod +x "$BATS_TEST_TMPDIR/stubs/curl"
  export PATH="$BATS_TEST_TMPDIR/stubs:$PATH"
  unset PACTIFY_DOWNLOAD_BASE PACTIFY_VERSION
}

@test "install.sh downloads latest assets without GitHub API" {
  stub_release_downloads
  run sh "$REPO/install.sh"
  [ "$status" -eq 0 ]
  [ -x "$PACTIFY_BINDIR/pactify" ]
  [[ "$output" == *'is not on your PATH'* ]]
  [ "$(cat "$CURL_LOG")" = "https://github.com/agentjoey/pactify/releases/latest/download/pactify_${os}_${arch}.tar.gz
https://github.com/agentjoey/pactify/releases/latest/download/checksums.txt" ]
  run "$PACTIFY_BINDIR/pactify"
  [ "$output" = fake-pactify ]
}

@test "install.sh stub download rejects mismatched checksum" {
  stub_release_downloads
  printf 'corrupt archive' >> "$DOWNLOAD_FIXTURE/pactify_${os}_${arch}.tar.gz"
  run sh "$REPO/install.sh"
  [ "$status" -ne 0 ]
  [ ! -e "$PACTIFY_BINDIR/pactify" ]
}

@test "install.sh uses pinned release asset URLs" {
  stub_release_downloads
  run env PACTIFY_VERSION=v0.13.0 sh "$REPO/install.sh"
  [ "$status" -eq 0 ]
  [ "$(cat "$CURL_LOG")" = "https://github.com/agentjoey/pactify/releases/download/v0.13.0/pactify_${os}_${arch}.tar.gz
https://github.com/agentjoey/pactify/releases/download/v0.13.0/checksums.txt" ]
}

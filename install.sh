#!/bin/sh
# pactify installer. Usage: curl -fsSL https://raw.githubusercontent.com/agentjoey/pactify/main/install.sh | sh
# Overrides (for testing / pinning):
#   PACTIFY_VERSION       tag to install (default: latest release)
#   PACTIFY_DOWNLOAD_BASE base URL/dir holding the archives + checksums.txt (default: GitHub release)
#   PACTIFY_BINDIR        install dir (default: first writable of /usr/local/bin, ~/.local/bin)
set -eu

REPO="agentjoey/pactify"

detect_os() { uname -s | tr '[:upper:]' '[:lower:]'; }
detect_arch() {
  a="${1:-$(uname -m)}"
  case "$a" in
    x86_64|amd64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) echo "unsupported arch: $a" >&2; return 1 ;;
  esac
}

# --source-only lets tests load the functions without running main.
# When sourced, `return 0` ends sourcing early. When executed, `return` fails
# (outside a function) so `exit 0` runs; 2>/dev/null hides the shell's return
# complaint. shellcheck can't model the runtime-dependent reachability of exit 0.
# shellcheck disable=SC2317
case "${1:-}" in --source-only) return 0 2>/dev/null || exit 0 ;; esac

main() {
  os="$(detect_os)"; arch="$(detect_arch)"
  case "$os" in darwin|linux) ;; *) echo "unsupported OS: $os" >&2; exit 1 ;; esac

  version="${PACTIFY_VERSION:-}"
  if [ -z "${PACTIFY_DOWNLOAD_BASE:-}" ]; then
    if [ -z "$version" ]; then
      version="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
    fi
    base="https://github.com/$REPO/releases/download/$version"
  else
    base="$PACTIFY_DOWNLOAD_BASE"
  fi

  archive="pactify_${os}_${arch}.tar.gz"
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  echo "Downloading $archive ..."
  fetch() { case "$1" in file://*) cp "${1#file://}" "$2" ;; *) curl -fsSL "$1" -o "$2" ;; esac; }
  fetch "$base/$archive" "$tmp/$archive"
  fetch "$base/checksums.txt" "$tmp/checksums.txt"

  echo "Verifying checksum ..."
  ( cd "$tmp" && grep " $archive\$" checksums.txt | shasum -a 256 -c - ) >/dev/null

  tar xzf "$tmp/$archive" -C "$tmp"

  bindir="${PACTIFY_BINDIR:-}"
  if [ -z "$bindir" ]; then
    if [ -w /usr/local/bin ]; then bindir=/usr/local/bin; else bindir="$HOME/.local/bin"; fi
  fi
  mkdir -p "$bindir"
  install -m 0755 "$tmp/pactify" "$bindir/pactify"

  echo "✅ pactify installed to $bindir/pactify"
  case ":$PATH:" in *":$bindir:"*) ;; *) echo "⚠️  $bindir is not on your PATH — add it, e.g.: export PATH=\"$bindir:\$PATH\"" ;; esac
  echo "Next: run \`pactify setup\` in your repo to get started."
}

main

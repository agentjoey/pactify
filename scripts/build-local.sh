#!/bin/sh
# Rebuild pactify from this checkout, including the embedded dashboard and panel.
# On macOS the binary is ad-hoc signed: launchd kills an unsigned locally built
# binary (OS_REASON_CODESIGNING) without any output.
#
# Usage: scripts/build-local.sh [--no-web] [output]   (default output: ./pactify)
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
web=1
if [ "${1:-}" = "--no-web" ]; then
  web=0
  shift
fi
out=${1:-"$root/pactify"}
case "$out" in
  /*) ;;
  *) out="$(pwd)/$out" ;;
esac

if [ "$web" = 1 ]; then
  (cd "$root/web" && npm run build && npm run build:panel)
fi
(cd "$root" && go build -o "$out" ./cmd/pactify)
if [ "$(uname -s)" = Darwin ]; then
  codesign --force -s - "$out"
fi
echo "built $out"

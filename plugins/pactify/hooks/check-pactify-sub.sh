#!/usr/bin/env bash
# Report whether the pactify CLI with the `sub` command is available. Never fail the session.
if ! command -v pactify >/dev/null 2>&1; then
  echo "pactify plugin: the pactify CLI is not on PATH; /codex and /kimi will not work. Install: go install github.com/agentjoey/pactify/cmd/pactify@latest"
  exit 0
fi
if ! pactify sub --help >/dev/null 2>&1; then
  echo "pactify plugin: the installed pactify has no 'sub' command; upgrade pactify."
  exit 0
fi
exit 0

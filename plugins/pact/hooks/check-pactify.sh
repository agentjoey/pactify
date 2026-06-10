#!/bin/sh
# Detect the pactify binary; if missing, print the install hint (never auto-install).
if command -v pactify >/dev/null 2>&1; then
  exit 0
fi
echo "pact plugin: the 'pactify' binary is not on PATH."
echo "Install it:  curl -fsSL https://pactify.dev/install.sh | sh"
echo "Then run:    pactify setup"
exit 0

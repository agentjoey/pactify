#!/bin/sh
# Reuse the local check, stay quiet on success and never fail the session.
"${CLAUDE_PLUGIN_ROOT}/bin/pactify-check" --quiet || :

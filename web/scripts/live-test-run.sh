#!/usr/bin/env bash
# live-test-run.sh — one-shot reproducer for the Live agent-mirror feature.
#
# Spins up a REAL opencode worker run in the dogfood repo so the dashboard's
# Live view shows a lane terminal streaming live agent stdout. Uses opencode
# (not claude) on purpose: `claude -p` buffers to completion and the stream
# stays empty, while opencode emits incrementally — the only way to actually
# watch the terminal fill.
#
# Usage:
#   web/scripts/live-test-run.sh                 # assign + commit + orchestrate + tail
#   DOGFOOD=/path/to/repo web/scripts/live-test-run.sh
#   web/scripts/live-test-run.sh --no-tail       # don't tail; just launch
#   web/scripts/live-test-run.sh --clean         # reset dogfood: stop runs, drop
#                                                # feat-live* branches + demo files
#
# Then open http://127.0.0.1:17082 → pick the dogfood project → Live (key 3).
# The working lane auto-expands into "LIVE · opencode · <task>".

set -euo pipefail

DOGFOOD="${DOGFOOD:-$HOME/AgentWorks/Code_Claude/pact-dogfood-squad}"
SERVE="${SERVE:-http://127.0.0.1:17082}"
ORCH_AS="${ORCH_AS:-claude-opus}"   # orchestrator/reviewer seat in the dogfood roster
WORKER="${WORKER:-opencode}"        # worker seat (must map to the opencode kind)
TAIL=1
[[ "${1:-}" == "--no-tail" ]] && TAIL=0

[[ -d "$DOGFOOD/.pact" ]] || { echo "✗ no .pact in $DOGFOOD (set DOGFOOD=...)"; exit 1; }

# --clean: tear down anything a prior demo left behind so the next run starts fresh.
if [[ "${1:-}" == "--clean" ]]; then
  cd "$DOGFOOD"
  pkill -f "orchestrate --feature live" 2>/dev/null || true
  pkill -f "tlive" 2>/dev/null || true
  sleep 1
  git checkout -fq main 2>/dev/null || true
  git checkout -q opencode.json 2>/dev/null || true
  git clean -fdq docs .pact/orchestrate 2>/dev/null || true
  for b in $(git branch --format='%(refname:short)' | grep -E '^feat-live' || true); do
    git branch -Dq "$b" 2>/dev/null || true
    echo "  dropped $b"
  done
  echo "✓ dogfood reset to clean main"
  git status --short
  exit 0
fi

stamp="$(date +%H%M%S)"
task="tlive${stamp}"
feat="live${stamp}"
branch="feat-${feat}"
spec=".pact/tasks/${task}.md"
outfile="docs/live-demo-${stamp}.md"

cd "$DOGFOOD"

echo "▸ dogfood: $DOGFOOD"
echo "▸ task=$task feature=$feat branch=$branch"

# 1. Land on a clean main — the worker does `git checkout main && checkout -b`,
#    which aborts on a dirty tree (the #1 reason the stream stays empty).
git checkout -q main
if [[ -n "$(git status --porcelain)" ]]; then
  echo "✗ dogfood main is dirty — commit/stash/clean it first:"
  git status --short
  exit 1
fi

# 2. Write a task spec with a one-line verify: gate. The work is deliberately
#    multi-step so opencode narrates over a sustained window (~1–2 min) — a
#    one-file task finishes before you can switch to the Live tab.
mkdir -p .pact/tasks docs
cat > "$spec" <<EOF
# ${task} — live-stream demo

Create \`${outfile}\`, a docs page about this pact multi-agent dogfood. Work
**step by step**, narrating each step as you go:

1. First inspect the repo (read the root README and list \`docs/\`) to ground the content.
2. Write \`${outfile}\` with these sections, a short paragraph each:
   - **## Overview** — what this repo demonstrates.
   - **## How it works** — git + \`.pact/\` files as the source of truth; orchestrate drives agents.
   - **## Roles** — orchestrator / worker / reviewer and the two rules.
   - **## Try it** — how someone would run a task here.
3. Re-read the file you wrote and fix any section that reads awkwardly.

Touch only \`${outfile}\`.

verify: test -f ${outfile} && grep -q '## Roles' ${outfile}
EOF

# 3. Assign (orchestrator writes it) and commit the ledger to main so the worker
#    sees the assignment after it checks out main.
PACT_AGENT_ID="$ORCH_AS" pactify assign "$task" \
  --feature "$feat" --branch "$branch" \
  --owner "$WORKER" --reviewer "$ORCH_AS" --spec "$spec"
git add "$spec" .pact/log.jsonl .pact/STATE.yml
git commit -q -m "assign ${task} (${feat}) to ${WORKER} — live-stream demo"
echo "✓ assignment committed on main"

# 4. Launch orchestrate in the background; opencode worker starts streaming.
log="/tmp/live-test-${feat}.log"
PACT_AGENT_ID="$ORCH_AS" nohup pactify orchestrate --feature "$feat" --as "$ORCH_AS" \
  --seat-kind "${WORKER}=opencode" --seat-kind "${ORCH_AS}=claude-code" \
  > "$log" 2>&1 &
orch_pid=$!
echo "✓ orchestrate started (pid $orch_pid, log $log)"

streamf=".pact/orchestrate/streams/${task}.log"
echo
echo "→ Open ${SERVE} → pick $(basename "$DOGFOOD") → Live (key 3); the working"
echo "  lane auto-expands into 'LIVE · ${WORKER} · ${task}'."
echo "→ stream file: ${DOGFOOD}/${streamf}"
echo "→ stop early:  kill ${orch_pid}   (or: pkill -f 'orchestrate --feature ${feat}')"
echo

if [[ "$TAIL" == "1" ]]; then
  echo "▸ tailing the live stream (Ctrl-C to stop tailing; the run keeps going)…"
  echo "  (waiting for opencode to emit — first bytes take a few seconds)"
  # wait for the file, then follow it
  for _ in $(seq 1 40); do [[ -s "$streamf" ]] && break; sleep 1; done
  tail -f "$streamf"
fi

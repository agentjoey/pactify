#!/usr/bin/env bash
# pact.sh — pact protocol reference implementation (Phase 0).
# Sourced by agents. All state lives under $PACT_DIR.

PACT_DIR="${PACT_DIR:-.pact}"
PACT_LOG="$PACT_DIR/log.jsonl"
PACT_STATE="$PACT_DIR/STATE.yml"
PACT_TASKS="$PACT_DIR/tasks"

pact_init() { echo "stub" >&2; return 1; }

#!/bin/sh
# Runs one verification step and counts its outcome on a todou card, so we can
# see which steps ever stop a change (PHB-105).
#
#   scripts/gate.sh <gate> <command> [args...]
#
# The step's exit code is passed through unchanged. Each run adds one to
# `<gate>.pass` or `<gate>.fail` in the `gate-stats` metadata namespace of the
# stats card (PROHIBITORUM_GATE_STATS_CARD, default PHB-105). Counting is
# best-effort: it is skipped in GitHub Actions, without todou or jq, or with
# PROHIBITORUM_GATE_STATS=0, and a failed write only prints a note.
#
# Read the counts with: todou metadata get PHB-105 --namespace gate-stats
set -u

[ $# -ge 2 ] || { printf '%s\n' 'Usage: scripts/gate.sh <gate> <command> [args...]' >&2; exit 2; }
gate=$1
shift

status=0
"$@" || status=$?

todou_call() {
  if command -v timeout >/dev/null 2>&1; then timeout 15 todou "$@"; else todou "$@"; fi
}

record() {
  [ "${PROHIBITORUM_GATE_STATS:-1}" != 0 ] || return 0
  [ -z "${GITHUB_ACTIONS:-}" ] || return 0
  command -v todou >/dev/null 2>&1 || return 0
  command -v jq >/dev/null 2>&1 || return 0

  cd "$(dirname -- "$0")/.." || return 1
  card=${PROHIBITORUM_GATE_STATS_CARD:-PHB-105}
  if [ "$status" = 0 ]; then key=$gate.pass; else key=$gate.fail; fi

  # Compare-and-set, so concurrent runs in other worktrees don't lose a count.
  for _ in 1 2 3 4 5; do
    current=$(todou_call --json metadata get "$card" --namespace gate-stats |
      jq -r --arg k "$key" '.entries[] | select(.key == $k) | .value') || return 1
    if [ -z "$current" ]; then
      todou_call metadata set "$card" --namespace gate-stats --if-absent "$key" "$key=1" && return 0
    else
      todou_call metadata set "$card" --namespace gate-stats --if-match "$key=$current" "$key=$((current + 1))" && return 0
    fi
  done
  return 1
}
(record >/dev/null 2>&1) ||
  printf 'gate-stats: could not count %s on todou; the step result is unaffected.\n' "$gate" >&2

exit "$status"

#!/usr/bin/env bash
set -euo pipefail

TARGET_DIR="${1:-.}"
MAX_RESTARTS="${GOOLOOP_MAX_RESTARTS:-10}"
RESTARTS=0

while true; do
  goloop run "$TARGET_DIR" "${@:2}"
  code=$?
  if [[ "$code" -eq 75 ]]; then
    RESTARTS=$((RESTARTS + 1))
    if [[ "$RESTARTS" -gt "$MAX_RESTARTS" ]]; then
      echo "goloop: max tool restarts ($MAX_RESTARTS) exceeded" >&2
      exit 75
    fi
    echo "goloop: restarting after tool install (attempt $RESTARTS/$MAX_RESTARTS)…" >&2
    sleep 1
    continue
  fi
  exit "$code"
done

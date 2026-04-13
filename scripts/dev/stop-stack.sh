#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

STATE_DIR="${PHOTONEST_DEV_STATE_DIR:-$ROOT_DIR/.cache/dev-stack}"
PID_DIR="$STATE_DIR/pids"

stop_process() {
  local name="$1"
  local pid_file="$PID_DIR/$name.pid"

  if [[ ! -f "$pid_file" ]]; then
    echo "$name is not running"
    return 0
  fi

  local pid
  pid="$(cat "$pid_file")"
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    echo "stopped $name ($pid)"
  else
    echo "$name pid file existed but process $pid was not running"
  fi
  rm -f "$pid_file"
}

stop_process web
stop_process worker
stop_process api

echo "Application processes stopped. Use 'make down' if you also want to remove docker dependencies."

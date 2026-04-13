#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

STATE_DIR="${PHOTONEST_DEV_STATE_DIR:-$ROOT_DIR/.cache/dev-stack}"
PID_DIR="$STATE_DIR/pids"
LOG_DIR="$STATE_DIR/logs"

print_process() {
  local name="$1"
  local pid_file="$PID_DIR/$name.pid"

  if [[ ! -f "$pid_file" ]]; then
    echo "$name: stopped"
    return
  fi

  local pid
  pid="$(cat "$pid_file")"
  if kill -0 "$pid" 2>/dev/null; then
    echo "$name: running (pid $pid) log=$LOG_DIR/$name.log"
  else
    echo "$name: stale pid file ($pid) log=$LOG_DIR/$name.log"
  fi
}

print_process api
print_process worker
print_process web

echo
docker compose ps || true

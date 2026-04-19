#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

STATE_DIR="${PHOTONEST_DEV_STATE_DIR:-$ROOT_DIR/.cache/dev-stack}"
PID_DIR="$STATE_DIR/pids"

find_listening_pids() {
  local port="$1"
  lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true
}

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

stop_orphaned_port_processes() {
  local name="$1"
  local port="$2"
  local pid_file="$PID_DIR/$name.pid"
  local managed_pid=""

  if [[ -f "$pid_file" ]]; then
    managed_pid="$(cat "$pid_file")"
  fi

  local found_any=0
  while read -r pid; do
    [[ -z "$pid" ]] && continue
    if [[ -n "$managed_pid" && "$pid" == "$managed_pid" ]]; then
      continue
    fi
    if kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      echo "stopped orphaned $name process on :$port ($pid)"
      found_any=1
    fi
  done < <(find_listening_pids "$port")

  if [[ "$found_any" -eq 0 ]]; then
    echo "no orphaned $name process found on :$port"
  fi
}

stop_process web
stop_process worker
stop_process api
stop_orphaned_port_processes web 3000
stop_orphaned_port_processes api 8080

echo "Application processes stopped. Use 'make down' if you also want to remove docker dependencies."

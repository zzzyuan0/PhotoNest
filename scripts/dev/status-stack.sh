#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

STATE_DIR="${PHOTONEST_DEV_STATE_DIR:-$ROOT_DIR/.cache/dev-stack}"
PID_DIR="$STATE_DIR/pids"
LOG_DIR="$STATE_DIR/logs"

find_listening_pids() {
  local port="$1"
  lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true
}

print_process() {
  local name="$1"
  local port="$2"
  local pid_file="$PID_DIR/$name.pid"

  if [[ ! -f "$pid_file" ]]; then
    local listening
    listening="$(find_listening_pids "$port" | tr '\n' ' ' | xargs echo -n)"
    if [[ -n "$listening" ]]; then
      echo "$name: orphaned listener on :$port pid=$listening log=$LOG_DIR/$name.log"
    else
      echo "$name: stopped"
    fi
    return
  fi

  local pid
  pid="$(cat "$pid_file")"
  if kill -0 "$pid" 2>/dev/null; then
    echo "$name: running (pid $pid) log=$LOG_DIR/$name.log"
  else
    local listening
    listening="$(find_listening_pids "$port" | tr '\n' ' ' | xargs echo -n)"
    if [[ -n "$listening" ]]; then
      echo "$name: stale pid file ($pid), orphaned listener on :$port pid=$listening log=$LOG_DIR/$name.log"
    else
      echo "$name: stale pid file ($pid) log=$LOG_DIR/$name.log"
    fi
  fi
}

print_process api 8080
print_process worker 0
print_process web 3000

echo
docker compose ps || true

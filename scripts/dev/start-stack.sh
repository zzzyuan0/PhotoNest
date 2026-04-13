#!/usr/bin/env bash
set -euo pipefail

MODE="${1:-sim}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

source "$ROOT_DIR/scripts/dev/load-env.sh" "$ROOT_DIR"

STATE_DIR="${PHOTONEST_DEV_STATE_DIR:-$ROOT_DIR/.cache/dev-stack}"
LOG_DIR="$STATE_DIR/logs"
PID_DIR="$STATE_DIR/pids"

mkdir -p "$LOG_DIR" "$PID_DIR"

prepare_go_runtime() {
  echo "preparing Go toolchain and modules..."
  "$ROOT_DIR/scripts/dev/go-tool.sh" go mod download
}

wait_for_port() {
  local name="$1"
  local port="$2"
  local attempts="${3:-30}"
  local count=0

  while (( count < attempts )); do
    if node -e "const net=require('net');const socket=net.createConnection({host:'127.0.0.1',port:$port});socket.setTimeout(1000);socket.on('connect',()=>{socket.end();process.exit(0)});socket.on('timeout',()=>{socket.destroy();process.exit(1)});socket.on('error',()=>process.exit(1));"; then
      echo "$name is ready on :$port"
      return 0
    fi
    count=$((count + 1))
    sleep 1
  done

  echo "$name did not become ready on :$port" >&2
  return 1
}

start_process() {
  local name="$1"
  local command="$2"
  local pid_file="$PID_DIR/$name.pid"
  local log_file="$LOG_DIR/$name.log"

  if [[ -f "$pid_file" ]]; then
    local existing_pid
    existing_pid="$(cat "$pid_file")"
    if kill -0 "$existing_pid" 2>/dev/null; then
      echo "$name already running with pid $existing_pid"
      return 0
    fi
    rm -f "$pid_file"
  fi

  setsid bash -lc "cd '$ROOT_DIR' && source '$ROOT_DIR/scripts/dev/load-env.sh' '$ROOT_DIR' && export PHOTONEST_CONFIG='${PHOTONEST_CONFIG}' && export NUXT_PUBLIC_API_BASE_URL='${NUXT_PUBLIC_API_BASE_URL}' && exec nohup ${command} </dev/null >>'$log_file' 2>&1" >/dev/null 2>&1 &
  echo $! >"$pid_file"
  echo "started $name with pid $(cat "$pid_file")"
}

case "$MODE" in
  sim)
    export PHOTONEST_CONFIG="${PHOTONEST_CONFIG:-$("$ROOT_DIR/scripts/dev/render-local-object-sim-config.sh")}"
    docker compose --profile object-sim up -d postgres redis minio minio-init
    wait_for_port postgres 5432 60
    wait_for_port redis 6379 60
    wait_for_port minio 9000 60
    ;;
  cos)
    export PHOTONEST_CONFIG="$("$ROOT_DIR/scripts/dev/render-local-cos-config.sh")"
    docker compose up -d postgres redis
    wait_for_port postgres 5432 60
    wait_for_port redis 6379 60
    ;;
  *)
    echo "unsupported mode: $MODE" >&2
    echo "usage: $0 [sim|cos]" >&2
    exit 1
    ;;
esac

prepare_go_runtime

start_process api "./scripts/dev/api.sh"
wait_for_port api 8080 120
start_process worker "./scripts/dev/worker.sh"
start_process web "./scripts/dev/web.sh"
wait_for_port web 3000 90

cat <<EOF
Stack started in ${MODE} mode.

Config: ${PHOTONEST_CONFIG}
API:    http://localhost:8080
Web:    http://localhost:3000
Logs:   ${LOG_DIR}
EOF

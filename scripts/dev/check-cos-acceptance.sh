#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

source "$ROOT_DIR/scripts/dev/load-env.sh" "$ROOT_DIR"
source "$ROOT_DIR/scripts/dev/go-env.sh"

status=0

check_env() {
  local key="$1"
  if [[ -n "${!key:-}" ]]; then
    printf 'ok   env %s\n' "$key"
  else
    printf 'fail env %s\n' "$key"
    status=1
  fi
}

check_port() {
  local name="$1"
  local host="$2"
  local port="$3"
  if node -e "const net=require('net'); const socket=net.createConnection({host:'$host',port:$port}); socket.setTimeout(1500); socket.on('connect',()=>{socket.end(); process.exit(0);}); socket.on('timeout',()=>{socket.destroy(); process.exit(1);}); socket.on('error',()=>process.exit(1));"; then
    printf 'ok   port %s %s:%s\n' "$name" "$host" "$port"
  else
    printf 'fail port %s %s:%s\n' "$name" "$host" "$port"
    status=1
  fi
}

check_file() {
  local path="$1"
  if [[ -f "$path" ]]; then
    printf 'ok   file %s\n' "$path"
  else
    printf 'fail file %s\n' "$path"
    status=1
  fi
}

check_command() {
  local label="$1"
  local path="$2"
  if [[ -x "$path" ]]; then
    printf 'ok   tool %s\n' "$label"
  else
    printf 'fail tool %s\n' "$label"
    status=1
  fi
}

echo "Checking prerequisites for COS upload acceptance..."

check_command "go" "$PHOTONEST_GO_ROOT/bin/go"
check_file "$ROOT_DIR/docs/cos-upload-acceptance.md"

check_env "PHOTONEST_COS_SECRET_ID"
check_env "PHOTONEST_COS_SECRET_KEY"
check_env "PHOTONEST_SESSION_SIGNING_KEY"
check_env "PHOTONEST_BOOTSTRAP_PASSWORD"

if [[ -n "${PHOTONEST_DATABASE_DSN:-}" ]]; then
  printf 'ok   env PHOTONEST_DATABASE_DSN\n'
else
  check_env "PHOTONEST_DATABASE_PASSWORD"
fi

check_port "postgres" "127.0.0.1" "5432"
check_port "redis" "127.0.0.1" "6379"

if [[ $status -eq 0 ]]; then
  echo "All acceptance prerequisites look ready."
else
  echo "Acceptance prerequisites are still incomplete."
fi

exit "$status"

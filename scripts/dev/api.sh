#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

source "$ROOT_DIR/scripts/dev/load-env.sh" "$ROOT_DIR"

CONFIG_PATH="${PHOTONEST_CONFIG:-./config/examples/app.yaml}"

if command -v go >/dev/null 2>&1; then
  PHOTONEST_CONFIG="$CONFIG_PATH" go run ./cmd/api
  exit 0
fi

docker run --rm -it \
  -v "$ROOT_DIR:/workspace" \
  -w /workspace \
  -e PHOTONEST_CONFIG="$CONFIG_PATH" \
  golang:1.24 \
  go run ./cmd/api

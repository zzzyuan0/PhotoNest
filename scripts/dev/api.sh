#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

source "$ROOT_DIR/scripts/dev/load-env.sh" "$ROOT_DIR"
source "$ROOT_DIR/scripts/dev/go-env.sh"

CONFIG_PATH="${PHOTONEST_CONFIG:-./config/examples/app.yaml}"
PHOTONEST_CONFIG="$CONFIG_PATH" "$ROOT_DIR/scripts/dev/go-tool.sh" go run ./cmd/api

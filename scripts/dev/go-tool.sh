#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

source "$ROOT_DIR/scripts/dev/load-env.sh" "$ROOT_DIR"
source "$ROOT_DIR/scripts/dev/go-env.sh"

TOOL_NAME="${1:?tool name is required}"
shift

if [[ -x "$PHOTONEST_GO_ROOT/bin/$TOOL_NAME" ]]; then
  exec "$PHOTONEST_GO_ROOT/bin/$TOOL_NAME" "$@"
fi

cat >&2 <<EOF
Go toolchain not found at $PHOTONEST_GO_ROOT/bin/$TOOL_NAME.
This repository is configured to keep Go binaries, module cache, build cache, and env data under \$HOME only.
Install Go under $PHOTONEST_GO_ROOT and rerun the command.
EOF
exit 1

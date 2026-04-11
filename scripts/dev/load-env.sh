#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${1:?root directory is required}"

if [[ -f "$ROOT_DIR/.env.local" ]]; then
  # shellcheck disable=SC1091
  set -a && source "$ROOT_DIR/.env.local" && set +a
  exit 0
fi

if [[ -f "$ROOT_DIR/.env.example" ]]; then
  # shellcheck disable=SC1091
  set -a && source "$ROOT_DIR/.env.example" && set +a
fi

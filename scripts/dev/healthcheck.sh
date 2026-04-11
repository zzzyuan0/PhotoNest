#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

docker compose ps

echo
echo "Expected service endpoints:"
echo "  postgres -> localhost:5432"
echo "  redis    -> localhost:6379"
echo "  api      -> http://localhost:8080/api/v1/health"

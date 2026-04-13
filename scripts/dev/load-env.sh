#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${1:?root directory is required}"

if [[ -f "$ROOT_DIR/.env.local" ]]; then
  # shellcheck disable=SC1091
  set -a && source "$ROOT_DIR/.env.local" && set +a
elif [[ -f "$ROOT_DIR/.env.example" ]]; then
  # shellcheck disable=SC1091
  set -a && source "$ROOT_DIR/.env.example" && set +a
fi

if [[ -z "${PHOTONEST_COS_SECRET_ID:-}" && -n "${STORAGE_COS_SECRET_ID:-}" ]]; then
  export PHOTONEST_COS_SECRET_ID="$STORAGE_COS_SECRET_ID"
fi

if [[ -z "${PHOTONEST_COS_SECRET_KEY:-}" && -n "${STORAGE_COS_SECRET_KEY:-}" ]]; then
  export PHOTONEST_COS_SECRET_KEY="$STORAGE_COS_SECRET_KEY"
fi

if [[ -z "${PHOTONEST_COS_BACKUP_SECRET_ID:-}" && -n "${STORAGE_COS_SECRET_ID:-}" ]]; then
  export PHOTONEST_COS_BACKUP_SECRET_ID="$STORAGE_COS_SECRET_ID"
fi

if [[ -z "${PHOTONEST_COS_BACKUP_SECRET_KEY:-}" && -n "${STORAGE_COS_SECRET_KEY:-}" ]]; then
  export PHOTONEST_COS_BACKUP_SECRET_KEY="$STORAGE_COS_SECRET_KEY"
fi

if [[ -z "${PHOTONEST_DATABASE_PASSWORD:-}" ]]; then
  export PHOTONEST_DATABASE_PASSWORD="postgres"
fi

if [[ -z "${PHOTONEST_DATABASE_DSN:-}" ]]; then
  export PHOTONEST_DATABASE_DSN="postgres://postgres:${PHOTONEST_DATABASE_PASSWORD}@localhost:5432/photonest?sslmode=disable"
fi

if [[ -z "${PHOTONEST_REDIS_PASSWORD:-}" ]]; then
  export PHOTONEST_REDIS_PASSWORD=""
fi

if [[ -z "${PHOTONEST_SESSION_SIGNING_KEY:-}" ]]; then
  export PHOTONEST_SESSION_SIGNING_KEY="photonest-local-session-signing-key-2026"
fi

if [[ -z "${PHOTONEST_BOOTSTRAP_PASSWORD:-}" ]]; then
  export PHOTONEST_BOOTSTRAP_PASSWORD="secret-password"
fi

if [[ -z "${PHOTONEST_AI_OPENAI_TOKEN:-}" ]]; then
  export PHOTONEST_AI_OPENAI_TOKEN="local-dev-openai-token"
fi

if [[ -z "${NUXT_PUBLIC_API_BASE_URL:-}" ]]; then
  export NUXT_PUBLIC_API_BASE_URL="http://localhost:8080"
fi

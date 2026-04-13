#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

source "$ROOT_DIR/scripts/dev/load-env.sh" "$ROOT_DIR"

STATE_DIR="${PHOTONEST_DEV_STATE_DIR:-$ROOT_DIR/.cache/dev-stack}"
CONFIG_OUT="$STATE_DIR/app.cos.local.yaml"

mkdir -p "$STATE_DIR"

required_vars=(
  STORAGE_COS_BUCKET
  STORAGE_COS_REGION
  STORAGE_COS_ENDPOINT
)

for key in "${required_vars[@]}"; do
  if [[ -z "${!key:-}" ]]; then
    echo "missing required env: $key" >&2
    exit 1
  fi
done

key_prefix="${STORAGE_COS_KEY_PREFIX:-photonest/dev/}"
health_url="${STORAGE_COS_ENDPOINT%/}"

cat >"$CONFIG_OUT" <<EOF
service:
  name: photonest
  environment: development

server:
  port: 8080

database:
  host: localhost
  port: 5432
  name: photonest
  user: postgres
  password:
    env: PHOTONEST_DATABASE_PASSWORD
  dsn:
    env: PHOTONEST_DATABASE_DSN
  sslMode: disable
  maxOpenConns: 20
  maxIdleConns: 5

queue:
  address: localhost:6379
  password:
    env: PHOTONEST_REDIS_PASSWORD
    allowEmpty: true
  db: 0
  namespace: photonest
  maxConsumers: 8

storageProviders:
  primary:
    name: primary-cos
    kind: tencent-cos
    bucket: ${STORAGE_COS_BUCKET}
    region: ${STORAGE_COS_REGION}
    endpoint: ${STORAGE_COS_ENDPOINT}
    keyPrefix: ${key_prefix}
    accessKeyId:
      env: PHOTONEST_COS_SECRET_ID
    accessKeySecret:
      env: PHOTONEST_COS_SECRET_KEY
    uploadPresignTTL: 15m
    downloadPresignTTL: 5m
    allowedOrigins:
      - http://localhost:3000
      - http://127.0.0.1:3000
    privateRead: true
    healthCheckURL: ${health_url}
    publicReadBlockMode: fail-fast
  backup: []

aiProviders:
  - name: remote-openai
    kind: openai-compatible
    endpoint: https://api.openai.com/v1
    model: gpt-4.1-mini
    capabilities:
      - caption
      - ocr
      - embedding
    token:
      env: PHOTONEST_AI_OPENAI_TOKEN
    timeout: 20s
    allowRemote: true
    executionBoundary: remote-service
    healthCheckURL: https://api.openai.com/v1/models

telemetry:
  logLevel: info
  enableMetrics: true
  enableTracing: true
  enableStructured: true
  redactionProfile: strict

security:
  csrfEnabled: true
  recentAuthWindow: 15m
  uploadCredentialTTL: 15m
  downloadCredentialTTL: 5m
  debugRetention: 24h
  strictPrivateObjectCheck: true
  session:
    cookieName: photonest_session
    csrfCookieName: photonest_csrf
    csrfHeaderName: X-CSRF-Token
    signingKey:
      env: PHOTONEST_SESSION_SIGNING_KEY
    maxAge: 12h
    secureCookies: false
    sameSite: strict
  bootstrapAuth:
    username: admin
    password:
      env: PHOTONEST_BOOTSTRAP_PASSWORD
    subject: bootstrap-admin
    displayName: Bootstrap Admin
    roles:
      - admin
    libraryIds:
      - 11111111-1111-1111-1111-111111111111
EOF

printf '%s\n' "$CONFIG_OUT"

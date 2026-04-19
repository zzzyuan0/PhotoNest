#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

source "$ROOT_DIR/scripts/dev/load-env.sh" "$ROOT_DIR"

STATE_DIR="${PHOTONEST_DEV_STATE_DIR:-$ROOT_DIR/.cache/dev-stack}"
CONFIG_OUT="$STATE_DIR/app.object-sim.local.yaml"

mkdir -p "$STATE_DIR"

endpoint="${PHOTONEST_OBJECT_STORAGE_ENDPOINT:-http://localhost:9000}"

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
    name: primary-minio
    kind: s3-compatible
    bucket: photonest-main
    region: us-east-1
    endpoint: ${endpoint}
    forcePathStyle: true
    keyPrefix: libraries/main/
    accessKeyId:
      value: photonest
    accessKeySecret:
      value: photonest-dev-password
    sessionToken:
      allowEmpty: true
    uploadPresignTTL: 15m
    downloadPresignTTL: 5m
    allowedOrigins:
      - http://localhost:3000
      - http://127.0.0.1:3000
    privateRead: true
    healthCheckURL: http://localhost:9000/minio/health/live
    publicReadBlockMode: fail-fast
  backup:
    - name: backup-minio
      kind: s3-compatible
      bucket: photonest-backup
      region: us-east-1
      endpoint: ${endpoint}
      forcePathStyle: true
      keyPrefix: backup/library-main/
      accessKeyId:
        value: photonest
      accessKeySecret:
        value: photonest-dev-password
      sessionToken:
        allowEmpty: true
      uploadPresignTTL: 15m
      downloadPresignTTL: 5m
      allowedOrigins:
        - http://localhost:3000
      privateRead: true
      healthCheckURL: http://localhost:9000/minio/health/live
      publicReadBlockMode: fail-fast

aiProviders:
  - name: remote-openai
    kind: openai-compatible
    endpoint: ${PHOTONEST_AI_OPENAI_ENDPOINT:-https://api.openai.com/v1}
    modelProfile: ${PHOTONEST_AI_MODEL_PROFILE:-default}
    models:
      budget: ${PHOTONEST_AI_MODEL_BUDGET:-gpt-4.1-mini}
      default: ${PHOTONEST_AI_MODEL_DEFAULT:-gpt-4.1-mini}
    capabilities:
      - caption
      - ocr
      - embedding
    token:
      env: PHOTONEST_AI_OPENAI_TOKEN
    timeout: 20s
    allowRemote: true
    executionBoundary: remote-service
    healthCheckURL: ${PHOTONEST_AI_HEALTHCHECK_URL:-${PHOTONEST_AI_OPENAI_ENDPOINT:-https://api.openai.com/v1}/models}

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

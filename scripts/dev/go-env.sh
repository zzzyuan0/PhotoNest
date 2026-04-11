#!/usr/bin/env bash
set -euo pipefail

default_home_dir="${PHOTONEST_HOME_DIR:-}"
if [[ -z "$default_home_dir" ]]; then
  if [[ -d /home/root ]]; then
    default_home_dir="/home/root"
  else
    default_home_dir="$HOME"
  fi
fi

export PHOTONEST_HOME_DIR="$default_home_dir"
export PHOTONEST_GO_ROOT="${PHOTONEST_GO_ROOT:-$PHOTONEST_HOME_DIR/.local/go}"
export PHOTONEST_GO_HOME_DIR="${PHOTONEST_GO_HOME_DIR:-$PHOTONEST_HOME_DIR/.local/share/photonest/go/home}"
export GOPATH="${GOPATH:-$PHOTONEST_HOME_DIR/.local/share/photonest/go}"
export GOMODCACHE="${GOMODCACHE:-$GOPATH/pkg/mod}"
export GOCACHE="${GOCACHE:-$PHOTONEST_HOME_DIR/.cache/photonest/go-build}"
export GOENV="${GOENV:-$PHOTONEST_HOME_DIR/.config/go/env}"
export GOTELEMETRY="${GOTELEMETRY:-off}"

mkdir -p "$PHOTONEST_GO_HOME_DIR" "$GOPATH/bin" "$GOMODCACHE" "$GOCACHE" "$(dirname "$GOENV")"
touch "$GOENV"

if [[ -x "$PHOTONEST_GO_ROOT/bin/go" ]]; then
  export PATH="$PHOTONEST_GO_ROOT/bin:$PATH"
fi

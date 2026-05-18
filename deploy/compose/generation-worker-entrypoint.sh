#!/bin/sh
set -eu

codex_home="${CODEX_HOME:-${HOME:-/home/app}/.codex}"
host_codex="${CODEX_HOST_HOME:-/mnt/host-codex}"

mkdir -p "$codex_home"

if [ -d "$host_codex" ]; then
  for name in auth.json config.toml version.json models_cache.json; do
    if [ -f "$host_codex/$name" ]; then
      cp "$host_codex/$name" "$codex_home/$name"
      chmod 600 "$codex_home/$name" 2>/dev/null || true
    fi
  done
fi

exec "$@"

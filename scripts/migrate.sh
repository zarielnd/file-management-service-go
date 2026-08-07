#!/usr/bin/env bash

set -e

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [ -f "$ROOT_DIR/.env" ]; then
    set -a
    source "$ROOT_DIR/.env"
    set +a
fi

DATABASE_URL="${DATABASE_URL:?DATABASE_URL is not set}"

migrate \
    -path "$ROOT_DIR/services/storage-service/migrations" \
    -database "$DATABASE_URL" \
    up
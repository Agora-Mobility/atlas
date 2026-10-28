#!/usr/bin/env bash
set -euo pipefail

echo "entrypoint: applying database migrations"
go run ./cmd/migrate up

exec "$@"

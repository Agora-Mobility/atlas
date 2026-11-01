#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose_file="$root/test/load/docker-compose.yml"
work="$(mktemp -d)"
port="${LOAD_HTTP_PORT:-18700}"
results="${LOAD_RESULTS:-$work}"
mkdir -p "$results"

cleanup() {
  if [[ -n "${api_pid:-}" ]]; then
    kill -TERM "$api_pid" 2>/dev/null || true
    wait "$api_pid" 2>/dev/null || true
  fi
  docker compose -f "$compose_file" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker compose -f "$compose_file" up -d --wait

export APP_ENV=test LOG_LEVEL=warn HTTP_PORT="$port" DB_MIGRATE_ON_START=false DB_MAX_CONNS="${LOAD_DB_CONNS:-60}" DB_MIN_CONNS=5
export DATABASE_URL="postgres://atlas:atlas@127.0.0.1:25432/atlas?sslmode=disable"
export REDIS_ADDR=127.0.0.1:26379 RABBITMQ_URL=amqp://atlas:atlas@127.0.0.1:25672/
export JWT_SECRET="$(openssl rand -hex 32)" MFA_ENCRYPTION_KEY="$(openssl rand -base64 32)"
export RATE_LIMIT_REQUESTS=100000000 OUTBOX_POLL_INTERVAL=100ms RABBITMQ_PREFETCH=50

cd "$root"
go build -o "$work/atlas" .
go build -o "$work/migrate" ./cmd/migrate
go build -o "$work/seed" ./test/load/seed
go build -o "$work/probe" ./test/load/probe

cd "$work"
./migrate up
./seed -customers "${LOAD_CUSTOMERS:-1000}" -providers "${LOAD_PROVIDERS:-500}" -out "$work/fixtures.json"
./atlas > "$results/atlas.log" 2>&1 &
api_pid=$!

for _ in $(seq 1 60); do
  curl -fsS "http://127.0.0.1:$port/ready" >/dev/null 2>&1 && break
  sleep 1
done

ulimit -n 65535 2>/dev/null || true
./probe -base "http://127.0.0.1:$port" -fixtures "$work/fixtures.json" -idle "${LOAD_IDLE_STREAMS:-1000}" > "$results/sse.json" &
probe_pid=$!
sleep 3
status=0
k6 run --quiet --summary-export "$results/summary.json" \
  -e BASE_URL="http://127.0.0.1:$port" -e FIXTURES="$work/fixtures.json" \
  "$root/test/load/dispatch.js" | tee "$results/k6.txt" || status=$?

kill -TERM "$probe_pid" 2>/dev/null || true
wait "$probe_pid" 2>/dev/null || true
cat "$results/sse.json"
curl -fsS "http://127.0.0.1:$port/metrics" > "$results/metrics.txt" || true
docker stats --no-stream --format '{{.Name}} {{.CPUPerc}} {{.MemUsage}}' > "$results/docker-stats.txt" || true
echo "results in $results"
exit "$status"

#!/bin/sh
set -e

# Arranque da API: espera pelo Postgres, aplica as migrations e carrega os dados de teste.
# Serve as duas imagens: a de produção traz os binários /migrate e /seed; a de dev (Dockerfile.dev)
# não os tem e corre o código com go run.

: "${DATABASE_URL:?DATABASE_URL is required}"

# Sem a query string: o pg_isready (libpq) recusa parâmetros que só o pgx entende.
DB_ADDRESS="${DATABASE_URL%%\?*}"
DB_WAIT_SECONDS="${DB_WAIT_SECONDS:-60}"

# O prazo conta segundos reais: cada pg_isready pode demorar até 2s, e contar tentativas fazia a espera
# passar do tempo que o deploy dá ao healthcheck, que matava o container antes de sabermos o motivo.
# A saída do pg_isready ("host:porta - no response") diz para onde está a ligar, sem a senha.
echo "Waiting for PostgreSQL to be ready (up to ${DB_WAIT_SECONDS}s)..."
deadline=$(($(date +%s) + DB_WAIT_SECONDS))
attempt=0
while :; do
  status=0
  result=$(pg_isready -t 2 -d "$DB_ADDRESS" 2>&1) || status=$?
  [ "$status" -eq 0 ] && break
  if [ "$status" -eq 3 ]; then
    echo "pg_isready cannot use DATABASE_URL (exit 3): check its format and percent-encode special characters in the password. ${result}"
    exit 1
  fi
  attempt=$((attempt + 1))
  [ $((attempt % 10)) -eq 1 ] && echo "PostgreSQL not ready yet (exit ${status}): ${result}"
  if [ "$(date +%s)" -ge "$deadline" ]; then
    echo "PostgreSQL is not ready after ${DB_WAIT_SECONDS}s (exit ${status}): ${result}"
    exit 1
  fi
  sleep 1
done
echo "PostgreSQL is ready: ${result}"

# O /migrate cria o schema atlas antes de aplicar as migrations. Qualquer erro derruba o arranque.
echo "Running database migrations..."
set +e
if [ -x /migrate ]; then
  /migrate up
else
  go run ./cmd/migrate up
fi
MIGRATE_EXIT_CODE=$?
set -e

if [ $MIGRATE_EXIT_CODE -eq 0 ]; then
  echo "Migrations applied successfully (or already up to date)."
else
  echo "Migration failed with exit code $MIGRATE_EXIT_CODE"
  exit $MIGRATE_EXIT_CODE
fi

# Os dados de teste (utilizadores com senha conhecida, prestador, oficinas, tarifas de exemplo) nunca
# entram em produção. O seed é idempotente; se falhar, fica no log e a API arranca na mesma.
if [ "${APP_ENV:-production}" = "production" ]; then
  echo "APP_ENV=production: skipping seed."
else
  echo "Running seed (APP_ENV=$APP_ENV)..."
  if [ -x /seed ]; then
    /seed || echo "Seed failed (ignored)."
  else
    go run ./cmd/seed || echo "Seed failed (ignored)."
  fi
fi

exec "$@"

#!/bin/sh
set -e

# Arranque da API: espera pelo Postgres, aplica as migrations e carrega os dados de teste.
# Serve as duas imagens: a de produção traz os binários /migrate e /seed; a de dev (Dockerfile.dev)
# não os tem e corre o código com go run.

: "${DATABASE_URL:?DATABASE_URL is required}"

# Sem a query string: o pg_isready (libpq) recusa parâmetros que só o pgx entende.
DB_ADDRESS="${DATABASE_URL%%\?*}"
DB_WAIT_SECONDS="${DB_WAIT_SECONDS:-60}"

echo "Waiting for PostgreSQL to be ready..."
waited=0
until pg_isready -q -d "$DB_ADDRESS"; do
  waited=$((waited + 1))
  if [ "$waited" -ge "$DB_WAIT_SECONDS" ]; then
    echo "PostgreSQL is not ready after ${DB_WAIT_SECONDS}s."
    exit 1
  fi
  sleep 1
done
echo "PostgreSQL is ready."

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

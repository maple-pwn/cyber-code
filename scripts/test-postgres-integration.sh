#!/bin/sh
set -eu

if [ -n "${CYBER_CODE_TEST_POSTGRES_DSN:-}" ]; then
  GOCACHE="${GOCACHE:-/private/tmp/cyber-code-build-cache}" go test ./internal/authorization -run PostgreSQLIntegration -count=1 -v
  exit $?
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "SKIP: Docker is unavailable and CYBER_CODE_TEST_POSTGRES_DSN is not set"
  exit 0
fi

container="cyber-code-postgres-$$"
cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

docker run --detach --rm --name "$container" \
  --env POSTGRES_USER=cyber \
  --env POSTGRES_PASSWORD=cyber-test-password \
  --env POSTGRES_DB=cyber_code \
  --publish 127.0.0.1::5432 \
  postgres:17-alpine >/dev/null

attempt=0
while [ "$attempt" -lt 30 ]; do
  if docker exec "$container" pg_isready --username cyber --dbname cyber_code >/dev/null 2>&1; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done
if [ "$attempt" -eq 30 ]; then
  echo "PostgreSQL did not become ready" >&2
  exit 1
fi

port=$(docker port "$container" 5432/tcp | sed -n 's/.*://p' | head -n 1)
if [ -z "$port" ]; then
  echo "Unable to resolve PostgreSQL port" >&2
  exit 1
fi

CYBER_CODE_TEST_POSTGRES_DSN="postgres://cyber:cyber-test-password@127.0.0.1:${port}/cyber_code?sslmode=disable" \
GOCACHE="${GOCACHE:-/private/tmp/cyber-code-build-cache}" \
go test ./internal/authorization -run PostgreSQLIntegration -count=1 -v

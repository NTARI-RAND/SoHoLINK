#!/bin/sh
# Create the soholink_test database in the local Docker Compose postgres
# container for running integration tests. Idempotent — safe to re-run.
set -e

cd "$(dirname "$0")/.."

EXISTS=$(docker compose exec -T postgres psql -U postgres -tAc \
  "SELECT 1 FROM pg_database WHERE datname = 'soholink_test'")

if [ "$EXISTS" = "1" ]; then
  echo "database soholink_test already exists — nothing to do"
else
  docker compose exec -T postgres psql -U postgres -c "CREATE DATABASE soholink_test;"
  echo "created database soholink_test"
fi

echo ""
echo "Next: export TEST_DATABASE_URL in your shell."
echo "Connection string template:"
echo "  postgres://postgres:<POSTGRES_PASSWORD>@localhost:5432/soholink_test?sslmode=disable"
echo "where <POSTGRES_PASSWORD> is the value from .env."

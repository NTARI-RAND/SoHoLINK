# Test Database Setup

Integration tests require a dedicated Postgres database. They must never run
against the production database or the default `postgres` database.

## Why a dedicated database

Six test files issue `TRUNCATE` (or bulk `DELETE`) at the start of every
test run. Running against the production database destroys live data.
All six files enforce this guard at connection time:

- `internal/api/nodes_test.go`
- `internal/orchestrator/orchestrator_integration_test.go`
- `internal/portal/testhelpers_test.go`
- `internal/store/payouts_test.go`
- `internal/store/uptime_test.go`
- `test/integration/phase1_test.go`

The guard queries `current_database()` and fatals if the database name does
not contain `"test"`. Setting `TEST_DATABASE_URL` to a database named
`soholink_test` satisfies this check.

## Local setup

Requires the Docker Compose postgres container to be running. Run the setup
script once:

```bash
bash scripts/setup-test-db.sh
```

The script creates `soholink_test` inside the container (idempotent — safe
to re-run). Set `TEST_DATABASE_URL` in your shell, substituting the
`POSTGRES_PASSWORD` value from `.env`:

```
export TEST_DATABASE_URL=postgres://postgres:<POSTGRES_PASSWORD>@localhost:5432/soholink_test?sslmode=disable
```

Then run integration tests:

```bash
go test -v -tags integration -count=1 -p 1 ./...
```

## CI

The GitHub Actions workflow creates `soholink_test` automatically via the
`POSTGRES_DB: soholink_test` service env variable and passes
`TEST_DATABASE_URL` to the integration test step. No manual setup is needed
in CI.

## Production database

`DATABASE_URL` is used exclusively by the production binaries
(`cmd/portal/main.go`, `cmd/orchestrator/main.go`). It is never read by
any test file.

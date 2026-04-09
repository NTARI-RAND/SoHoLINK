# SoHoLINK Load Tests

Load tests are written for [k6](https://k6.io/).

## Install k6

```bash
# macOS
brew install k6

# Windows
choco install k6

# Linux
sudo apt install k6
```

## Prerequisites

**1. Seed the database**

```bash
DATABASE_URL="postgres://postgres:changeme@localhost:5432/postgres?sslmode=disable" \
  go run ./cmd/seed/
```

The seed program inserts 10 providers, 10 nodes with default resource profiles,
and 10 consumers — and sets `password_hash` on all consumers to a bcrypt hash
of `changeme`. Load tests work out of the box after seeding.

**2. Start the portal**

```bash
DATABASE_URL=... SESSION_PRIVATE_KEY=... STRIPE_SECRET_KEY=... \
  PORTAL_ADDR=:8080 PORTAL_BASE_URL=http://localhost:8080 \
  PORTAL_TEMPLATES_DIR=web/templates ORCHESTRATOR_TOKEN_SECRET=... \
  METRICS_ADDR=:9090 \
  go run ./cmd/portal/
```

## Run the tests

```bash
# Marketplace throughput (50 VUs, 30s)
BASE_URL=http://localhost:8080 k6 run deploy/loadtest/marketplace.js

# Override credentials
BASE_URL=http://localhost:8080 \
  CONSUMER_EMAIL=consumer-01@seed.internal \
  CONSUMER_PASSWORD=changeme \
  k6 run deploy/loadtest/marketplace.js

# Login rate limiter (10 VUs, 60s)
BASE_URL=http://localhost:8080 k6 run deploy/loadtest/login.js
```

## Thresholds

| Script | Threshold |
|---|---|
| `marketplace.js` | p95 response time < 500ms, error rate < 1% |
| `login.js` | Zero 5xx responses (401 and 429 are expected and pass) |

## View results in Grafana

Import `deploy/grafana/job-activity.json` and `deploy/grafana/network-health.json`
into Grafana (see `deploy/README.md` Step 7). k6 can stream live metrics to
InfluxDB or Prometheus remote-write for real-time dashboard integration.

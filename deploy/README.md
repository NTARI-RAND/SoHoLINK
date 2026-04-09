# SoHoLINK v2 — Deployment Guide

## Prerequisites

1. Ubuntu 22.04 LTS on the target server
2. SSH key pair at `~/.ssh/soholink_deploy` (or update `deploy/ansible/inventory.yml`)
3. Ansible installed on the control machine: `pip install ansible`
4. Domain DNS A record pointing to the server IP

---

## Step 1 — Update inventory

Edit `deploy/ansible/vars.yml` and replace `192.168.1.x` with the actual server IP.

---

## Step 2 — Create the secrets file on the server

The Ansible playbook writes non-secret env vars automatically, but secrets must exist before the portal starts. SSH into the server and create the file manually:

```bash
sudo mkdir -p /etc/soholink
sudo tee /etc/soholink/portal.env > /dev/null <<EOF
DATABASE_URL=postgres://user:password@localhost:5432/soholink?sslmode=disable
SESSION_PRIVATE_KEY=<128 hex chars — see generation command below>
STRIPE_SECRET_KEY=sk_live_...
ORCHESTRATOR_TOKEN_SECRET=<64 hex chars — run: openssl rand -hex 32>
EOF
sudo chmod 640 /etc/soholink/portal.env
sudo chown root:soholink /etc/soholink/portal.env
```

Required variables:

| Variable | Description |
|---|---|
| `DATABASE_URL` | PostgreSQL connection string |
| `SESSION_PRIVATE_KEY` | Ed25519 private key, 64 bytes as 128 hex chars — generate with the command below |
| `STRIPE_SECRET_KEY` | Stripe live secret key (`sk_live_...`) — also required for payout release (`store.RunPayoutReleaser`) |
| `ORCHESTRATOR_TOKEN_SECRET` | 32 random bytes as hex — `openssl rand -hex 32` |

To generate `SESSION_PRIVATE_KEY` (seed \|\| public key, 128 hex chars):

```bash
KEY=$(mktemp); openssl genpkey -algorithm ed25519 -out $KEY 2>/dev/null; \
  printf "%s%s\n" \
    "$(openssl pkey -in $KEY -outform DER | tail -c 32 | xxd -p -c 32)" \
    "$(openssl pkey -in $KEY -pubout -outform DER | tail -c 32 | xxd -p -c 32)"; \
  rm $KEY
```

---

## Step 2b — Create the orchestrator secrets file

```bash
sudo tee /etc/soholink/orchestrator.env > /dev/null <<EOF
DATABASE_URL=<same as portal>
ORCHESTRATOR_TOKEN_SECRET=<same as portal>
API_ADDR=:8443
METRICS_ADDR=:9091
SPIFFE_ENDPOINT_SOCKET=unix:///tmp/spire-agent/public/api.sock
EOF
sudo chmod 640 /etc/soholink/orchestrator.env
sudo chown root:soholink /etc/soholink/orchestrator.env
```

---

## Step 3 — Create the agent secrets file

```bash
sudo tee /etc/soholink/agent.env > /dev/null <<EOF
AGENT_NODE_ID=<UUID — run: uuidgen>
AGENT_PROVIDER_ID=<UUID of the provider record in the database>
AGENT_NODE_CLASS=A
AGENT_COUNTRY_CODE=US
AGENT_CONTROL_PLANE_ADDR=https://soholink.ntari.org:8443
SPIFFE_ENDPOINT_SOCKET=unix:///tmp/spire-agent/public/api.sock
AGENT_TOKEN_SECRET=<same value as ORCHESTRATOR_TOKEN_SECRET>
EOF
sudo chmod 640 /etc/soholink/agent.env
sudo chown root:soholink /etc/soholink/agent.env
```

---

## Step 4 — Run the playbook

```bash
cd deploy/ansible
ansible-playbook -i inventory.yml playbook.yml --ask-become-pass
```

---

## Step 5 — Issue TLS certificate

After NGINX is running:

```bash
certbot --nginx -d soholink.ntari.org
```

---

## Step 6 — Verify

```bash
systemctl status soholink-portal
systemctl status soholink-orchestrator
curl -s http://localhost:9090/metrics | head -20
curl -s http://localhost:9091/metrics | head -20
curl -I https://soholink.ntari.org
```

---

## Step 7 — Import Grafana dashboards

Two dashboard definitions are in `deploy/grafana/`. Import them via the Grafana UI:

1. Open Grafana → Dashboards → Import
2. Upload `deploy/grafana/network-health.json`
3. Select your Prometheus datasource when prompted
4. Repeat for `deploy/grafana/job-activity.json`

Dashboards refresh every 30 seconds. The Prometheus datasource must be named
exactly `Prometheus` or edit the `datasource` field in each JSON before importing.

---

## Security note

`deploy/ansible/vars.yml` contains only non-sensitive configuration (addresses, paths, domain).
All secrets live exclusively in `/etc/soholink/*.env` on the server — never committed to git.

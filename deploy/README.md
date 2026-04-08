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
SESSION_SECRET=<64 hex chars — run: openssl rand -hex 32>
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
| `SESSION_SECRET` | 32 random bytes as hex — `openssl rand -hex 32` |
| `STRIPE_SECRET_KEY` | Stripe live secret key (`sk_live_...`) |
| `ORCHESTRATOR_TOKEN_SECRET` | 32 random bytes as hex — `openssl rand -hex 32` |

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
curl -s http://localhost:9090/metrics | head -20
curl -I https://soholink.ntari.org
```

---

## Security note

`deploy/ansible/vars.yml` contains only non-sensitive configuration (addresses, paths, domain).
All secrets live exclusively in `/etc/soholink/*.env` on the server — never committed to git.

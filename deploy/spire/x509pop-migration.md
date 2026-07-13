# SPIRE join_token → x509pop migration (TODO 36)

**Why.** `join_token` is single-use: it is consumed at first attestation and the
SVID is cached. If the SVID expires while the agent is down (a reboot, a
multi-hour outage) the agent restarts, tries to re-attest with the already-used
token, and crash-loops on *"join token does not exist or has already been
used"* — dragging the orchestrator into SPIFFE-degraded mode. This recurred
2026-07-09 and twice on 2026-07-13, each needing a manual token-rotation +
workload-entry re-registration.

**Fix.** `x509pop` derives the agent's SPIFFE ID from a leaf-certificate
fingerprint. The ID is **stable across restarts** and there is **no consumable
token** — the crash-loop cannot happen. The config + compose in this repo are
already switched to x509pop (server keeps `join_token` as a fallback attestor);
this runbook is the one-time host-side apply.

Certs are host-local trust material (like `.env`), never committed:
`deploy/spire/x509pop/` is gitignored. `SPIRE_X509POP_DIR` overrides the path.

## 1 — Generate the CA + agent cert (once, on the host)

From the repo root, in Git Bash. `MSYS_NO_PATHCONV=1` stops MSYS mangling the
openssl `-subj`.

```sh
mkdir -p deploy/spire/x509pop && cd deploy/spire/x509pop
export MSYS_NO_PATHCONV=1
openssl req -x509 -newkey rsa:2048 -nodes -keyout ca.key -out ca.crt -days 3650 \
  -subj "/O=NTARI/CN=SoHoLINK SPIRE x509pop CA"
openssl req -newkey rsa:2048 -nodes -keyout agent.key -out agent.csr \
  -subj "/O=NTARI/CN=soholink-spire-agent-host"
openssl x509 -req -in agent.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out agent.crt -days 1825
openssl verify -CAfile ca.crt agent.crt   # must print: agent.crt: OK
cd -
```

Keep `ca.key` safe (it can mint new agent certs). `agent.key` is the agent's
identity — 0600, never leaves the host.

## 2 — Apply (recreate the SPIRE pair, then re-anchor the orchestrator)

The agent's SPIFFE ID changes (join_token/<token> → x509pop/<fingerprint>), so
the orchestrator workload entry must be re-registered under the new parent.

```sh
cd "<repo>"
docker compose up -d --force-recreate spire-server spire-agent
# wait for the agent to attest, then read its new stable ID:
MSYS_NO_PATHCONV=1 docker compose exec spire-server \
  /opt/spire/bin/spire-server agent list \
  -socketPath /run/spire-server/private/api.sock
# -> note the SPIFFE ID: spiffe://soholink.org/spire/agent/x509pop/<hash>

# register the orchestrator entry under the new parent (uid:0 selector):
MSYS_NO_PATHCONV=1 docker compose exec spire-server \
  /opt/spire/bin/spire-server entry create \
  -socketPath /run/spire-server/private/api.sock \
  -parentID "spiffe://soholink.org/spire/agent/x509pop/<hash>" \
  -spiffeID "spiffe://soholink.org/orchestrator" \
  -selector unix:uid:0 -ttl 3600

docker compose up -d --force-recreate orchestrator
```

## 3 — Verify (including the thing join_token failed at)

```sh
curl -s https://api.soholink.org/health          # {"identity":"ready","status":"ok"}
# the actual fix: restart the agent and confirm it re-attests with NO token:
docker compose restart spire-agent
docker compose logs --since 1m spire-agent | grep -i attest   # succeeds, no "token" error
curl -s https://api.soholink.org/health          # still ready
```

`SPIRE_AGENT_JOIN_TOKEN` in `.env` is now unused (the server keeps the
`join_token` attestor only as a break-glass fallback). Old join_token workload
entries can be pruned with `spire-server entry delete -entryID <id>`.

## Rollback (to the previous join_token model)

```sh
git checkout <prev> -- deploy/spire/server.conf deploy/spire/agent.conf docker-compose.yml
# generate a fresh token, set SPIRE_AGENT_JOIN_TOKEN in .env, then:
docker compose up -d --force-recreate spire-server spire-agent
bash deploy/register-entries.sh
docker compose up -d --force-recreate orchestrator
```

Blast radius of a bad apply is bounded: a failed attestation only puts the
orchestrator in degraded mode (public site stays up; SPIFFE-protected node
routes 503) — recover by rollback. Do the apply as a deliberate step, not
inside another change.

# SoHoLINK — Federated SOHO Compute Marketplace

[![Go Version](https://img.shields.io/badge/Go-1.25.7-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-AGPL%203.0-blue.svg)](LICENSE.txt)
[![Build](https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/actions/workflows/build.yml/badge.svg)](https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/actions/workflows/build.yml)

**Aggregate the idle compute power in your home or office. Earn real payments for sharing what you already own.**

SoHoLINK is a federated compute marketplace for Small Office/Home Office (SOHO) hardware. It turns underutilized desktops, NAS devices, mini-PCs, and mobile devices into income-generating compute nodes — connected through a Kubernetes-inspired scheduler, settled via Stripe and Lightning, and governed by Open Policy Agent rules the provider fully controls.

---

## What It Does

| For Providers | For Requesters |
|---|---|
| Earn per hour of CPU/storage contributed | Browse available SOHO compute nodes — filter by CPU, region, GPU, price, reputation |
| Set pricing and resource limits in a wizard | Top up a prepaid satoshi wallet via Lightning invoice or Stripe |
| Accept or auto-accept rental requests via OPA policy | Get instant cost estimates in sats and USD before committing |
| Watch earnings accumulate in real time on the dashboard | Launch workloads in one API call — wallet debited + workload submitted atomically |
| Visualize the network on a 3D globe | Track and cancel orders; proportional refund for unused time |

---

## Features

- **Hardware Discovery** — Detects CPU, GPU, RAM, storage, and network across Windows, Linux, and macOS at startup; computes a fair per-hour provider rate automatically
- **Federated Scheduler (FedScheduler)** — Kubernetes-inspired workload placement across independent SOHO nodes; supports placement constraints, auto-scaling, and health monitoring
- **IPFS Storage Pool** — Content-addressed data movement via a local Kubo daemon; inputs pinned as CIDs, outputs returned as CIDs, no central file server
- **Dual-Rail Payments** — Stripe for card payments; Lightning Network for sub-cent micropayments; 1% platform fee, ~97% payout to providers
- **OPA Policy Governance** — Providers express resource-sharing rules in Rego (max CPU share, bandwidth limits, requester reputation thresholds); auto-accept engine enforces them
- **Per-Hour Metering** — Billing loop charges requesters per hour of actual usage; `FedScheduler` feeds live placement data to the `UsageMeter` goroutine; Lightning hold invoices (HTLC) release payment only after result verification; provider payouts via `POST /api/revenue/request-payout` with full history at `GET /api/revenue/payouts`
- **ML-Driven Scheduling** — `LinUCBBandit` contextual bandit replaces round-robin node selection in `ScheduleMobile`; per-node UCB scores learned from HTLC settle/cancel outcomes; `TelemetryRecorder` streams JSONL dispatch events to disk for offline training; falls back to uniform random if no bandit is wired
- **Setup Wizard (8 steps)** — Guided onboarding: hardware review → pricing → network → payments → K8s edges → IPFS → provisioning limits → policies
- **Dashboard (8 tabs)** — Overview, Hardware, Orchestration, Storage, Billing, Users, Policies, Logs; all live data, no page refresh needed
- **Federation & Marketplace Settings** — Dedicated settings dialog (Settings → Federation & Marketplace…) covers coordinator role, fee %, coordinator URL, region, CPU-hour pricing, and heartbeat interval; supports both private cooperative groups and open-marketplace participation from the same UI
- **3D Globe Visualization** — WebSocket-connected Three.js globe; topology mode and geographic mode (real lat/lon from node metadata); animated data flow arcs
- **Zero-Dependency Installers** — Statically linked Windows `.exe` + NSIS setup wizard; macOS universal `.pkg`; Linux `.deb`, `.rpm`, AppImage — produced by GoReleaser in one command
- **P2P LAN Mesh Discovery** — Nodes on the same network automatically find each other via Ed25519-signed multicast UDP announcements (group `239.255.42.99:7946`; RFC 2365, stays on LAN); discovered peers instantly visible to the scheduler and accessible via `GET /api/peers`; topology follows the Watts–Strogatz small-world model — dense local clusters, short global paths
- **Buyer-Side Marketplace** — Full requester experience: browse provider nodes (filter by CPU, region, GPU, price, reputation); get real-time cost estimates in sats + USD; top up a prepaid satoshi wallet via Lightning or Stripe; purchase and launch workloads in a single atomic call; track and cancel orders with proportional refund — all from REST API or Flutter app
- **Mobile Participation** *(Go prerequisites complete; native apps in development)* — Android TV always-on compute node; Android "Earn While Charging" client; iOS monitoring and management app

---

## Quick Start

### Download a Release (Recommended)

Go to [Releases](https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/releases) and download the installer for your platform:

- **Windows:** `SoHoLINK-Setup.exe` (NSIS installer — no runtime dependencies)
- **macOS:** `SoHoLINK-*.pkg` (universal binary — Intel + Apple Silicon)
- **Linux:** `FedAAA-*-x86_64.AppImage` or `.deb` / `.rpm`

### Build from Source

```bash
# Prerequisites: Go 1.24.6, GCC/MinGW (for GUI), git

# Clone
git clone https://github.com/NetworkTheoryAppliedResearchInstitute/soholink
cd soholink

# Build headless CLI (no CGO required)
make build-cli

# Build GUI (requires CGO + GCC; on Windows: MinGW via winget install msys2)
make build-gui

# Run setup wizard (first launch)
./bin/soholink

# All-platform release packages (requires goreleaser)
make dist
```

> **Windows GUI note:** Fyne requires CGO + MinGW.
> `winget install msys2` → open MSYS2 terminal → `pacman -S mingw-w64-x86_64-gcc`
> Add `C:\msys64\mingw64\bin` to your PATH, then `make build-gui`.

---

## Architecture Overview

```
                        ┌──────────────────────────────────────┐
                        │         SoHoLINK Node (SOHO)         │
                        │                                      │
  Requester             │  ┌─────────────┐  ┌──────────────┐  │
  (submits workload) ──►│  │ FedScheduler│  │  HTTP API    │  │
                        │  │  (Go)       │  │  (REST)      │  │
                        │  └──────┬──────┘  └──────────────┘  │
                        │         │                            │
                        │  ┌──────▼──────────────────────┐    │
                        │  │        OPA Policies          │    │
                        │  │  (resource_sharing.rego)     │    │
                        │  └──────┬──────────────────────┘    │
                        │         │                            │
                        │  ┌──────▼──────┐  ┌──────────────┐  │
                        │  │  IPFS Pool  │  │  Payment     │  │
                        │  │  (storage)  │  │  Stripe/LN   │  │
                        │  └─────────────┘  └──────────────┘  │
                        │                                      │
                        │  ┌────────────────────────────────┐  │
                        │  │   Fyne GUI Dashboard           │  │
                        │  │   + 3D Globe (WebSocket)       │  │
                        │  └────────────────────────────────┘  │
                        └──────────────────────────────────────┘
                                         │
                              Federation (WebSocket/P2P)
                                         │
              ┌──────────────────────────┼──────────────────────────┐
              │                          │                          │
   ┌──────────▼──────────┐   ┌──────────▼──────────┐   ┌──────────▼──────────┐
   │  SOHO Node (Linux)  │   │  SOHO Node (macOS)  │   │ Android TV Node     │
   │  mini-PC / NAS      │   │  iMac / MacBook     │   │ (always-on mobile)  │
   └─────────────────────┘   └─────────────────────┘   └─────────────────────┘
```

### Key Subsystems

| Package | Role |
|---|---|
| `internal/p2p/` | Small-world LAN mesh: signed multicast UDP discovery, Ed25519 peer auth, `/api/peers` |
| `internal/orchestration/` | FedScheduler, auto-scaler, node discovery, mobile scheduling, K8s edge adapter |
| `internal/ml/` | Pure-Go ML primitives: `LinUCBBandit`, `TelemetryRecorder`, dimension constants |
| `internal/httpapi/` | 46+ REST endpoints + WebSocket hub; per-IP rate limiting; Ed25519 federation auth; marketplace + wallet handlers |
| `internal/storage/` | Local content-addressed pool + IPFS HTTP client (Kubo) |
| `internal/payment/` | Stripe + Lightning processors, HTLC hold invoices, metering loop, ledger, settler, payout system |
| `internal/notification/` | APNs push notification client (iOS — JWT auth, auto-refresh) |
| `internal/wasm/` | Wasm task executor interface + stub (wazero implementation: v0.3) |
| `internal/rental/` | Auto-accept engine for incoming resource requests |
| `internal/wizard/` | Hardware detection, cost calculator, pricing, policy config |
| `internal/gui/dashboard/` | Fyne dashboard (~1,750 lines): 8 tabs, 8 settings dialogs, 8-step wizard |
| `internal/central/` | 1% platform fee calculation and ledger recording |
| `internal/store/` | SQLite via `modernc.org/sqlite`; all persistent state |
| `internal/lbtas/` | Trust and reputation scoring |
| `internal/blockchain/` | Tamper-evident Merkle chain for accounting logs |
| `configs/policies/` | OPA Rego policies for resource sharing and governance |
| `ui/globe-interface/` | Three.js 3D globe, WebSocket bridge to DDS graph |

---

## Node Participation Tiers

SoHoLINK supports a spectrum of hardware from always-on servers to mobile devices:

| Tier | Hardware | Role | Constraints |
|---|---|---|---|
| **Full** | Desktop, mini-PC, NAS, server | Compute worker + storage node + scheduler peer | None |
| **Partial** | Laptop | Compute worker when plugged in | Suspend awareness |
| **Headless mobile** | Android TV / Fire TV box | Compute worker (always-on, no battery) | ARM64 tasks |
| **Mobile (Android)** | Smartphone / tablet | Short-burst compute + storage relay | Plugged in + WiFi; tasks ≤120 s |
| **Monitoring (iOS)** | iPhone / iPad | Earnings dashboard + management client | No background compute |

> See [`docs/MOBILE_INTEGRATION.md`](docs/MOBILE_INTEGRATION.md) for the full mobile participation roadmap.

---

## Build Targets

| Make target | Output | Notes |
|---|---|---|
| `make build-cli` | `bin/soholink` | Headless CLI, no CGO required |
| `make build-gui` | `bin/soholink-gui` | Full GUI, requires CGO + GCC |
| `make build-static-windows` | `bin/soholink.exe` | Statically linked, zero DLL deps |
| `make fyne-package-windows` | `SoHoLINK.exe` | Fyne-bundled with manifest + icon |
| `make dist` | `dist/` | All platforms via GoReleaser (snapshot) |
| `make dist-release` | `dist/` + GitHub Release | Requires `git tag v*` |
| `make test` | — | Race detector + coverage |
| `make help` | — | Full target reference |

---

## Payment Flow

```
Requester pays $1.00
        │
        ▼ Stripe processes
   $0.029 + $0.30 processor fee → Stripe
        │
        ▼ Net ≈ $0.671
   $0.00671 (1%) → SoHoLINK platform
        │
        ▼
   $0.664 (99% of net) → Provider
```

Lightning Network payments skip the Stripe fee entirely, making sub-cent micropayments economical for short tasks.

---

## Security

- **Ed25519 Federation Authentication** — every provider node signs its announce and heartbeat
  messages with its Ed25519 private key; the coordinator verifies the signature against the
  stored public key before accepting resource updates or heartbeats; forged registrations return
  HTTP 401
- **Rate Limiting on All Mutating Endpoints** — federation announce/deregister: 5 req/IP/min;
  heartbeat: 10 req/IP/min; mobile WebSocket upgrades: 30 req/IP/min; mobile register:
  20 req/IP/min; implemented via a per-IP sliding-window `ipRateLimiter`
- **OPA-enforced resource limits** — providers cannot be exploited beyond their declared policy
- **TLS 1.2+ minimum** on all K8s edge connections and LND Lightning connections; x509 cert
  pinning on LND via configurable `lnd_tls_cert_path` (PEM CA bundle)
- **HTLC payment gating** — Lightning hold invoices; payment only releases after result verification
- **Result replication** — mobile node results verified against a second node before settlement
- **Tamper-evident accounting** — SHA3-256 Merkle chain over all billing events
- **Startup config validation** — non-fatal warnings at startup for common misconfigurations:
  default RADIUS secret, missing payment processor credentials, coordinator without HTTP API,
  empty node DID, and LND without certificate pinning

---

## Documentation

| Document | Description |
|---|---|
| [BUILD.md](BUILD.md) | Build instructions, cross-compilation, CGO/MinGW setup |
| [docs/INSTALL.md](docs/INSTALL.md) | Installation and first-run guide |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Full system design and component reference |
| [docs/TESTING.md](docs/TESTING.md) | Running tests and verifying deployments |
| [docs/OPERATIONS.md](docs/OPERATIONS.md) | Day-to-day node management |
| [docs/MOBILE_INTEGRATION.md](docs/MOBILE_INTEGRATION.md) | Mobile participation roadmap and implementation plan |
| [docs/research/ML_LOAD_BALANCING.md](docs/research/ML_LOAD_BALANCING.md) | ML scheduling research: RL, bandits, GNNs, LSTM, anomaly detection |
| [docs/research/SOHOLINK_CAPABILITIES.md](docs/research/SOHOLINK_CAPABILITIES.md) | Current capability assessment |
| [docs/research/MOBILE_PARTICIPATION.md](docs/research/MOBILE_PARTICIPATION.md) | Research: can mobile devices participate? |

---

## Dependencies

**Core:**
- [`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite) — Pure Go SQLite (no CGO required for CLI)
- [`fyne.io/fyne/v2`](https://fyne.io) — Cross-platform GUI toolkit (requires CGO for GUI build)
- [`github.com/open-policy-agent/opa`](https://github.com/open-policy-agent/opa) — Policy engine (Rego)
- [`github.com/shirou/gopsutil`](https://github.com/shirou/gopsutil) — Cross-platform hardware metrics
- [`github.com/stripe/stripe-go`](https://github.com/stripe/stripe-go) — Stripe payment processing
- [`github.com/gorilla/websocket`](https://github.com/gorilla/websocket) — WebSocket hub for mobile node connections

**Observability:**
- `go.opentelemetry.io/otel` — Distributed tracing
- `github.com/prometheus/client_golang` — Metrics

**Crypto:**
- `golang.org/x/crypto` — Ed25519, SHA3-256
- `github.com/decred/dcrd/dcrec/secp256k1` — Secp256k1 for Lightning

All dependencies are vendored (`vendor/`) for reproducible, offline builds.

---

## License

AGPL-3.0 — See [LICENSE.txt](LICENSE.txt) for details.

This project is licensed under the GNU Affero General Public License v3.0, ensuring that all modifications remain open source and accessible to the community, supporting the federation sovereignty principles of SoHoLINK.

---

## Mobile App

The Flutter mobile app (`mobile/flutter-app/`) lets you **monitor your node** and **buy compute** from any phone or browser on your network. It includes a full buyer-side marketplace: browse provider nodes, configure workloads, estimate costs, top up your wallet, and launch jobs — all without leaving the app.

### Changing the Node Address

The app ships with the node address hardcoded for zero-setup access. To point it at a different machine, edit **one line**:

```dart
// mobile/flutter-app/lib/api/soholink_client.dart  (line ~17)
const kNodeUrl = 'http://192.168.1.220:4000';
//                        ^^^^^^^^^^^^ change to your node's LAN IP
//                                     ^^^^ change port if needed (fedaaa default: 8080)
```

After editing, rebuild the web bundle:

```bash
cd mobile/flutter-app
flutter build web
# then restart the mock/preview server, or point to your live fedaaa instance
```

### Running the Preview Server (mock data)

```bash
# From the repo root — serves Flutter web + mock API on port 4000
python mobile/flutter-app/mock_server.py
```

Open `http://<your-LAN-IP>:4000` on any device on the same Wi-Fi.

### Connecting to a Live Node

Point `kNodeUrl` at your running `fedaaa` instance (default port 8080).
The node must expose these REST endpoints:

| Endpoint | Description |
|---|---|
| `GET /api/health` | Returns `{"status":"ok"}` — used to verify connectivity |
| `GET /api/status` | Node metrics including `btc_usd_rate` for USD conversion |
| `GET /api/peers` | Federation peer list |
| `GET /api/revenue` | Earnings history and totals |
| `GET /api/workloads` | Active rental workloads |
| `GET /api/marketplace/nodes` | Browse available provider nodes (buyer flow) |
| `POST /api/marketplace/estimate` | Estimate workload cost in sats + USD |
| `POST /api/marketplace/purchase` | Buy and launch a workload |
| `GET /api/wallet/balance` | Prepaid wallet balance |
| `POST /api/wallet/topup` | Create Lightning invoice or Stripe intent to top up |
| `GET /api/orders` | Order history |
| `POST /api/orders/{id}/cancel` | Cancel order and receive proportional refund |

### Currency Display

All earnings are shown in **USD**, converted from satoshis using the live BTC/USD rate the node returns in `btc_usd_rate`. To change currency symbol or locale, update the `NumberFormat.currency(symbol: '\$', ...)` calls in:
- `lib/pages/dashboard_page.dart`
- `lib/pages/revenue_page.dart`
- `lib/pages/workloads_page.dart`
- `lib/pages/order_page.dart` (cost estimate and wallet balance cards)

---

## Contributing

This project is part of the **Network Theory Applied Research Institute's** work on community computing infrastructure. Contributions welcome — open an issue or pull request.

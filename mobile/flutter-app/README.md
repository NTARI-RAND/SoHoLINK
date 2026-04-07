# SoHoLINK Node Dashboard — Flutter App

Native app for monitoring and managing your SoHoLINK federated edge node.
Built in pure Flutter/Dart — no visual editors required.

## Platform Support

| Platform | Status | Notes |
|----------|--------|-------|
| Android phone | ✅ Full | Portrait, WebSocket, FCM push |
| Android TV | ✅ Full | NavigationRail, D-pad, LEANBACK_LAUNCHER |
| Amazon Fire TV | ✅ Full | AMAZON_TV_LAUNCHER, same APK as Android TV |
| Samsung Tizen | ✅ Web agent | `/tv` endpoint in-browser, or `.wgt` package |
| LG webOS | ✅ Web agent | `/tv` endpoint in-browser, or `.ipk` package |
| iOS | 🔜 Planned | APNs notifier already wired in Go backend |
| Roku | ❌ Not supported | BrightScript has no background execution |

## Features

| Tab | Description |
|-----|-------------|
| **Dashboard** | Node health, uptime, active rentals, and resource gauges |
| **Peers** | LAN-mesh and mobile federation peers with latency and status |
| **Revenue** | Daily earnings in sats, 30-day bar chart, fee breakdown |
| **Workloads** | Active and real-time pushed workloads via WebSocket |
| **Settings** | Node URL configuration and connection test |

## Prerequisites

| Tool | Min Version | Install |
|------|-------------|---------|
| Flutter SDK | 3.19.0 | https://flutter.dev/docs/get-started/install |
| Dart SDK | 3.3.0 | Bundled with Flutter |
| Android SDK | API 21+ | Via Android Studio or `sdkmanager` |
| Running `fedaaa` node | any | `go run ./cmd/fedaaa start` |

## Quick Start

```bash
# 1. Navigate to the app directory
cd mobile/flutter-app

# 2. Install dependencies
flutter pub get

# 3. Connect a device / start emulator, then run
flutter run

# Build release APK (phone + Android TV + Fire TV)
flutter build apk --release
```

## Connecting to Your Node

On first launch the **Setup** screen appears. Enter your node's URL:

| Environment | URL |
|---|---|
| Android emulator | `http://10.0.2.2:8080` |
| Same Wi-Fi | `http://192.168.1.<your-ip>:8080` |
| USB-tethered phone | `http://192.168.42.1:8080` (typical) |

The app sends a `GET /api/health` ping to validate the connection before saving.

## WebSocket Node Mode

After initial setup the app opens a persistent WebSocket connection to
`ws[s]://<node>/ws/nodes`. This connection:

- Registers the device as a compute node (`mobile-android` or `android-tv`).
- Receives task descriptors pushed by the scheduler in real time.
- Sends heartbeat frames every 25 s.
- Auto-reconnects with exponential backoff (1 s → 2 s → … → 60 s cap) on
  disconnect.

## FCM Push Notifications

Android and Android TV builds support Firebase Cloud Messaging (FCM) for
background wakeup. When the WebSocket is disconnected and the scheduler needs
to dispatch a task, the Go backend sends a silent FCM data message that wakes
the app and triggers a reconnect.

### Server-side setup

Set two environment variables before starting the node:

```bash
export SOHOLINK_FCM_PROJECT_ID="my-firebase-project"
export SOHOLINK_FCM_SERVICE_ACCOUNT_JSON="$(cat service-account.json)"
```

`service-account.json` is the key file downloaded from the Firebase Console →
Project Settings → Service Accounts.

### Client-side setup

Place your app's `google-services.json` at:

```
android/app/google-services.json
```

**This file is gitignored** — inject it at CI/CD build time. Do not commit it.

> FCM requires Google Play Services. Fire TV OS devices typically do not have
> Google Play — on Fire TV the FCM token registration silently no-ops and the
> scheduler falls back to the desktop node path.

## Android TV Build + Sideload

```bash
# Build APK
flutter build apk --release

# Connect Android TV emulator (or physical TV via ADB over network)
adb connect <TV-IP>:5555

# Install
adb install build/app/outputs/flutter-apk/app-release.apk
```

The app detects Android TV automatically (checks `android.software.leanback`
feature flag) and switches to the NavigationRail layout with D-pad navigation.

### D-pad Controls

| Key | Action |
|-----|--------|
| ↑ / ↓ | Move focus in content area |
| ← / → | Cycle navigation rail items |
| → (from rail) | Move focus into content panel |
| Enter / Select | Activate focused element |

## Fire TV Build + Sideload

Same APK as Android TV — no separate build needed.

```bash
# Enable ADB debugging on Fire TV: Settings → My Fire TV → Developer Options
adb connect <fire-tv-ip>:5555
adb install build/app/outputs/flutter-apk/app-release.apk
```

The app appears in **Your Apps and Channels** on the Fire TV home screen
(via the `amazon.intent.category.AMAZON_TV_LAUNCHER` intent filter).

## Tizen / webOS TV (Web Agent)

For Samsung Tizen and LG webOS TVs, navigate the TV's browser to:

```
http://<node-ip>:8080/tv
```

The TV agent auto-connects via WebSocket, registers as `android-tv` class,
and shows connection status and metrics. It cannot execute compute workloads —
the scheduler falls back automatically.

For packaging as a native `.wgt` (Tizen) or `.ipk` (webOS) app, see
[`docs/tv-agent-packaging.md`](../../docs/tv-agent-packaging.md).

## Project Layout

```
lib/
  main.dart                   ← Entry point; TV detection, Firebase init
  theme/
    app_theme.dart            ← Dark + TV themes (1.5× text scale for TV)
  api/
    soholink_client.dart      ← HTTP client singleton, wsUrl getter
  models/
    node_status.dart          ← /api/status DTO
    peer_info.dart            ← /api/peers DTO
    revenue.dart              ← /api/revenue DTO
    workload.dart             ← /api/workloads DTO
    task.dart                 ← WebSocket wire types (MobileNodeInfo, etc.)
  services/
    websocket_service.dart    ← Persistent WS connection, heartbeat, backoff
    fcm_service.dart          ← FCM token registration, notification handlers
  widgets/
    stat_card.dart            ← Metric tile (D-pad focusable on TV)
    resource_bar.dart         ← CPU/RAM/disk/net progress bar
    section_header.dart       ← Labelled section divider
    status_dot.dart           ← Animated health indicator
  pages/
    setup_page.dart           ← First-run URL entry
    home_page.dart            ← Adaptive shell: NavigationBar (phone) / NavigationRail (TV)
    dashboard_page.dart       ← Overview tab
    peers_page.dart           ← Federation peers tab
    revenue_page.dart         ← Earnings + chart tab
    workloads_page.dart       ← Workloads tab (real-time WebSocket stream)
    settings_page.dart        ← Node URL + about tab
android/
  app/src/main/
    AndroidManifest.xml       ← INTERNET, FCM, leanback, Amazon TV launcher
    res/xml/
      network_security_config.xml ← Allows HTTP to RFC-1918 addresses
    res/drawable/
      banner.png              ← 320×180 Fire TV launcher banner
```

## API Endpoints Consumed

| Method | Path | Used by |
|--------|------|---------|
| `GET` | `/api/health` | Setup validation, Settings test |
| `GET` | `/api/status` | Dashboard |
| `GET` | `/api/peers` | Peers |
| `GET` | `/api/revenue` | Revenue |
| `GET` | `/api/workloads` | Workloads |
| `WS` | `/ws/nodes` | WebSocket node registration + task stream |
| `POST` | `/api/v1/nodes/mobile/fcm-token` | FCM token registration |

## Android Release Build

```bash
# Generate a keystore (once)
keytool -genkey -v -keystore soholink.jks -keyAlg RSA -keySize 2048 \
        -validity 10000 -alias soholink

# Build signed APK
flutter build apk --release

# Install on connected device
flutter install
```

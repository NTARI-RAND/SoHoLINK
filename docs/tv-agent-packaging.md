# SoHoLINK TV Agent — Packaging Guide

The TV agent (`/tv`) is a self-contained HTML page served by the SoHoLINK node at
`http[s]://<node-ip>:8080/tv`. For Tizen (Samsung) and webOS (LG) smart TVs the page can
be packaged as a native app so it appears in the TV's launcher instead of requiring the user
to open a browser manually.

---

## Samsung Tizen — `.wgt` Package

### Prerequisites

| Tool | Source |
|------|--------|
| Tizen Studio 5.x | https://developer.samsung.com/smarttv/develop/getting-started/setting-up-sdk/installing-tv-sdk.html |
| Samsung Smart TV certificate | Tizen Studio → Certificate Manager |
| TV in Developer Mode | Settings → Support → About → rapid press Remote button 5× |

### Steps

1. **Export the agent page**

   Copy `tv-agent.html` to a new directory; rename it `index.html`:

   ```bash
   mkdir tizen-app
   cp internal/httpapi/ui/tv-agent.html tizen-app/index.html
   ```

2. **Create `config.xml`**

   Place this file inside `tizen-app/`:

   ```xml
   <?xml version="1.0" encoding="UTF-8"?>
   <widget xmlns="http://www.w3.org/ns/widgets"
           xmlns:tizen="http://tizen.org/ns/widgets"
           id="https://soholink.io/tv-agent"
           version="1.0.0">
     <tizen:application id="SoHoLINKTV.app" package="SoHoLINKTV" required_version="4.0"/>
     <tizen:feature name="http://tizen.org/feature/network.wifi"/>
     <content src="index.html"/>
     <name>SoHoLINK TV Agent</name>
     <icon src="icon.png"/>
     <tizen:privilege name="http://tizen.org/privilege/network.get"/>
     <tizen:privilege name="http://tizen.org/privilege/internet"/>
   </widget>
   ```

3. **Add an icon** (optional)

   Drop a 512×512 PNG named `icon.png` into `tizen-app/`.

4. **Build the `.wgt`**

   ```bash
   # From within Tizen Studio: File → Import → Tizen → Tizen Web Project
   # Right-click project → Build Signed Package
   ```

   Or via CLI (Tizen Studio CLI must be on PATH):

   ```bash
   tizen package -t wgt -s "SoHoLINK" -- tizen-app/
   ```

5. **Install on TV**

   ```bash
   # Pair with TV (TV IP shown in Developer Mode screen)
   sdb connect <TV-IP>

   # Install
   tizen install -n SoHoLINKTV.wgt -t <TV-serial>

   # Launch
   tizen run -p SoHoLINKTV.app -t <TV-serial>
   ```

6. **Point the agent at your node**

   The agent auto-connects to `ws://<origin>/ws/nodes`. For a packaged app `origin`
   is `file://` — this won't match the node. Edit the `wsUrl()` function in
   `tv-agent.html` to hard-code your node IP before packaging:

   ```js
   function wsUrl() {
     return 'ws://192.168.1.100:8080/ws/nodes';  // your node IP
   }
   ```

---

## LG webOS — `.ipk` Package

### Prerequisites

| Tool | Source |
|------|--------|
| webOS Studio (VS Code extension) | https://webostv.developer.lge.com/develop/tools/webos-studio-installation |
| LG Developer Mode app | LG Content Store → Developer Mode |
| webOS CLI (`ares`) | Bundled with webOS Studio |

### Steps

1. **Create the app directory**

   ```bash
   mkdir webos-app && cd webos-app
   cp ../internal/httpapi/ui/tv-agent.html index.html
   ```

2. **Create `appinfo.json`**

   ```json
   {
     "id":          "io.soholink.tv-agent",
     "version":     "1.0.0",
     "vendor":      "NTARI",
     "type":        "web",
     "main":        "index.html",
     "title":       "SoHoLINK TV Agent",
     "icon":        "icon.png",
     "largeIcon":   "icon_large.png",
     "iconColor":   "#0D1117",
     "resolution":  "1920x1080",
     "transparentBackground": false,
     "requiredPermissions": ["network.connection"]
   }
   ```

3. **Build the `.ipk`**

   ```bash
   ares-package webos-app/
   # Outputs: io.soholink.tv-agent_1.0.0_all.ipk
   ```

4. **Install on TV**

   ```bash
   # Discover TV (TV must be on same LAN and Developer Mode active)
   ares-setup-device

   # Install
   ares-install io.soholink.tv-agent_1.0.0_all.ipk -d <device-name>

   # Launch
   ares-launch io.soholink.tv-agent -d <device-name>
   ```

5. **Node URL**

   Same as Tizen: edit `wsUrl()` in `tv-agent.html` before packaging to hard-code the
   node IP, or serve the page from the node and point the TV's browser to
   `http://<node-ip>:8080/tv` without packaging at all.

---

## Serving Without Packaging (Recommended for Development)

For any TV with a built-in browser (Tizen, webOS, Android TV Chrome, Fire TV Silk):

1. Start your SoHoLINK node.
2. Open the TV browser.
3. Navigate to `http://<node-ip>:8080/tv`.

The agent connects automatically to the same origin. No packaging required.

---

## Node Registration

The TV agent registers with `node_class: "android-tv"`. The Go scheduler already
handles this class with `DefaultConstraints()` from `internal/orchestration/mobile.go`.
Because TV agents cannot execute compute workloads, every task is immediately declined
and the scheduler falls back to a desktop node automatically.

---

## Roku

Roku is **not supported**. BrightScript channels are suspended within seconds of
backgrounding — there is no path to maintaining a persistent WebSocket connection.
The Tizen/webOS approach above covers the "TV as monitoring panel" use case for
Samsung and LG devices.

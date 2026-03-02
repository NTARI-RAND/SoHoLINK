# NTARI OS — Globe Network Graph Interface
## Design Document v0.1

**Status:** Prototype
**License:** AGPL-3.0
**Companion file:** `ntarios-globe.html`

---

## 1. Design Premise

Every mainstream operating system presents the user with a local-first metaphor: a desktop, a file system, a launcher. These interfaces imply that the machine itself is the primary object of concern, and that the network is a secondary utility accessed through discrete applications.

NTARI OS inverts this. The network *is* the primary object. The machine is one participant in a larger cooperative graph. The interface must express this inversion immediately and viscerally — before a user opens a terminal, before they see a file, the first thing they encounter is the network they belong to.

The globe interface is that first thing.

---

## 2. Core Design Decisions

### 2.1 The Globe as Instrument, Not Map

The globe is not a geographic map. It carries no political borders, no coastlines, no terrestrial features. It is an abstract sphere — a scientific instrument, like an orrery — whose only purpose is to express topological relationships between network nodes.

This choice is deliberate and non-negotiable:

- **No geography prevents misreading.** Node position on the globe does not represent physical location. It represents network distance from the local node. A geographic globe would imply that spatial proximity on Earth translates to network proximity. It does not.
- **An abstract sphere is politically neutral.** Cooperative infrastructure must not privilege any region visually.
- **The instrument aesthetic communicates precision and seriousness.** A wireframe lattice sphere communicates "this is a tool built by people who care about accuracy" in a way that a rendered terrain globe does not.

The wireframe lattice — fine latitude/longitude lines, no fill, a subtle limb-glow at the horizon — is the globe's visual vocabulary. It is readable at all network sizes and does not become visually noisy as nodes accumulate.

### 2.2 The Globe Breathes

The globe's radius grows logarithmically with the number of nodes in the network. This is the single most important behavioral design decision in the interface.

**Why logarithmic growth:**
- A linear scale would make a 2-node globe microscopic relative to a 200-node globe. The range is too extreme.
- A logarithmic scale produces a globe that feels noticeably larger as the network grows, but plateaus into a comfortable maximum — reflecting that large networks are not infinitely more complex to perceive, they are just denser.
- The growth should feel organic and weighted, as though the globe has gravitational mass that increases with participation.

**Scale reference points (approximate):**

| Nodes | Globe behavior | Human interpretation |
|---|---|---|
| 0–2 | Small, intimate. Fits comfortably in the center of the screen with generous space around it. | A new cooperative. We are just beginning. |
| 3–8 | Noticeably larger. The network has a presence. | A growing local mesh. |
| 9–15 | Globe fills a substantial portion of the viewport. Auto-zoom engages. | A district-scale cooperative network. |
| 16–30 | Globe at near-maximum radius. Auto-zoom compresses the view significantly. | A regional cooperative. |
| 30+ | Globe at maximum radius. Zoom continues to compress. | Infrastructure at scale. |

**The farthest node is always on the other side of the globe.**
When a new node joins, it is placed such that its angular distance from the local node reflects its network latency. The highest-latency node in the graph is always positioned diametrically opposite the local node. This makes the globe a spatial metaphor for network distance — users learn to read the globe as a topology without needing to understand IP addresses or routing tables.

### 2.3 Auto-Zoom at Density Threshold

When more than approximately 10 nodes are visible in the display area, automatic zoom-out engages. The zoom factor compresses smoothly and continuously — it does not snap or jump.

**Design intent:** The user should never need to manually zoom to maintain a coherent view of the network as it grows. The interface manages its own legibility. This reflects a broader NTARI OS principle: the system should adapt to the network's state, not require the user to adapt to the system.

**Manual zoom is always available** via scroll wheel (desktop) or pinch (touch). Manual zoom temporarily overrides auto-zoom and returns to automatic management after 3 seconds of inactivity.

### 2.4 Nodes as Participants, Not Icons

Nodes are rendered as luminous points — small, precise, lit from within. They are not icons, not avatars, not labeled boxes on a dashboard. They are presences.

**Visual treatment:**
- **Core:** A crisp white dot (2–4px radius depending on state) with a subtle outer glow that pulses slowly at a per-node frequency. Each node has a slightly different pulse phase and speed, making the overall constellation feel alive rather than mechanical.
- **Glow radius:** Expands and contracts with a sine wave, giving each node a breathing quality. The pulse is slow enough to be calming, fast enough to communicate activity.
- **Hidden-side nodes:** Nodes on the back hemisphere of the globe are rendered at dramatically reduced opacity (approximately 12% of front-face opacity). They are visible enough to indicate that the network extends beyond what is currently facing the viewer — they are not hidden, they are simply behind. This communicates network depth.
- **Selected state:** The selected node renders in the accent color (warm orange: `#ff6b35`) with an outer ring. Orange was chosen to contrast maximally with the cool blue of the default node and globe palette. Selection is unmistakable.
- **Hover state:** Cursor changes to pointer. Node radius increases slightly. This is the only hover affordance — the interface does not show tooltips on hover, because tooltips are a desktop-application convention that feels wrong in an instrument metaphor. Information about a node appears in the detail panel only after intentional selection.

### 2.5 Labels

Node names display directly adjacent to their dot, right-aligned to avoid occlusion with the globe wireframe. Labels are:

- Rendered in JetBrains Mono at 10px — monospaced to communicate machine-origin, light weight to avoid visual competition with the globe itself
- Fade in after a node appears (the first 40 frames of a node's life are used to animate its appearance, labels emerge during the second half of this window)
- Hidden for nodes on the back hemisphere
- At reduced opacity when the node is not selected or hovered — labels are present, but the globe and node positions carry the primary meaning

**Node naming convention (recommended for NTARI OS deployments):**
`<domain>.<function>` — e.g., `core.scheduler`, `mesh.relay_0`, `sensor.ambient`. The dot-delimited format groups related nodes visually in the label space and provides an implicit taxonomy that administrators can read at a glance.

### 2.6 Edges

Edges represent active topic subscriptions between nodes. They are rendered as faint quadratic bezier curves rather than straight lines — the curve gives the impression of transmission through space rather than rigid connection. Edge opacity increases with age (edges fade in over approximately one second after appearing) and remains low (maximum 25% opacity) to avoid visual dominance over the nodes themselves.

Edges on the back hemisphere are rendered at further-reduced opacity. Edges that cross from front to back fade mid-arc.

### 2.7 The Search Bar

The search bar is positioned to the right of the globe, vertically centered. This placement was chosen after considering several alternatives:

- **Top-center:** Too prominent. A search bar at top-center implies the interface is primarily a search interface, which it is not. The globe is primary.
- **Bottom-left or bottom-right:** Too subordinate. The search bar is a meaningful tool for large networks; it should be accessible without effort.
- **Right-center:** The globe occupies the center and left of the visual field. The right side is the natural landing zone for eyes moving clockwise from the globe. The search bar is present without competing.

The search bar expands on focus (180px → 220px) and reveals a dropdown of matching nodes. Selecting a result both opens the node detail panel and orients the globe toward the selected node, bringing it to the front hemisphere.

Search queries match against both node names and topic names. A node appears in results if any of its subscribed topics match the query — this allows administrators to search for all nodes participating in, for example, `/alerts/emergency`.

---

## 3. HUD Chrome

Four persistent UI elements frame the globe without cluttering it:

**Top-left — Wordmark:**
`NTARI OS` in the display serif (DM Serif Display). Below it, the current graph context: `Network Graph · v0.1.0`. The serif choice for the wordmark contrasts with the monospaced labels throughout the rest of the interface — the project name is the only element that is not machine-like in its typography.

**Top-right — Network stats:**
Three counters: total nodes, total unique topics, active nodes. These are tabular-numeric, light weight, updated live. The "Active" counter is highlighted in the accent blue (`#00d4ff`) to draw attention to the living network count.

**Bottom-left — Status ticker:**
A pulsing dot (the same pulse animation as nodes, but steady) followed by status text: `Graph online · DDS domain 0`. This communicates DDS domain membership — cooperative administrators need to know which domain they are viewing. The pulse communicates that the graph connection is live.

**Bottom-right — Scale indicator:**
A narrow progress bar (80px wide) that fills proportionally to the network's node count relative to the display maximum. Below it, a human-readable scale label: `2 nodes · local`, `12 nodes · district`, etc. This gives administrators an intuitive sense of network scale without requiring them to read the node count.

---

## 4. Color Palette

The palette is derived from a deep-space industrial aesthetic: near-black void, cool blues for infrastructure, warm orange for selection/emphasis.

| Role | Value | Usage |
|---|---|---|
| `--void` | `#050608` | Page background |
| `--deep` | `#080c12` | Deep surface |
| `--wire` | `#1a3a5c` | Globe wireframe |
| `--wire-bright` | `#2a6090` | Globe silhouette ring, scale bar |
| `--node-core` | `#e8f4ff` | Node dot fill (front hemisphere) |
| `--node-glow` | `#4ab3ff` | Node glow, edge color, search UI |
| `--node-pulse` | `#00d4ff` | Active stat counter, status dot |
| `--label` | `#a8cce8` | Node labels (default) |
| `--label-bright` | `#ddeeff` | Labels (selected/hovered), wordmark |
| `--accent` | `#ff6b35` | Selected node, selected label |
| `--text-muted` | `#3a5a7a` | HUD labels, inactive text |

The palette has a clear temperature logic: cool blues for infrastructure and information; warm orange exclusively for the selected/active state. This means orange always means "this is what you are looking at right now."

---

## 5. Typography

**JetBrains Mono** — all functional text: node labels, HUD counters, search, panel fields.
Rationale: monospaced for machine-origin legibility; JetBrains Mono specifically for its slightly wider letterforms (more readable at small sizes than alternatives) and its association with development tooling rather than consumer applications.

**DM Serif Display** — wordmark and node detail panel title only.
Rationale: the serif introduces one moment of warmth and humanity into an otherwise entirely technical type system. The wordmark should not feel like it belongs in a terminal; it should feel like it was named by people, for people. The panel title uses the same serif — a selected node deserves to be named, not labeled.

---

## 6. Interaction Model Summary

| Action | Result |
|---|---|
| Drag globe | Rotate freely on X and Y axes |
| Release drag | Auto-rotation resumes after 3 seconds |
| Scroll / pinch | Manual zoom (returns to auto-zoom after 3s) |
| Click node | Select node, open detail panel, orient globe |
| Click empty space | Deselect, close panel |
| Type in search | Filter nodes by name or topic |
| Select search result | Select node, orient globe toward it |
| + Add node (demo) | Add one node with auto-placement |
| + Add 5 / + Add 10 (demo) | Staggered node addition (180ms intervals) |
| Reset (demo) | Clear all nodes, return to minimal state |

---

## 7. States

### 7.1 Empty State (0 nodes)
Globe renders at half base radius. No labels, no edges. The interface is waiting. This state should communicate "ready" rather than "broken."

### 7.2 Minimal State (1–2 nodes)
Globe at base radius. The two initial nodes — typically `core.scheduler` and `mesh.relay_0` in a new NTARI OS deployment — are placed on roughly opposite hemispheres. The network has a size and a shape. One edge connects them.

### 7.3 Growing State (3–15 nodes)
Globe radius increasing. New nodes appear with a brief fade-in animation. Edges accumulate. The auto-zoom may begin engaging at the upper end of this range.

### 7.4 Dense State (15+ nodes)
Globe at near-maximum radius. Zoom compresses the view. Node labels begin to overlap — this is acceptable and expected. The globe is communicating that the network is too large to read individually; administrators should use search or interact with specific nodes rather than reading all labels simultaneously. Label overlap at scale is a feature, not a bug: it communicates density.

---

## 8. Implementation Notes

### Current prototype
The prototype (`ntarios-globe.html`) is a single-file, zero-dependency HTML/CSS/JS implementation using the HTML5 Canvas 2D API. It is intentionally dependency-free to maximize portability — it can run in any modern browser without a build step, a CDN, or a server.

### Production implementation path
For the production NTARI OS interface layer, the following changes are recommended:

**Replace simulated node data with a live ROS2 graph source.**
The interface should consume the ROS2 graph introspection API (`ros2 node list`, `ros2 topic list`, `/rosout`) and translate the DDS computation graph into the node/edge data model used by the visualization. A small WebSocket bridge process (running as a ROS2 node) should stream graph updates to the browser interface at approximately 1–2 Hz for topology changes.

**Node placement should use actual ROS2 latency data.**
In the prototype, node positions are approximated. In production, each node's angular position from the local node should be derived from measured round-trip latency on the DDS domain. Nodes with higher latency are placed farther around the globe. This makes the globe a genuine latency topology map, not an approximation.

**Consider Three.js or WebGL for large networks.**
The Canvas 2D implementation performs well up to approximately 50–80 nodes. Beyond that, WebGL rendering (Three.js or raw WebGL) will be necessary to maintain smooth animation. The architectural separation between the data model and the rendering layer in the prototype is designed to support this transition without rewriting the interaction logic.

**The interface layer runs as a ROS2 node.**
In the NTARI OS four-layer architecture, the interface layer is itself a participant in the DDS graph. The globe interface should be aware of its own node — rendering it distinctly (as the "origin" of the topology, always positioned at the viewer-facing pole of the globe) — and should publish interaction events (selected node, search queries) as ROS2 topics so that other system components can respond to administrator focus.

---

## 9. Open Design Questions

1. **Node health states.** The current prototype treats all nodes as active. Production needs a visual language for degraded, unreachable, and recovering nodes. Proposed: degraded nodes shift from white to a desaturated amber; unreachable nodes become hollow circles (ring only, no fill); recovering nodes pulse between hollow and filled.

2. **Topic visualization on demand.** When a node is selected and its panel shows subscribed topics, should clicking a topic highlight all nodes subscribing to that same topic on the globe? This would make the globe a live topic graph explorer, not just a node explorer. Recommend: yes, implement this in v0.2.

3. **Multi-domain support.** DDS supports multiple domains. If NTARI OS nodes span multiple DDS domains (e.g., domain 0 for local cooperative, domain 1 for federation with neighboring cooperatives), how does the globe represent this? Proposed: each domain gets a distinct orbital shell — the local domain globe, with a faint outer sphere for each federated domain. This is a significant design and implementation challenge deferred to v0.3.

4. **Label collision handling.** At high node density, labels overlap. The current approach accepts this as a density signal. An alternative is a force-directed label layout that pushes labels away from each other. This adds complexity and animation cost; evaluate against actual usability testing with dense networks.

---

*This document is part of the NTARI OS interface design series. It should be revised alongside the prototype as the design evolves through community feedback and usability testing.*

// SoHoLINK mesh topology graph — topology.js
// Canvas-based force-directed graph, no external dependencies.
// Renders nodes from /api/topology/mesh/peers with edge routing.

'use strict';

(function () {
  // Exposed so app.js can add nodes from WebSocket events
  window._topoNodes = [];
  window._topoAddNode = addOrUpdateNode;

  let canvas, ctx, nodes = [], edges = [], animFrame = null;
  let dragging = null, dragOX = 0, dragOY = 0;

  // ── Config ──
  const NODE_RADIUS = 18;
  const COLORS = {
    'baseline':       '#58a6ff',
    'high-security':  '#3fb950',
    'data-residency': '#d29922',
    'gpu-tier':       '#bc8cff',
    'unknown':        '#8b949e',
  };
  const EDGE_COLOR = 'rgba(88,166,255,0.2)';
  const BG_COLOR   = '#161b22';

  // ── Force simulation state ──
  const REPEL  = 3000;
  const SPRING = 0.04;
  const DAMP   = 0.85;
  const GRAV   = 0.002;

  function nodeColor(n) {
    return COLORS[n.compliance_level] || COLORS.unknown;
  }

  function addOrUpdateNode(data) {
    const existing = nodes.find(n => n.id === data.id);
    if (existing) {
      Object.assign(existing, data);
    } else {
      nodes.push({
        id: data.id || data.cluster_id || String(Math.random()),
        label: shortLabel(data.id || data.cluster_id || '?'),
        compliance_level: data.compliance_level || 'baseline',
        distance: data.distance || 1,
        x: canvas ? canvas.width / 2 + (Math.random() - .5) * 200 : 300,
        y: canvas ? canvas.height / 2 + (Math.random() - .5) * 200 : 200,
        vx: 0, vy: 0,
        ...data,
      });
    }
    rebuildEdges();
    if (!animFrame) animate();
  }

  function shortLabel(id) {
    if (!id) return '?';
    return id.length > 8 ? id.slice(-8) : id;
  }

  function rebuildEdges() {
    edges = [];
    // Connect each non-local node back to the local (index 0) node
    for (let i = 1; i < nodes.length; i++) {
      edges.push({ from: 0, to: i });
    }
  }

  // ── Physics step ──
  function step() {
    const W = canvas.width, H = canvas.height;
    const cx = W / 2, cy = H / 2;

    for (let i = 0; i < nodes.length; i++) {
      const a = nodes[i];

      // Gravity toward center
      a.vx += (cx - a.x) * GRAV;
      a.vy += (cy - a.y) * GRAV;

      // Node-node repulsion
      for (let j = i + 1; j < nodes.length; j++) {
        const b = nodes[j];
        const dx = a.x - b.x, dy = a.y - b.y;
        const dist2 = dx * dx + dy * dy + 1;
        const f = REPEL / dist2;
        const fx = f * dx / Math.sqrt(dist2);
        const fy = f * dy / Math.sqrt(dist2);
        a.vx += fx; a.vy += fy;
        b.vx -= fx; b.vy -= fy;
      }
    }

    // Spring forces along edges
    for (const e of edges) {
      const a = nodes[e.from], b = nodes[e.to];
      if (!a || !b) continue;
      const dx = b.x - a.x, dy = b.y - a.y;
      const dist = Math.sqrt(dx * dx + dy * dy) || 1;
      const targetDist = 120 + (b.distance || 1) * 40;
      const f = (dist - targetDist) * SPRING;
      const fx = f * dx / dist, fy = f * dy / dist;
      a.vx += fx; a.vy += fy;
      b.vx -= fx; b.vy -= fy;
    }

    // Update positions
    for (const n of nodes) {
      if (n === dragging) continue;
      n.vx *= DAMP; n.vy *= DAMP;
      n.x += n.vx; n.y += n.vy;
      n.x = Math.max(NODE_RADIUS, Math.min(W - NODE_RADIUS, n.x));
      n.y = Math.max(NODE_RADIUS, Math.min(H - NODE_RADIUS, n.y));
    }
  }

  // ── Render ──
  function draw() {
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.fillStyle = BG_COLOR;
    ctx.fillRect(0, 0, canvas.width, canvas.height);

    // Edges
    ctx.strokeStyle = EDGE_COLOR;
    ctx.lineWidth = 1.5;
    for (const e of edges) {
      const a = nodes[e.from], b = nodes[e.to];
      if (!a || !b) continue;
      ctx.beginPath();
      ctx.moveTo(a.x, a.y);
      ctx.lineTo(b.x, b.y);
      ctx.stroke();
    }

    // Nodes
    for (const n of nodes) {
      const color = nodeColor(n);

      // Glow
      const grad = ctx.createRadialGradient(n.x, n.y, 0, n.x, n.y, NODE_RADIUS * 2);
      grad.addColorStop(0, color + '33');
      grad.addColorStop(1, 'transparent');
      ctx.fillStyle = grad;
      ctx.beginPath();
      ctx.arc(n.x, n.y, NODE_RADIUS * 2, 0, Math.PI * 2);
      ctx.fill();

      // Circle
      ctx.beginPath();
      ctx.arc(n.x, n.y, NODE_RADIUS, 0, Math.PI * 2);
      ctx.fillStyle = '#0d1117';
      ctx.fill();
      ctx.strokeStyle = color;
      ctx.lineWidth = 2;
      ctx.stroke();

      // Label
      ctx.fillStyle = '#c9d1d9';
      ctx.font = '10px monospace';
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.fillText(n.label, n.x, n.y);

      // Distance badge
      if (n.distance > 0) {
        ctx.fillStyle = color;
        ctx.font = 'bold 9px sans-serif';
        ctx.fillText(`D${n.distance}`, n.x, n.y + NODE_RADIUS + 10);
      }
    }
  }

  function animate() {
    step();
    draw();
    animFrame = requestAnimationFrame(animate);
  }

  // ── Mouse drag ──
  function onMouseDown(e) {
    const { mx, my } = mouse(e);
    for (const n of [...nodes].reverse()) {
      if (Math.hypot(mx - n.x, my - n.y) <= NODE_RADIUS) {
        dragging = n;
        dragOX = mx - n.x;
        dragOY = my - n.y;
        return;
      }
    }
  }
  function onMouseMove(e) {
    if (!dragging) return;
    const { mx, my } = mouse(e);
    dragging.x = mx - dragOX;
    dragging.y = my - dragOY;
    dragging.vx = 0; dragging.vy = 0;
  }
  function onMouseUp() { dragging = null; }
  function mouse(e) {
    const r = canvas.getBoundingClientRect();
    return { mx: (e.clientX - r.left) * (canvas.width / r.width),
             my: (e.clientY - r.top) * (canvas.height / r.height) };
  }

  // ── Init ──
  async function init() {
    canvas = document.getElementById('topology-canvas');
    if (!canvas) return;
    ctx = canvas.getContext('2d');

    function resize() {
      const rect = canvas.getBoundingClientRect();
      canvas.width  = rect.width  || canvas.offsetWidth;
      canvas.height = rect.height || 400;
    }
    resize();
    window.addEventListener('resize', resize);

    canvas.addEventListener('mousedown', onMouseDown);
    canvas.addEventListener('mousemove', onMouseMove);
    canvas.addEventListener('mouseup', onMouseUp);
    canvas.addEventListener('mouseleave', onMouseUp);

    // Load initial peer data
    try {
      const data = await fetch('/api/topology/mesh/peers').then(r => r.json());
      const peers = data.peers || data || [];

      // Add local node as root (index 0)
      const statusData = await fetch('/api/status').then(r => r.json()).catch(() => ({}));
      addOrUpdateNode({ id: statusData.node_did || 'local', label: 'local',
                        compliance_level: statusData.compliance_level || 'baseline', distance: 0,
                        x: canvas.width / 2, y: canvas.height / 2 });

      for (const p of peers) {
        addOrUpdateNode({
          id: p.coordinator_did || p.cluster_id || p.id,
          label: shortLabel(p.coordinator_did || p.cluster_id),
          compliance_level: p.compliance_level || 'baseline',
          distance: p.distance || 1,
        });
      }
    } catch {}

    if (!animFrame) animate();
  }

  // Run init when topology tab is activated
  window.loadTopology = init;

  // Also auto-init if already visible
  if (document.querySelector('#panel-topology.active')) init();
})();

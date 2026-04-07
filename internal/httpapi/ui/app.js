/**
 * SoHoLINK Dashboard - app.js
 *
 * AUTH FLOW (no race conditions):
 *   1. Script loads, reads token from localStorage. No side effects.
 *   2. DOMContentLoaded fires, checks token.
 *   3. No token -> showLogin(), everything blocked.
 *   4. Token present -> showApp(), loadOverview(), connectWS().
 *   5. Any 401 -> clearToken(), showLogin().
 *   6. Logout -> clearToken(), showLogin().
 *
 * No IIFE boot. No top-level data fetching. All gated on auth.
 */

'use strict';

// Token management
var _token = localStorage.getItem('soholink_token') || null;
var auth = {
  get:       function() { return _token; },
  isLoggedIn:function() { return !!_token; },
  save:      function(t) { _token = t; localStorage.setItem('soholink_token', t); },
  clear:     function()  { _token = null; localStorage.removeItem('soholink_token'); }
};

// API helper
async function api(path, opts) {
  var options = opts || {};
  var headers = Object.assign({ 'Content-Type': 'application/json' }, options.headers || {});
  if (auth.isLoggedIn()) headers['Authorization'] = 'Bearer ' + auth.get();
  var res = await fetch(path, Object.assign({}, options, { headers: headers }));
  if (res.status === 401) {
    auth.clear();
    showLogin('Session expired. Please log in again.');
    throw new Error('401');
  }
  if (!res.ok) {
    var txt = await res.text().catch(function() { return res.statusText; });
    throw new Error(res.status + ': ' + txt.trim().slice(0, 200));
  }
  var ct = res.headers.get('content-type') || '';
  if (ct.indexOf('application/json') !== -1) return res.json();
  return {};
}

// DOM helpers
function qs(sel, ctx)   { return (ctx || document).querySelector(sel); }
function setText(id, v) { var el = document.getElementById(id); if (el) el.textContent = v; }
function setHtml(id, v) { var el = document.getElementById(id); if (el) el.innerHTML = v; }

function esc(s) {
  return String(s == null ? '' : s)
    .replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}
function badge(text, cls) {
  return '<span class="badge badge-' + esc(cls || 'blue') + '">' + esc(text) + '</span>';
}
function levelBadge(level) {
  var map = { 'baseline':'blue','high-security':'green','data-residency':'purple','gpu-tier':'yellow','tpm-verified':'green' };
  return badge(level || 'baseline', map[level] || 'blue');
}
function slaBadge(tier) {
  var map = { 'premium':'green','standard':'yellow','best-effort':'blue' };
  return badge(tier || 'best-effort', map[tier] || 'blue');
}
function statusBadge(s) {
  var map = { 'running':'green','healthy':'green','ok':'green','failed':'red','error':'red',
              'pending':'yellow','degraded':'yellow','processing':'yellow','settled':'green' };
  return badge(s || 'unknown', map[(s||'').toLowerCase()] || 'blue');
}
function shortDID(did) {
  if (!did || did === '-') return '-';
  return did.length > 24 ? did.slice(0,12) + '...' + did.slice(-6) : did;
}
function fmtDate(s) {
  if (!s) return '-';
  try { return new Date(s).toLocaleString(undefined,{dateStyle:'short',timeStyle:'short'}); } catch(e) { return s; }
}
function fmtSats(n) {
  if (n == null) return '-';
  return Number(n).toLocaleString() + ' sats';
}
function emptyState(msg) {
  return '<div class="empty-state"><p>' + esc(msg) + '</p></div>';
}
function skeletonRows(cols, rows) {
  var row = '<tr>' + '<td><div class="skeleton"></div></td>'.repeat(cols) + '</tr>';
  return row.repeat(rows || 3);
}
function skeletonCards(n) {
  var html = '';
  for (var i = 0; i < n; i++) {
    html += '<div class="kpi-card"><div class="skeleton" style="width:60%;margin-bottom:12px;height:0.75em"></div><div class="skeleton" style="width:40%;height:1.8em"></div></div>';
  }
  return html;
}
function statRow(label, value) {
  return '<div class="stat-row"><span class="stat-label">' + esc(label) + '</span><span class="stat-value">' + value + '</span></div>';
}
function kpiCard(label, value, sub) {
  return '<div class="kpi-card"><div class="kpi-label">' + esc(label) + '</div><div class="kpi-value">' + esc(String(value)) + '</div><div class="kpi-sub">' + esc(sub) + '</div></div>';
}
function errorCard(msg) {
  return '<div class="alert alert-error">Failed to load: ' + esc(msg) + '</div>';
}

// Login / App visibility
function showLogin(msg) {
  document.getElementById('login-screen').classList.remove('hidden');
  document.getElementById('app').classList.add('hidden');
  if (_ws) { try { _ws.close(); } catch(e) {} _ws = null; }
  setText('login-error', msg || '');
  var inp = document.getElementById('login-privkey');
  if (inp) { inp.value = ''; setTimeout(function(){ inp.focus(); }, 100); }
}

function showApp() {
  document.getElementById('login-screen').classList.add('hidden');
  document.getElementById('app').classList.remove('hidden');
  setText('login-error', '');
}

// Login handler
async function doLogin() {
  var input = document.getElementById('login-privkey');
  var errEl = document.getElementById('login-error');
  var btn   = document.getElementById('login-btn');
  var seed  = input.value.trim();
  errEl.textContent = '';

  if (!/^[0-9a-fA-F]{64}$/.test(seed)) {
    errEl.textContent = 'Must be exactly 64 hex characters.';
    input.focus();
    return;
  }

  btn.disabled    = true;
  btn.textContent = 'Connecting...';

  try {
    var res = await fetch('/api/auth/owner-login', {
      method:  'POST',
      headers: { 'Content-Type': 'application/json' },
      body:    JSON.stringify({ seed: seed, device_name: 'browser:' + navigator.userAgent.slice(0, 60) })
    });
    if (!res.ok) {
      var msg = await res.text().catch(function(){ return res.statusText; });
      errEl.textContent = 'Login failed: ' + msg.trim();
      return;
    }
    var json = await res.json();
    if (!json.device_token) { errEl.textContent = 'No token received from server.'; return; }
    auth.save(json.device_token);
    showApp();
    bootstrap();
  } catch(e) {
    errEl.textContent = 'Error: ' + e.message;
  } finally {
    btn.disabled    = false;
    btn.textContent = 'Connect to Node';
  }
}

function doLogout() {
  auth.clear();
  showLogin();
}

// Navigation
var _currentPanel = 'overview';

function navigateTo(name) {
  if (!auth.isLoggedIn()) return;
  document.querySelectorAll('.panel').forEach(function(p) { p.classList.remove('active'); });
  document.querySelectorAll('.nav-item').forEach(function(n) {
    n.classList.remove('active');
    n.removeAttribute('aria-current');
  });
  var panel = document.getElementById('panel-' + name);
  if (panel) panel.classList.add('active');
  var navItem = document.querySelector('.nav-item[data-panel="' + name + '"]');
  if (navItem) { navItem.classList.add('active'); navItem.setAttribute('aria-current','page'); }
  _currentPanel = name;
  loadPanel(name);
  document.getElementById('sidebar').classList.remove('open');
}

function loadPanel(name) {
  switch(name) {
    case 'overview':    loadOverview();    break;
    case 'management':  loadManagement();  break;
    case 'workloads':   loadWorkloads();   break;
    case 'peers':       loadPeers();       break;
    case 'topology':    loadTopology();    break;
    case 'marketplace': loadMarketplace(); break;
    case 'compliance':  loadCompliance();  break;
  }
}

function refreshCurrent() { if (auth.isLoggedIn()) loadPanel(_currentPanel); }

// Connection status
function setConn(on, label) {
  var dot = document.getElementById('conn-dot');
  var lbl = document.getElementById('conn-label');
  if (dot) dot.className = on ? '' : 'disconnected';
  if (lbl) lbl.textContent = label || (on ? 'Connected' : 'Disconnected');
  setText('status-text', label || (on ? 'Connected' : 'Disconnected'));
}

// WebSocket
var _ws = null;
var _wsDelay = 1000;

function connectWS() {
  if (!auth.isLoggedIn()) return;
  var dot = document.getElementById('conn-dot');
  if (dot) dot.className = 'connecting';
  setText('conn-label', 'Connecting...');

  var proto = location.protocol === 'https:' ? 'wss' : 'ws';
  _ws = new WebSocket(proto + '://' + location.host + '/ws/nodes?token=' + auth.get());

  _ws.onopen = function() {
    _wsDelay = 1000;
    setConn(true, 'Live');
  };
  _ws.onclose = function() {
    _ws = null;
    if (!auth.isLoggedIn()) return;
    setConn(false, 'Reconnecting...');
    setTimeout(connectWS, _wsDelay);
    _wsDelay = Math.min(_wsDelay * 2, 30000);
  };
  _ws.onerror = function() { setConn(false, 'Connection error'); };
  _ws.onmessage = function(e) {
    try { var m = JSON.parse(e.data); if (window._topoAddNode) window._topoAddNode(m); } catch(ex) {}
  };
}

// Bootstrap - called once after auth confirmed
function bootstrap() {
  connectWS();
  loadOverview();
  setInterval(function() { if (auth.isLoggedIn()) loadPanel(_currentPanel); }, 30000);
}

// OVERVIEW
async function loadOverview() {
  setHtml('overview-kpis', skeletonCards(4));
  setHtml('overview-cards', skeletonCards(2));
  try {
    var results = await Promise.allSettled([api('/api/status'), api('/api/health')]);
    var status  = results[0].status === 'fulfilled' ? results[0].value : {};
    var health  = results[1].status === 'fulfilled' ? results[1].value : {};

    var nodeDID = status.node_did || status.did || '';
    if (nodeDID) {
      setText('header-did', shortDID(nodeDID));
      document.getElementById('node-did-pill').title = nodeDID;
    }
    setConn(true, 'Connected');

    setHtml('overview-kpis',
      kpiCard('Uptime',          (status.uptime_percent||0).toFixed(1) + '%', 'Last 30 days') +
      kpiCard('Active Rentals',  status.active_rentals || 0,                  'Running now') +
      kpiCard('Federation Peers',status.federation_nodes || 0,                'Connected nodes') +
      kpiCard('Reputation',      (status.reputation_score||0) + '/100',       'Trust score')
    );

    setHtml('overview-cards',
      '<div class="card"><div class="card-title">Node Identity</div>' +
        statRow('DID',        '<span class="mono-cell">' + esc(shortDID(nodeDID)) + '</span>') +
        statRow('Version',    esc(status.version || '-')) +
        statRow('OS',         esc(status.os || '-')) +
        statRow('Health',     statusBadge(health.status || 'ok')) +
        statRow('Compliance', levelBadge(status.compliance_level)) +
        statRow('SLA',        slaBadge(status.sla_tier)) +
      '</div>' +
      '<div class="card"><div class="card-title">Resources</div>' +
        statRow('CPU Available', (status.available_cpu||0).toFixed(1) + ' cores') +
        statRow('Memory',        ((status.available_memory_mb||0)/1024).toFixed(1) + ' GiB') +
        statRow('Disk',          (status.available_disk_gb||0) + ' GB') +
        statRow('GPU',           esc(status.gpu_model||'none')) +
        statRow('Failure Rate',  ((status.failure_rate||0)*100).toFixed(2) + '%') +
        statRow('Region',        esc(status.region||'-')) +
      '</div>'
    );
  } catch(e) {
    if (e.message === '401') return;
    setConn(false, 'Error');
    setHtml('overview-kpis', '<div style="grid-column:1/-1">' + errorCard(e.message) + '</div>');
    setHtml('overview-cards', '');
  }
}

// MANAGEMENT
async function loadManagement() {
  setHtml('earnings-kpis', skeletonCards(3));
  setHtml('rentals-tbody',  skeletonRows(5,3));
  setHtml('payouts-tbody',  skeletonRows(5,3));

  var settled = await Promise.allSettled([
    api('/api/revenue/balance'),
    api('/api/revenue/active-rentals'),
    api('/api/revenue/payouts')
  ]);
  var bal   = settled[0].status === 'fulfilled' ? settled[0].value : {};
  var rentD = settled[1].status === 'fulfilled' ? settled[1].value : {};
  var payD  = settled[2].status === 'fulfilled' ? settled[2].value : {};

  var total   = bal.total_revenue_sats  || bal.total   || 0;
  var settl   = bal.settled_sats        || bal.settled || 0;
  var pending = bal.pending_payout_sats || bal.pending || 0;

  setHtml('earnings-kpis',
    kpiCard('Total Revenue',  fmtSats(total),   'All time') +
    kpiCard('Settled',        fmtSats(settl),   'Paid out') +
    kpiCard('Pending Payout', fmtSats(pending), 'Awaiting')
  );

  var rentals = rentD.rentals || rentD.active_rentals || [];
  setHtml('rentals-tbody', !rentals.length ?
    '<tr><td colspan="5">' + emptyState('No active rentals') + '</td></tr>' :
    rentals.map(function(r) {
      return '<tr>' +
        '<td class="mono-cell">' + esc(shortDID(r.user_did)) + '</td>' +
        '<td class="mono-cell">' + esc(shortDID(r.resource_id)) + '</td>' +
        '<td>' + esc(r.resource_type||'-') + '</td>' +
        '<td>' + esc(fmtSats(r.payment_amount)) + '</td>' +
        '<td>' + esc(fmtDate(r.created_at)) + '</td></tr>';
    }).join('')
  );

  var payouts = payD.payouts || [];
  setHtml('payouts-tbody', !payouts.length ?
    '<tr><td colspan="5">' + emptyState('No payouts yet') + '</td></tr>' :
    payouts.map(function(p) {
      return '<tr>' +
        '<td class="mono-cell">' + esc(shortDID(p.payout_id)) + '</td>' +
        '<td>' + esc(fmtSats(p.amount_sats)) + '</td>' +
        '<td>' + esc(p.processor||'-') + '</td>' +
        '<td>' + statusBadge(p.status) + '</td>' +
        '<td>' + esc(fmtDate(p.requested_at)) + '</td></tr>';
    }).join('')
  );

  drawIncomeChart();
}

async function drawIncomeChart() {
  var canvas = document.getElementById('income-chart');
  if (!canvas) return;
  var ctx = canvas.getContext('2d');
  var rows = [];
  try { var d = await api('/api/revenue/history?limit=30'); rows = d.transactions||d.history||d||[]; } catch(e) {}

  var buckets = {};
  var now = Date.now();
  for (var i = 29; i >= 0; i--) {
    buckets[new Date(now - i*86400000).toISOString().slice(0,10)] = 0;
  }
  rows.forEach(function(r) {
    var day = (r.created_at||r.timestamp||'').slice(0,10);
    if (day in buckets) buckets[day] += (r.producer_payout||r.total_amount||r.amount||0);
  });

  var days = Object.keys(buckets).sort();
  var vals = days.map(function(d) { return buckets[d]; });
  var maxV = Math.max.apply(null, vals.concat([1]));
  var dpr  = window.devicePixelRatio || 1;
  canvas.width  = canvas.offsetWidth  * dpr;
  canvas.height = canvas.offsetHeight * dpr;
  ctx.scale(dpr, dpr);
  var W = canvas.offsetWidth, H = canvas.offsetHeight;
  var pL=8,pR=8,pT=12,pB=22,cW=W-pL-pR,cH=H-pT-pB;
  var bW = Math.max(2, cW/days.length - 2);
  ctx.clearRect(0,0,W,H);
  ctx.strokeStyle='rgba(30,37,53,0.8)'; ctx.lineWidth=1;
  ctx.beginPath(); ctx.moveTo(pL,pT); ctx.lineTo(W-pR,pT); ctx.stroke();
  days.forEach(function(day,i) {
    var x=pL+i*(cW/days.length), bH=vals[i]===0?2:(vals[i]/maxV)*cH, y=pT+cH-bH;
    ctx.fillStyle = vals[i]>0 ? 'rgba(79,142,247,0.75)' : 'rgba(30,37,53,0.6)';
    ctx.fillRect(x+1, y, bW, bH);
  });
  ctx.fillStyle='rgba(90,100,120,0.9)'; ctx.font='10px system-ui'; ctx.textAlign='center';
  [[0,days[0]],[Math.floor(days.length/2),days[Math.floor(days.length/2)]],[days.length-1,days[days.length-1]]].forEach(function(pair) {
    if (!pair[1]) return;
    ctx.fillText(pair[1].slice(5), pL+pair[0]*(cW/days.length)+bW/2, H-5);
  });
}

function togglePayoutForm() {
  var f = document.getElementById('payout-form');
  if (f) f.classList.toggle('open');
}

async function submitPayout() {
  var alertEl = document.getElementById('payout-alert');
  var amount  = parseInt(document.getElementById('payout-amount').value, 10);
  var proc    = document.getElementById('payout-processor').value;
  alertEl.innerHTML = '';
  if (!amount || amount <= 0) { alertEl.innerHTML = '<div class="alert alert-error">Enter a valid amount in sats.</div>'; return; }
  var btn = document.getElementById('payout-submit-btn');
  btn.disabled = true; btn.textContent = 'Submitting...';
  try {
    var did = (document.getElementById('header-did')||{}).textContent || '';
    var data = await api('/api/revenue/request-payout', {
      method:'POST', body: JSON.stringify({ provider_did:did, amount_sats:amount, processor:proc })
    });
    alertEl.innerHTML = '<div class="alert alert-success">Submitted: ' + esc(data.payout_id||'OK') + '</div>';
    document.getElementById('payout-amount').value = '';
    loadManagement();
  } catch(e) {
    alertEl.innerHTML = '<div class="alert alert-error">' + esc(e.message) + '</div>';
  } finally {
    btn.disabled=false; btn.textContent='Submit';
  }
}

// WORKLOADS
async function loadWorkloads() {
  var el = document.getElementById('workloads-list');
  el.innerHTML = skeletonCards(3);
  try {
    var data = await api('/api/workloads');
    var wls  = data.workloads || data || [];
    if (!wls.length) { el.innerHTML = emptyState('No active workloads'); return; }
    el.innerHTML = wls.map(function(w) {
      var s = (w.status||'unknown').toLowerCase();
      return '<div class="wl-card ' + esc(s) + '">' +
        '<div class="wl-header"><span class="wl-name">' + esc(w.name||w.workload_id||'Workload') + '</span>' + statusBadge(s) + '</div>' +
        statRow('ID',       '<span class="mono-cell">' + esc(shortDID(w.workload_id)) + '</span>') +
        statRow('Owner',    '<span class="mono-cell">' + esc(shortDID(w.owner_did)) + '</span>') +
        statRow('CPU',      esc(String((w.spec&&w.spec.cpu_cores)||0)) + ' cores') +
        statRow('Memory',   esc(String((w.spec&&w.spec.memory_mb)||0)) + ' MB') +
        statRow('Replicas', esc(String(w.replicas||1))) +
      '</div>';
    }).join('');
  } catch(e) {
    if (e.message==='401') return;
    el.innerHTML = errorCard(e.message);
  }
}

// PEERS
async function loadPeers() {
  setHtml('peers-tbody', skeletonRows(6,4));
  try {
    var data  = await api('/api/topology/mesh/peers');
    var peers = data.peers || data || [];
    if (!peers.length) { setHtml('peers-tbody','<tr><td colspan="6">'+emptyState('No peers discovered yet')+'</td></tr>'); return; }
    setHtml('peers-tbody', peers.map(function(p) {
      return '<tr>' +
        '<td class="mono-cell">' + esc(shortDID(p.coordinator_did||p.did||p.id)) + '</td>' +
        '<td>' + esc(p.region||'-') + '</td>' +
        '<td>' + esc(String(((p.capacity&&p.capacity.available_cpu)||0).toFixed(1))) + '</td>' +
        '<td>' + esc(String(Math.round(((p.capacity&&p.capacity.available_memory_mb)||0)/1024))) + ' GiB</td>' +
        '<td>' + esc(p.distance!=null?String(p.distance):'-') + '</td>' +
        '<td>' + statusBadge(p.health||p.status||'unknown') + '</td></tr>';
    }).join(''));
  } catch(e) {
    if (e.message==='401') return;
    setHtml('peers-tbody','<tr><td colspan="6">'+errorCard(e.message)+'</td></tr>');
  }
}

// TOPOLOGY
function loadTopology() {
  if (window.initTopology) window.initTopology();
}

// MARKETPLACE
async function loadMarketplace() {
  setHtml('marketplace-tbody', skeletonRows(8,5));
  var comp = (document.getElementById('filter-compliance')||{}).value||'';
  var sla  = (document.getElementById('filter-sla')||{}).value||'';
  var url  = '/api/marketplace/nodes';
  var ps   = [];
  if (comp) ps.push('compliance_group='+encodeURIComponent(comp));
  if (sla)  ps.push('sla_tier='+encodeURIComponent(sla));
  if (ps.length) url += '?'+ps.join('&');
  try {
    var data  = await api(url);
    var nodes = data.nodes || [];
    if (!nodes.length) { setHtml('marketplace-tbody','<tr><td colspan="8">'+emptyState('No nodes available')+'</td></tr>'); return; }
    setHtml('marketplace-tbody', nodes.map(function(n) {
      var mult = n.price_multiplier>1 ? ' <span style="color:var(--yellow);font-size:0.7rem">x'+esc(n.price_multiplier.toFixed(2))+'</span>' : '';
      return '<tr>' +
        '<td class="mono-cell">' + esc(shortDID(n.node_did)) + '</td>' +
        '<td>' + esc(n.region||'-') + '</td>' +
        '<td>' + esc(String((n.available_cpu||0).toFixed(1))) + '</td>' +
        '<td>' + levelBadge(n.compliance_level) + '</td>' +
        '<td>' + slaBadge(n.sla_tier) + '</td>' +
        '<td>' + esc(String(n.price_per_cpu_hour_sats||0)) + ' sats' + mult + '</td>' +
        '<td>' + esc(String(n.reputation_score||0)) + '/100</td>' +
        '<td><button class="btn btn-secondary" style="padding:4px 10px;font-size:0.75rem" onclick="rentNode(\''+esc(n.node_did)+'\')">Rent</button></td></tr>';
    }).join(''));
  } catch(e) {
    if (e.message==='401') return;
    setHtml('marketplace-tbody','<tr><td colspan="8">'+errorCard(e.message)+'</td></tr>');
  }
}

function rentNode(did) {
  setText('status-text', 'Selected: ' + shortDID(did));
}

// COMPLIANCE
async function loadCompliance() {
  var el = document.getElementById('compliance-groups');
  el.innerHTML = skeletonCards(4);
  try {
    var data   = await api('/api/compliance/groups');
    var groups = data.groups || [];
    if (!groups.length) { el.innerHTML = emptyState('No compliance data'); return; }
    el.innerHTML = groups.map(function(g) {
      var members = (g.members||[]).slice(0,5).map(function(did) {
        return '<div style="font-family:var(--mono);font-size:0.7rem;color:var(--text-3);padding:2px 0">' + esc(shortDID(did)) + '</div>';
      }).join('');
      var more = g.member_count>5 ? '<div style="font-size:0.75rem;color:var(--text-3)">+' + (g.member_count-5) + ' more</div>' : '';
      return '<div class="card"><div class="card-title">' + levelBadge(g.group) + ' ' + esc(g.group) + '</div>' +
        statRow('Members', esc(String(g.member_count||0))) + members + more + '</div>';
    }).join('');
  } catch(e) {
    if (e.message==='401') return;
    el.innerHTML = errorCard(e.message);
  }
}

// DOM READY - single entry point, all wiring here
document.addEventListener('DOMContentLoaded', function() {

  // Login
  document.getElementById('login-btn').addEventListener('click', doLogin);
  document.getElementById('login-privkey').addEventListener('keydown', function(e) {
    if (e.key === 'Enter') doLogin();
  });

  // Logout + refresh
  document.getElementById('logout-btn').addEventListener('click', doLogout);
  document.getElementById('refresh-btn').addEventListener('click', refreshCurrent);

  // Sidebar nav via event delegation
  document.getElementById('sidebar').addEventListener('click', function(e) {
    var item = e.target.closest('.nav-item[data-panel]');
    if (item) navigateTo(item.dataset.panel);
  });

  // Hamburger (mobile)
  document.getElementById('hamburger').addEventListener('click', function() {
    var sidebar = document.getElementById('sidebar');
    var btn     = document.getElementById('hamburger');
    var open    = sidebar.classList.toggle('open');
    btn.setAttribute('aria-expanded', open ? 'true' : 'false');
  });

  // Close sidebar on main content click (mobile)
  document.getElementById('main').addEventListener('click', function() {
    document.getElementById('sidebar').classList.remove('open');
    document.getElementById('hamburger').setAttribute('aria-expanded','false');
  });

  // Payout form
  var toggleBtn  = document.getElementById('payout-toggle-btn');
  var submitBtn  = document.getElementById('payout-submit-btn');
  if (toggleBtn) toggleBtn.addEventListener('click', togglePayoutForm);
  if (submitBtn) submitBtn.addEventListener('click', submitPayout);

  // Marketplace filters
  var filterBtn = document.getElementById('market-filter-btn');
  if (filterBtn) filterBtn.addEventListener('click', loadMarketplace);

  // AUTH GATE - this is the ONLY place data loading begins
  if (auth.isLoggedIn()) {
    showApp();
    bootstrap();
  } else {
    showLogin();
  }

});

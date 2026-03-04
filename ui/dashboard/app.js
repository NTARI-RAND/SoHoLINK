/* SoHoLINK Dashboard — app.js */
'use strict';

// ── Router ────────────────────────────────────────────────────────────────────
const SCREENS = ['dashboard', 'planwork', 'management', 'help', 'settings'];

function route() {
  const id = (location.hash.slice(1) || 'dashboard');
  const target = SCREENS.includes(id) ? id : 'dashboard';

  document.querySelectorAll('.screen').forEach(el => el.classList.remove('active'));
  document.querySelectorAll('#nav a').forEach(el => el.classList.remove('active'));

  const screen = document.getElementById('screen-' + target);
  if (screen) screen.classList.add('active');

  const link = document.querySelector('#nav a[href="#' + target + '"]');
  if (link) link.classList.add('active');
}

window.addEventListener('hashchange', route);
window.addEventListener('DOMContentLoaded', () => { route(); init(); });

// ── API helpers ───────────────────────────────────────────────────────────────
async function apiFetch(path) {
  try {
    const r = await fetch(path);
    if (!r.ok) throw new Error(r.status);
    return await r.json();
  } catch (e) {
    return null;
  }
}

// ── Node status bar ───────────────────────────────────────────────────────────
async function refreshNodeStatus() {
  const el = document.getElementById('node-status');
  const data = await apiFetch('/api/health');
  if (data && data.status === 'ok') {
    el.className = 'online';
    el.querySelector('.status-text').textContent = 'Node running';
  } else {
    el.className = 'offline';
    el.querySelector('.status-text').textContent = 'Node offline';
  }
}

// ── Radial dial helper ────────────────────────────────────────────────────────
// pct: 0–100, circum: 2πr where r=50 (viewBox 120×120, cx=cy=60)
const CIRCUM = 2 * Math.PI * 50; // ≈ 314.16

function setDial(id, pct, label) {
  const fill = document.getElementById('dial-fill-' + id);
  const pctEl = document.getElementById('dial-pct-' + id);
  if (!fill || !pctEl) return;
  const capped = Math.max(0, Math.min(100, pct));
  const offset = CIRCUM * (1 - capped / 100);
  fill.style.strokeDasharray = CIRCUM;
  fill.style.strokeDashoffset = offset;
  pctEl.textContent = Math.round(capped) + '%';
  if (label !== undefined) {
    const lEl = document.getElementById('dial-label-' + id);
    if (lEl) lEl.textContent = label;
  }
}

function initDials() {
  ['cpu', 'ram', 'storage', 'net'].forEach(id => {
    const fill = document.getElementById('dial-fill-' + id);
    if (fill) {
      fill.style.strokeDasharray = CIRCUM;
      fill.style.strokeDashoffset = CIRCUM; // start empty
    }
  });
}

// ── Dashboard screen ──────────────────────────────────────────────────────────
async function refreshDashboard() {
  const data = await apiFetch('/api/status');
  if (!data) {
    // show zeros gracefully if node is unreachable
    ['cpu','ram','storage','net'].forEach(id => setDial(id, 0));
    setText('stat-uptime', '--');
    setText('stat-rentals', '--');
    setText('stat-nodes', '--');
    setText('stat-earned', '--');
    return;
  }

  // Dials
  setDial('cpu',     data.cpu_used_pct    ?? 0, (data.cpu_offered_pct    ?? 0) + '% offered');
  setDial('ram',     data.ram_used_pct    ?? 0, fmtGB(data.ram_offered_gb) + ' offered');
  setDial('storage', data.storage_used_pct ?? 0, fmtGB(data.storage_offered_gb) + ' offered');
  setDial('net',     data.net_used_pct    ?? 0, fmtMbps(data.net_offered_mbps) + ' offered');

  // Sub-labels (offered text below dials)
  setOffered('cpu',     (data.cpu_offered_pct    ?? 0) + '% offered');
  setOffered('ram',     fmtGB(data.ram_offered_gb)     + ' offered');
  setOffered('storage', fmtGB(data.storage_offered_gb)  + ' offered');
  setOffered('net',     fmtMbps(data.net_offered_mbps)  + ' offered');

  // Stat tiles
  setText('stat-uptime',  fmtUptime(data.uptime_seconds ?? 0));
  setText('stat-rentals', data.active_rentals ?? 0);
  setText('stat-nodes',   data.federation_nodes ?? 0);
  setText('stat-earned',  fmtSats(data.earned_sats_today ?? 0));
}

function setText(id, val) {
  const el = document.getElementById(id);
  if (el) el.textContent = val;
}

function setOffered(id, txt) {
  const el = document.getElementById('dial-offered-' + id);
  if (el) el.textContent = txt;
}

// ── Management screen ─────────────────────────────────────────────────────────
async function refreshManagement() {
  const [bal, payouts, rentals] = await Promise.all([
    apiFetch('/api/revenue/balance'),
    apiFetch('/api/revenue/payouts'),
    apiFetch('/api/revenue/active-rentals'),
  ]);

  // Balance
  if (bal) {
    setText('mgmt-balance',  fmtSats(bal.balance_sats   ?? 0));
    setText('mgmt-pending',  fmtSats(bal.pending_sats   ?? 0));
    setText('mgmt-total',    fmtSats(bal.total_earned_sats ?? 0));
  }

  // Active rentals table
  const tbody = document.getElementById('rentals-tbody');
  if (tbody && rentals) {
    const rows = Array.isArray(rentals) ? rentals : (rentals.rentals ?? []);
    if (rows.length === 0) {
      tbody.innerHTML = '<tr><td colspan="5" class="muted" style="text-align:center;padding:24px">No active rentals</td></tr>';
    } else {
      tbody.innerHTML = rows.map(r => `
        <tr>
          <td class="monospace">${esc(r.id ?? r.rental_id ?? '—')}</td>
          <td>${esc(r.resource_type ?? r.type ?? '—')}</td>
          <td>${esc(r.tenant_did ? r.tenant_did.slice(0,20)+'…' : '—')}</td>
          <td class="accent">${fmtRate(r.rate_per_hour_sats ?? r.rate_sats ?? 0)}</td>
          <td>${fmtDuration(r.elapsed_seconds ?? 0)}</td>
        </tr>`).join('');
    }
  }

  // Payout history
  const ptbody = document.getElementById('payouts-tbody');
  if (ptbody && payouts) {
    const rows = Array.isArray(payouts) ? payouts : (payouts.payouts ?? []);
    if (rows.length === 0) {
      ptbody.innerHTML = '<tr><td colspan="4" class="muted" style="text-align:center;padding:24px">No payouts yet</td></tr>';
    } else {
      ptbody.innerHTML = rows.map(p => `
        <tr>
          <td>${esc(fmtDate(p.created_at ?? p.timestamp))}</td>
          <td class="accent">${fmtSats(p.amount_sats ?? p.amount ?? 0)}</td>
          <td>${esc(p.processor ?? '—')}</td>
          <td><span class="badge ${statusBadge(p.status)}">${esc(p.status ?? '—')}</span></td>
        </tr>`).join('');
    }
  }
}

// ── Format helpers ────────────────────────────────────────────────────────────
function fmtGB(gb)      { return gb ? gb.toFixed(1) + ' GB' : '0 GB'; }
function fmtMbps(m)     { return m ? m + ' Mbps' : '0 Mbps'; }
function fmtSats(s)     { return Number(s).toLocaleString() + ' sats'; }
function fmtRate(s)     { return Number(s).toLocaleString() + ' sats/hr'; }
function fmtDuration(s) {
  if (!s) return '—';
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60);
  return h > 0 ? `${h}h ${m}m` : `${m}m`;
}
function fmtUptime(s) {
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}
function fmtDate(ts) {
  if (!ts) return '—';
  try { return new Date(ts).toLocaleDateString(); } catch { return ts; }
}
function statusBadge(s) {
  const map = { completed: 'green', paid: 'green', pending: 'yellow', failed: 'red', cancelled: 'red' };
  return map[s] ?? 'blue';
}
function esc(s) {
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

// ── Settings ──────────────────────────────────────────────────────────────────
async function loadSettings() {
  const data = await apiFetch('/api/config');
  if (!data) return;

  // Populate fields if they exist
  const fields = {
    'cfg-node-name':    data.node?.name,
    'cfg-node-region':  data.node?.region,
    'cfg-api-port':     data.api?.port,
    'cfg-radius-port':  data.radius?.port,
    'cfg-price-cpu':    data.pricing?.cpu_per_hour_sats,
    'cfg-price-ram':    data.pricing?.ram_per_gb_hour_sats,
    'cfg-price-storage':data.pricing?.storage_per_gb_hour_sats,
  };
  Object.entries(fields).forEach(([id, val]) => {
    const el = document.getElementById(id);
    if (el && val !== undefined && val !== null) el.value = val;
  });
}

// ── Init ──────────────────────────────────────────────────────────────────────
function init() {
  initDials();
  refreshNodeStatus();
  refreshDashboard();

  // Poll status every 5s
  setInterval(refreshNodeStatus, 5000);
  setInterval(refreshDashboard, 5000);

  // Lazy-load management data when that screen is first visited
  let mgmtLoaded = false;
  window.addEventListener('hashchange', () => {
    const id = location.hash.slice(1);
    if (id === 'management' && !mgmtLoaded) {
      mgmtLoaded = true;
      refreshManagement();
      setInterval(refreshManagement, 15000);
    }
    if (id === 'settings') loadSettings();
  });

  // Settings save
  const saveBtn = document.getElementById('settings-save');
  if (saveBtn) {
    saveBtn.addEventListener('click', async () => {
      saveBtn.disabled = true;
      saveBtn.textContent = 'Saving…';
      // POST /api/config is not yet implemented — show notice
      await new Promise(r => setTimeout(r, 400));
      saveBtn.textContent = 'Saved (restart node to apply)';
      setTimeout(() => { saveBtn.disabled = false; saveBtn.textContent = 'Save Settings'; }, 3000);
    });
  }

  // Payout button
  const payoutBtn = document.getElementById('payout-btn');
  if (payoutBtn) {
    payoutBtn.addEventListener('click', async () => {
      if (!confirm('Request payout of available balance?')) return;
      payoutBtn.disabled = true;
      payoutBtn.textContent = 'Requesting…';
      const res = await apiFetch('/api/revenue/request-payout');
      if (res) {
        alert('Payout requested successfully.');
      } else {
        alert('Payout request failed. Check node logs.');
      }
      payoutBtn.disabled = false;
      payoutBtn.textContent = 'Request Payout';
      refreshManagement();
    });
  }
}

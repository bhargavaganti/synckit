// synckit desktop frontend — talks to the Go backend via window.go.main.App.
const App = window.go.main.App;
const rt = window.runtime;

const $ = (id) => document.getElementById(id);
let state = { ov: null, matrix: null };
let lastRefresh = 0;

// ---------- helpers ----------
function status(msg) { $('status').textContent = msg; }

function toast(msg, kind = '') {
  const t = document.createElement('div');
  t.className = 'toast ' + kind;
  t.textContent = msg;
  $('toasts').appendChild(t);
  setTimeout(() => t.remove(), 6000);
}

function modal({ title, body, confirmText = 'Confirm', danger = false, onConfirm }) {
  const root = $('modal-root');
  root.innerHTML = `
    <div class="backdrop">
      <div class="modal">
        <h3>${esc(title)}</h3>
        <p>${esc(body)}</p>
        <div class="actions">
          <button class="btn ghost" id="mCancel">Cancel</button>
          <button class="btn ${danger ? 'danger' : 'primary'}" id="mOk">${esc(confirmText)}</button>
        </div>
      </div>
    </div>`;
  const close = () => (root.innerHTML = '');
  $('mCancel').onclick = close;
  $('mOk').onclick = () => { close(); onConfirm && onConfirm(); };
}

function errorModal(title, err) {
  modal({ title, body: String(err && err.message ? err.message : err), confirmText: 'OK', onConfirm() {} });
  $('mCancel').style.display = 'none';
}

function esc(s) {
  return String(s ?? '').replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
}
function h(tag, cls, html) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (html !== undefined) e.innerHTML = html;
  return e;
}

// ---------- tabs ----------
document.querySelectorAll('.tab').forEach((tab) => {
  tab.onclick = () => {
    document.querySelectorAll('.tab').forEach((t) => t.classList.remove('active'));
    document.querySelectorAll('.panel').forEach((p) => p.classList.remove('active'));
    tab.classList.add('active');
    $('panel-' + tab.dataset.tab).classList.add('active');
  };
});

// ---------- refresh ----------
async function refresh() {
  $('refreshBtn').disabled = true;
  status('Refreshing…');
  try {
    const ov = await App.Overview();
    state.ov = ov;
    $('machine').textContent = '— ' + ov.machine.hostname + ' · ' + ov.machine.os + '/' + ov.machine.arch;
    renderMachine(ov);
    renderBundles(ov);
    renderTailnet(ov);
    renderSettings(ov);
    lastRefresh = Date.now();
    status('Ready.');
  } catch (e) {
    errorModal('Failed to load', e);
    status('Error.');
  } finally {
    $('refreshBtn').disabled = false;
  }
  // matrix probes the network — load separately
  App.Matrix().then((m) => { state.matrix = m; renderMatrix(m); }).catch(() => {});
}

// "Refreshed N ago" indicator, ticking every second.
function tickRefreshed() {
  if (!lastRefresh) return;
  const s = Math.floor((Date.now() - lastRefresh) / 1000);
  const t = s < 5 ? 'just now' : s < 60 ? s + 's ago' : Math.floor(s / 60) + 'm ago';
  $('daemon').textContent = 'daemon: running · refreshed ' + t;
}
setInterval(tickRefreshed, 1000);
setInterval(() => { if (!document.querySelector('.backdrop')) refresh(); }, 30000); // auto-refresh
$('refreshBtn').onclick = refresh;

// ---------- This machine ----------
function renderMachine(ov) {
  const p = $('panel-machine');
  p.innerHTML = '';
  p.appendChild(h('h2', 'sec', 'Detected apps'));

  ov.apps.forEach((a) => {
    const card = h('div', 'card');
    const head = h('div', 'card row');
    head.appendChild(h('span', 'title', esc(a.id)));
    if (!a.installed) head.appendChild(h('span', 'chip', 'not installed'));
    else {
      const c = h('span', 'chip ' + (a.secretsCrossMachine ? 'good' : 'warn'),
        a.secretsCrossMachine ? 'secrets portable' : 'secrets: this machine only');
      c.title = a.note || '';
      const r = h('span', 'right'); r.appendChild(c); head.appendChild(r);
    }
    card.appendChild(head);
    (a.instances || []).forEach((inst) => {
      const row = h('div', 'inst card row');
      const left = h('div', 'grow');
      left.appendChild(h('div', 'title', esc(inst.label) + (inst.version ? '  v' + esc(inst.version) : '')));
      left.appendChild(h('div', 'sub', esc(inst.root)));
      row.appendChild(left);
      row.appendChild(h('span', 'chip ' + (inst.running ? 'bad' : 'good'), inst.running ? '⚠ running' : 'idle'));
      card.appendChild(row);
    });
    p.appendChild(card);
  });

  // snapshot bar
  const bar = h('div', 'card row');
  bar.appendChild(h('span', 'title', 'Create snapshot'));
  const checks = h('div', 'checks grow');
  ov.apps.filter((a) => a.installed).forEach((a) => {
    const lb = h('label', 'ck');
    lb.innerHTML = `<input type="checkbox" checked data-app="${esc(a.id)}"> ${esc(a.id)}`;
    checks.appendChild(lb);
  });
  bar.appendChild(checks);
  const btn = h('button', 'btn primary', '● Snapshot now');
  btn.onclick = doSnapshot;
  bar.appendChild(btn);
  p.appendChild(bar);
}

function selectedApps() {
  return [...document.querySelectorAll('#panel-machine input[data-app]:checked')].map((c) => c.dataset.app);
}

async function doSnapshot() {
  status('Creating snapshot…');
  try {
    const r = await App.Snapshot(selectedApps());
    let msg = `Snapshot created: ${r.bundle} (${r.instances} instance(s))`;
    if (!r.encrypted) msg += '\n⚠ unencrypted — set up a key in Settings';
    if (r.skipped && r.skipped.length) msg += '\nSkipped: ' + r.skipped.join('; ');
    toast(msg, 'ok');
    refresh();
  } catch (e) { errorModal('Snapshot failed', e); }
  status('Ready.');
}

// ---------- Bundles ----------
let bundleFilter = 'all';

function renderBundles(ov) {
  const p = $('panel-bundles');
  p.innerHTML = '';
  const all = ov.localBundles || [];
  p.appendChild(h('h2', 'sec', 'Local bundles'));
  if (!all.length) {
    p.appendChild(emptyState('📦', 'No bundles yet', 'Take a snapshot, or fetch one from the Tailnet tab.'));
    return;
  }

  // distinct source machines for the filter
  const hosts = [...new Set(all.map((b) => b.originHost).filter(Boolean))].sort();
  if (bundleFilter !== 'all' && !hosts.includes(bundleFilter)) bundleFilter = 'all';
  const shown = bundleFilter === 'all' ? all : all.filter((b) => b.originHost === bundleFilter);

  // toolbar: filter dropdown + delete-all
  const bar = h('div', 'card row');
  const lbl = h('span', 'sub', 'From machine:');
  const sel = document.createElement('select');
  sel.className = 'text';
  sel.style.maxWidth = '220px';
  sel.innerHTML = '<option value="all">All machines (' + all.length + ')</option>' +
    hosts.map((hn) => '<option value="' + esc(hn) + '"' + (hn === bundleFilter ? ' selected' : '') + '>' +
      esc(hn) + ' (' + all.filter((b) => b.originHost === hn).length + ')</option>').join('');
  sel.value = bundleFilter;
  sel.onchange = () => { bundleFilter = sel.value; renderBundles(state.ov); };
  bar.appendChild(lbl);
  bar.appendChild(sel);
  const del = h('button', 'btn danger right', '🗑 Delete ' + (bundleFilter === 'all' ? 'all' : 'shown') + ' (' + shown.length + ')');
  del.onclick = () => confirmDeleteBundles(shown);
  bar.appendChild(del);
  p.appendChild(bar);

  shown.forEach((b) => p.appendChild(bundleCard(b, [
    ['Dry-run', 'btn', () => doRestore(b.name, [], true, false)],
    ['Restore', 'btn primary', () => confirmRestore(b.name)],
    ['🗑', 'btn ghost', () => confirmDeleteBundles([b])],
  ])));
}

function confirmDeleteBundles(list) {
  if (!list.length) return;
  const what = list.length === 1 ? list[0].id || list[0].name : list.length + ' bundles';
  modal({
    title: 'Delete ' + what + '?',
    body: 'This permanently removes the bundle file(s) from this machine. Your app profiles are not affected. This cannot be undone.',
    confirmText: 'Delete', danger: true,
    onConfirm: async () => {
      let n = 0;
      for (const b of list) { try { await App.DeleteBundle(b.name); n++; } catch (e) {} }
      toast('Deleted ' + n + ' bundle(s)', 'ok');
      refresh();
    },
  });
}

function bundleCard(b, actions) {
  const card = h('div', 'card row');
  const left = h('div', 'grow');
  left.appendChild(h('div', 'title', esc(b.id || b.name)));
  let sub = (b.apps || []).join(' + ') + ' · ' + (b.sizeMB || 0).toFixed(1) + ' MB';
  if (b.createdAt) sub += ' · ' + b.createdAt;
  if (b.originHost) sub += ' · from ' + b.originHost + ' (' + b.originOS + ')';
  left.appendChild(h('div', 'sub', esc(sub)));
  card.appendChild(left);
  const r = h('div', 'right');
  actions.forEach(([label, cls, fn]) => { const btn = h('button', cls, label); btn.onclick = fn; r.appendChild(btn); });
  card.appendChild(r);
  return card;
}

function confirmRestore(name) {
  modal({
    title: 'Restore ' + name + '?',
    body: 'Your current profiles are backed up first. Close the target apps, or use force-close.',
    confirmText: 'Restore',
    onConfirm: () => doRestore(name, [], false, false),
  });
}

async function doRestore(name, apps, dryRun, forceClose) {
  status(dryRun ? 'Dry-run…' : 'Restoring…');
  try {
    const outcomes = await App.Restore(name, apps, dryRun, forceClose);
    handleOutcomes(outcomes, dryRun, name);
  } catch (e) { errorModal('Restore failed', e); }
  status('Ready.');
}

function handleOutcomes(outcomes, dryRun, name) {
  if (!outcomes || !outcomes.length) {
    modal({ title: 'Nothing to restore', body: 'This bundle has no apps that are installed on this machine, so there is nothing to put back. Install the app here first, then restore.', confirmText: 'OK', onConfirm() {} });
    $('mCancel').style.display = 'none';
    return;
  }
  const running = outcomes.filter((o) => o.skipped && o.skipped.includes('running'));
  if (running.length && !dryRun) {
    modal({
      title: 'Some apps are running',
      body: running.map((o) => o.app).join(', ') + ' are open. Force-close them and restore?\n(unsaved work in those apps will be lost)',
      confirmText: 'Force-close & restore', danger: true,
      onConfirm: () => doRestore(name, [], false, true),
    });
    return;
  }
  const skipped = outcomes.filter((o) => o.skipped);
  const applied = outcomes.filter((o) => !o.skipped); // dry-run: "would restore"; real: restored
  const verb = dryRun ? 'Would restore' : 'Restored';
  let msg = `${verb} ${applied.length}` + (skipped.length ? `, skipped ${skipped.length}` : '');
  if (applied.length) msg += '\n✓ ' + applied.map((o) => o.app + '/' + o.instance).join(', ');
  if (skipped.length) msg += '\n⚠ ' + skipped.map((o) => o.app + ': ' + o.skipped).join('\n⚠ ');
  toast(msg, dryRun ? '' : 'ok');
  refresh();
}

// ---------- Tailnet ----------
// fetchableFrom returns the bundles worth fetching FROM a peer TO this machine:
// only those that originated elsewhere (not our own echoed back), newest per
// source machine.
function fetchableFrom(peer, me) {
  const fromOthers = (peer.bundles || []).filter((b) => b.originHost && b.originHost !== me);
  const newest = {};
  fromOthers.forEach((b) => {
    const k = b.originHost;
    if (!newest[k] || (b.createdAt || '') > (newest[k].createdAt || '')) newest[k] = b;
  });
  return { list: Object.values(newest), hidden: (peer.bundles || []).length - Object.values(newest).length };
}

function renderTailnet(ov) {
  const p = $('panel-tailnet');
  p.innerHTML = '';
  p.appendChild(h('h2', 'sec', 'Fetch profiles from other machines'));
  if (!ov.tailscaleUp) {
    p.appendChild(emptyState('🔌', 'Tailscale not detected', 'Set the CLI path in Settings, then Refresh.'));
    return;
  }
  if (!ov.peers || !ov.peers.length) { p.appendChild(emptyState('🌐', 'No peers', 'Is Tailscale up?')); return; }

  const me = ov.machine.hostname;
  ov.peers.forEach((peer) => {
    const card = h('div', 'card');
    const head = h('div', 'row');
    head.appendChild(h('span', 'dot ' + (peer.online ? 'on' : 'off')));
    head.appendChild(h('span', 'title', esc(peer.host)));
    head.appendChild(h('span', 'sub grow', esc(peer.ip + ' · ' + peer.os)));
    let stChip = peer.online ? (peer.serving ? '<span class="chip good">● serving</span>' : '<span class="chip warn">not serving</span>') : '<span class="chip">offline</span>';
    head.appendChild(h('span', 'right', stChip));
    card.appendChild(head);

    const { list, hidden } = fetchableFrom(peer, me);
    list.forEach((b) => {
      const row = bundleCard(b, [
        ['Fetch only', 'btn', () => doFetch(peer.ip, b.name, false)],
        ['Fetch & import', 'btn primary', () => confirmFetch(peer, b)],
      ]);
      row.style.marginTop = '10px';
      card.appendChild(row);
    });
    if (peer.online && peer.serving && !list.length)
      card.appendChild(h('div', 'sub', hidden ? '(only your own bundles here — nothing new to fetch)' : '(no bundles offered)'));
    else if (hidden > 0)
      card.appendChild(h('div', 'sub', `(${hidden} of your own/older bundle(s) hidden)`));
    p.appendChild(card);
  });
}

function confirmFetch(peer, b) {
  modal({
    title: 'Fetch & import from ' + peer.host + '?',
    body: 'Download ' + b.name + ' and restore it here. Current profiles are backed up first.',
    confirmText: 'Fetch & import',
    onConfirm: () => doFetch(peer.ip, b.name, true),
  });
}

async function doFetch(ip, name, apply) {
  status('Fetching ' + name + '…');
  try {
    const outcomes = await App.Fetch(ip, name, apply, false);
    if (apply && outcomes) handleOutcomes(outcomes, false, name);
    else toast('Fetched ' + name + ' → Bundles', 'ok');
    refresh();
  } catch (e) { errorModal('Fetch failed', e); }
  status('Ready.');
}

// ---------- Sync map ----------
function relTime(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d) || d.getFullYear() < 2000) return ''; // zero time = never snapshotted
  const s = Math.max(0, (Date.now() - d.getTime()) / 1000);
  if (s < 60) return 'just now';
  if (s < 3600) return Math.floor(s / 60) + 'm ago';
  if (s < 86400) return Math.floor(s / 3600) + 'h ago';
  return Math.floor(s / 86400) + 'd ago';
}

// rowStatus gives one clear, actionable status for an app across the tailnet.
function rowStatus(r, me, machines) {
  const iHave = r.cells[me] && r.cells[me].present;
  const peers = machines.filter((mc) => mc !== me && r.cells[mc] && r.cells[mc].present);
  const peerMissing = machines.filter((mc) => mc !== me && (!r.cells[mc] || !r.cells[mc].present));
  if (!iHave && peers.length) return { txt: '⬇ on ' + peers[0] + ' — install here to sync', cls: 'warn' };
  if (iHave && !peers.length) return { txt: 'only on this machine', cls: '' };
  if (r.sync === 'in-sync') return { txt: '✓ in sync', cls: 'good' };
  if (r.sync === 'differs')
    return r.newestHost === me
      ? { txt: '⬆ you\'re newer — send to ' + (peerMissing[0] || peers[0]), cls: 'accent' }
      : { txt: '⬇ update available from ' + r.newestHost, cls: 'accent' };
  return { txt: 'snapshot both to compare', cls: '' };
}

function renderMatrix(m) {
  const p = $('panel-matrix');
  p.innerHTML = '';
  p.appendChild(h('h2', 'sec', 'App inventory & sync status'));
  if (!m || !m.rows || !m.rows.length) {
    p.appendChild(emptyState('🗺️', 'Nothing to map yet', 'Start synckit on your other machines (Tailscale up on both).'));
    return;
  }
  const me = state.ov ? state.ov.machine.hostname : '';
  const wrap = h('div', 'card');
  let html = '<table class="matrix"><thead><tr><th>App / profile</th>';
  m.machines.forEach((mc) => (html += '<th>' + esc(mc) + (mc === me ? ' (you)' : '') + '</th>'));
  html += '<th>Secrets</th><th>Status</th></tr></thead><tbody>';
  m.rows.forEach((r) => {
    html += '<tr><td>' + esc(r.app + ' / ' + r.role) + '</td>';
    m.machines.forEach((mc) => {
      const c = r.cells[mc];
      if (!c || !c.present) { html += '<td class="mono muted">—</td>'; return; }
      const when = relTime(c.snapshotAt);
      const line = (c.version ? '✓ ' + esc(c.version) : '✓') +
        (when ? '<div class="sub">' + when + '</div>' : '<div class="sub">not snapshotted</div>');
      html += '<td class="mono">' + line + '</td>';
    });
    const verdict = { full: '✅ full', 'settings-only': '⚠ settings', seed: '—' }[r.verdict] || r.verdict;
    const st = rowStatus(r, me, m.machines);
    html += '<td>' + verdict + '</td><td><span class="chip ' + st.cls + '">' + esc(st.txt) + '</span></td></tr>';
  });
  html += '</tbody></table>';
  wrap.innerHTML = html;
  p.appendChild(wrap);

  const legend = h('div', 'sub');
  legend.style.marginTop = '10px';
  legend.innerHTML =
    'Each cell shows the app version + when it was last snapshotted on that machine. ' +
    '“install here to sync” = the app is only on the other machine — install it here first. ' +
    'Fetch/import from the <b>Tailnet</b> tab.';
  p.appendChild(legend);
}

// ---------- transfer progress ----------
function showTransfer(d) {
  let bar = $('xfer');
  if (!d.active) { if (bar) bar.remove(); return; }
  if (!bar) {
    bar = h('div', 'card');
    bar.id = 'xfer';
    bar.style.cssText = 'position:fixed;left:18px;right:18px;bottom:44px;z-index:55;margin:0';
    document.body.appendChild(bar);
  }
  const pct = d.total > 0 ? Math.round((d.done / d.total) * 100) : null;
  const mb = (n) => (n / (1 << 20)).toFixed(1);
  bar.innerHTML =
    '<div class="row" style="margin-bottom:8px"><span class="title">Transferring ' + esc(d.name) + '</span>' +
    '<span class="right sub">' + (pct !== null ? pct + '% · ' + mb(d.done) + ' / ' + mb(d.total) + ' MB' : mb(d.done) + ' MB') + '</span></div>' +
    '<div class="progress"><i style="width:' + (pct !== null ? pct : 30) + '%"></i></div>';
}

// ---------- Settings ----------
async function renderSettings(ov) {
  const p = $('panel-settings');
  const st = await App.GetSettings();
  const key = await App.KeyStatus();
  p.innerHTML = '';

  // Tailscale path
  const tsCard = h('div', 'card');
  tsCard.appendChild(h('div', 'title', 'Tailscale CLI path'));
  tsCard.appendChild(h('div', 'sub', 'Leave blank to auto-detect. Set this if peers aren\'t found (e.g. macOS: /Applications/Tailscale.app/Contents/MacOS/Tailscale).'));
  const inp = h('input', 'text'); inp.style.margin = '10px 0'; inp.value = st.tailscalePath || ''; inp.placeholder = 'auto-detect';
  tsCard.appendChild(inp);
  const tsRow = h('div', 'row');
  const saveBtn = h('button', 'btn primary', 'Save');
  const diagBtn = h('button', 'btn', 'Run diagnostics');
  tsRow.appendChild(saveBtn); tsRow.appendChild(diagBtn);
  tsCard.appendChild(tsRow);
  const diag = h('textarea', 'console'); diag.readOnly = true; diag.style.marginTop = '12px'; diag.value = 'Click “Run diagnostics”.';
  tsCard.appendChild(diag);
  saveBtn.onclick = async () => {
    try { await App.SetTailscalePath(inp.value.trim()); toast('Saved Tailscale path', 'ok'); runDiag(); refresh(); }
    catch (e) { errorModal('Save failed', e); }
  };
  diagBtn.onclick = runDiag;
  async function runDiag() { diag.value = 'Running…'; try { diag.value = await App.Diagnose(); } catch (e) { diag.value = String(e); } }
  p.appendChild(h('h2', 'sec', 'Settings')); p.appendChild(tsCard);

  // Encryption
  const encCard = h('div', 'card row');
  encCard.appendChild(h('div', 'grow',
    '<div class="title">Encryption</div><div class="sub">' +
    (key.enabled ? 'ON · ' + esc(key.recipient) : 'OFF — bundles are unencrypted') + '</div>'));
  if (!key.enabled) {
    const kb = h('button', 'btn primary', 'Enable (generate key)');
    kb.onclick = async () => { try { const r = await App.KeyInit(); toast('Key created. Copy it to your other machines.\n' + r, 'ok'); renderSettings(ov); } catch (e) { errorModal('Key init failed', e); } };
    encCard.appendChild(h('div', 'right')).appendChild(kb);
  } else encCard.appendChild(h('span', 'chip good', 'encrypted'));
  p.appendChild(encCard);

  // Ignore rules
  const igCard = h('div', 'card');
  igCard.appendChild(h('div', 'title', 'Ignore rules (keep bundles small)'));
  igCard.appendChild(h('div', 'sub', 'Exclude bulky paths. App "*" applies to all. e.g. chrome  IndexedDB/**'));
  const list = h('div', ''); list.style.margin = '10px 0';
  const rules = st.ignore || {};
  Object.keys(rules).forEach((appId) => rules[appId].forEach((g) => {
    const row = h('div', 'row'); row.style.padding = '4px 0';
    row.appendChild(h('span', 'chip accent', esc(appId)));
    row.appendChild(h('span', 'grow', ' ' + esc(g)));
    const x = h('button', 'btn ghost', '✕');
    x.onclick = async () => { await App.RemoveIgnore(appId, g); renderSettings(ov); };
    row.appendChild(x); list.appendChild(row);
  }));
  igCard.appendChild(list);
  const addRow = h('div', 'row');
  const appSel = h('input', 'text'); appSel.placeholder = 'app (or *)'; appSel.style.maxWidth = '140px';
  const patInp = h('input', 'text grow'); patInp.placeholder = 'pattern e.g. IndexedDB/**';
  const addBtn = h('button', 'btn', 'Add');
  addBtn.onclick = async () => { if (!appSel.value || !patInp.value) return; await App.AddIgnore(appSel.value.trim(), patInp.value.trim()); renderSettings(ov); };
  addRow.appendChild(appSel); addRow.appendChild(patInp); addRow.appendChild(addBtn);
  igCard.appendChild(addRow);
  p.appendChild(igCard);
}

function emptyState(icon, title, sub) {
  return h('div', 'empty', `<div class="big">${icon}</div><div style="font-size:16px;margin:8px 0 4px">${esc(title)}</div><div>${esc(sub)}</div>`);
}

// ---------- events + init ----------
if (rt && rt.EventsOn) {
  rt.EventsOn('bundle-received', () => { toast('Received a bundle from a peer', 'ok'); refresh(); });
  rt.EventsOn('transfer', (d) => showTransfer(d));
}
App.Version().then((v) => ($('version').textContent = v)).catch(() => {});
$('daemon').textContent = 'daemon: running';
refresh();

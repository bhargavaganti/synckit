package ui

// dashboardHTML is the single-page control panel served at "/". It is fully
// self-contained (inline CSS/JS, no external requests) and talks to /api/*.
// The embedded JS avoids backticks so it can live in a Go raw string literal.
const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>synckit</title>
<style>
  :root {
    --bg:#0f1115; --panel:#171a21; --panel2:#1e222b; --line:#2a2f3a;
    --fg:#e6e9ef; --muted:#8b93a7; --accent:#5b9dff; --good:#3fb950;
    --warn:#d29922; --bad:#f85149; --chip:#252b36;
  }
  @media (prefers-color-scheme: light) {
    :root { --bg:#f6f7f9; --panel:#fff; --panel2:#f0f2f5; --line:#e2e6ec;
      --fg:#1b1f27; --muted:#5a6473; --chip:#eceff3; }
  }
  * { box-sizing:border-box; }
  body { margin:0; font:14px/1.5 -apple-system,Segoe UI,Roboto,sans-serif;
    background:var(--bg); color:var(--fg); }
  header { padding:18px 24px; border-bottom:1px solid var(--line);
    display:flex; align-items:center; gap:14px; position:sticky; top:0;
    background:var(--bg); z-index:5; }
  header h1 { font-size:18px; margin:0; letter-spacing:.5px; }
  header .machine { color:var(--muted); font-size:13px; }
  header .spacer { flex:1; }
  main { max-width:1000px; margin:0 auto; padding:24px; }
  h2 { font-size:13px; text-transform:uppercase; letter-spacing:1px;
    color:var(--muted); margin:28px 0 12px; }
  .card { background:var(--panel); border:1px solid var(--line);
    border-radius:12px; padding:16px; margin-bottom:12px; }
  .row { display:flex; align-items:center; gap:12px; flex-wrap:wrap; }
  .row + .row { margin-top:10px; }
  .title { font-weight:600; }
  .sub { color:var(--muted); font-size:12px; word-break:break-all; }
  .chip { background:var(--chip); border:1px solid var(--line); color:var(--muted);
    border-radius:999px; padding:2px 10px; font-size:12px; white-space:nowrap; }
  .chip.good { color:var(--good); } .chip.warn { color:var(--warn); }
  .chip.bad { color:var(--bad); } .chip.accent { color:var(--accent); }
  .spacer { flex:1; }
  button { font:inherit; border:1px solid var(--line); background:var(--panel2);
    color:var(--fg); border-radius:8px; padding:7px 14px; cursor:pointer; }
  button:hover { border-color:var(--accent); }
  button.primary { background:var(--accent); border-color:var(--accent); color:#fff; }
  button:disabled { opacity:.5; cursor:not-allowed; }
  .inst { border-top:1px solid var(--line); padding-top:10px; margin-top:10px; }
  label.ck { display:inline-flex; align-items:center; gap:6px; color:var(--muted);
    font-size:13px; cursor:pointer; }
  .empty { color:var(--muted); padding:8px 0; }
  #toasts { position:fixed; right:16px; bottom:16px; display:flex;
    flex-direction:column; gap:8px; z-index:50; max-width:420px; }
  .toast { background:var(--panel2); border:1px solid var(--line);
    border-left:3px solid var(--accent); border-radius:8px; padding:10px 14px;
    box-shadow:0 6px 24px rgba(0,0,0,.3); font-size:13px; white-space:pre-wrap; }
  .toast.err { border-left-color:var(--bad); }
  .toast.ok { border-left-color:var(--good); }
  .peerhost { font-weight:600; }
  .dot { width:8px; height:8px; border-radius:50%; display:inline-block; }
  .dot.on { background:var(--good); } .dot.off { background:var(--muted); }
  .muted { color:var(--muted); }
  a { color:var(--accent); }
</style>
</head>
<body>
<header>
  <h1>🔄 synckit</h1>
  <span class="machine" id="machine">…</span>
  <span class="spacer"></span>
  <button onclick="refresh()" id="refreshBtn">Refresh</button>
</header>
<main>
  <h2>This machine</h2>
  <div id="apps"></div>
  <div class="card">
    <div class="row">
      <span class="title">Create snapshot</span>
      <span class="spacer"></span>
      <span id="snapApps"></span>
      <button class="primary" onclick="doSnapshot()" id="snapBtn">Snapshot now</button>
    </div>
    <div class="sub" style="margin-top:8px">Running apps are skipped automatically (close them first).</div>
  </div>

  <h2>Local bundles</h2>
  <div id="local"></div>

  <h2>Tailnet machines</h2>
  <div id="peers"></div>
</main>
<div id="toasts"></div>

<script>
var state = null;

function el(tag, cls, txt) {
  var e = document.createElement(tag);
  if (cls) e.className = cls;
  if (txt !== undefined) e.textContent = txt;
  return e;
}
function toast(msg, kind) {
  var t = el("div", "toast " + (kind||""), msg);
  document.getElementById("toasts").appendChild(t);
  setTimeout(function(){ t.remove(); }, 6000);
}
function api(path, body) {
  var opt = { method: body ? "POST" : "GET" };
  if (body) { opt.headers = {"Content-Type":"application/json"}; opt.body = JSON.stringify(body); }
  return fetch(path, opt).then(function(r){ return r.json(); });
}

function appChip(a) {
  if (!a.installed) return null;
  var c = el("span", "chip " + (a.secretsCrossMachine ? "good":"warn"),
    a.secretsCrossMachine ? "secrets: portable" : "secrets: this machine only");
  c.title = a.note || "";
  return c;
}

function renderApps() {
  var wrap = document.getElementById("apps");
  wrap.innerHTML = "";
  state.apps.forEach(function(a){
    var card = el("div","card");
    var head = el("div","row");
    head.appendChild(el("span","title", a.id));
    if (!a.installed) { head.appendChild(el("span","chip","not installed")); }
    else { var ch=appChip(a); if (ch) head.appendChild(ch); }
    card.appendChild(head);
    (a.instances||[]).forEach(function(inst){
      var r = el("div","inst row");
      var left = el("div","");
      left.appendChild(el("div","title", inst.label + (inst.version? "  v"+inst.version : "")));
      left.appendChild(el("div","sub", inst.root));
      r.appendChild(left);
      r.appendChild(el("span","spacer"));
      if (inst.running) r.appendChild(el("span","chip bad","running"));
      else r.appendChild(el("span","chip good","idle"));
      card.appendChild(r);
    });
    wrap.appendChild(card);
  });
  // snapshot app checkboxes
  var sa = document.getElementById("snapApps"); sa.innerHTML="";
  state.apps.filter(function(a){return a.installed;}).forEach(function(a){
    var lb = el("label","ck");
    var ck = el("input"); ck.type="checkbox"; ck.checked=true; ck.dataset.app=a.id;
    lb.appendChild(ck); lb.appendChild(document.createTextNode(a.id));
    sa.appendChild(lb);
  });
}

function bundleRow(b, actions) {
  var card = el("div","card");
  var r = el("div","row");
  var left = el("div","");
  left.appendChild(el("div","title", b.id || b.name));
  var meta = (b.apps||[]).join(" + ") + "  ·  " + (b.sizeMB||0).toFixed(1) + " MB"
    + (b.createdAt? "  ·  "+b.createdAt : "") + (b.originHost? "  ·  from "+b.originHost+" ("+b.originOS+")":"");
  left.appendChild(el("div","sub", meta));
  r.appendChild(left);
  r.appendChild(el("span","spacer"));
  actions.forEach(function(btn){ r.appendChild(btn); });
  card.appendChild(r);
  return card;
}

function renderLocal() {
  var wrap = document.getElementById("local"); wrap.innerHTML="";
  if (!state.localBundles || !state.localBundles.length) {
    wrap.appendChild(el("div","empty","No local bundles yet. Snapshot to create one."));
    return;
  }
  state.localBundles.forEach(function(b){
    var dry = el("button",null,"Dry-run");
    dry.onclick = function(){ doRestore(b.name, true); };
    var res = el("button","primary","Restore");
    res.onclick = function(){
      if (confirm("Restore "+b.name+"? Existing profiles are backed up first.")) doRestore(b.name, false);
    };
    wrap.appendChild(bundleRow(b, [dry, res]));
  });
}

function renderPeers() {
  var wrap = document.getElementById("peers"); wrap.innerHTML="";
  if (!state.peers || !state.peers.length) {
    wrap.appendChild(el("div","empty","No tailnet peers found (is Tailscale up?)."));
    return;
  }
  state.peers.forEach(function(p){
    var card = el("div","card");
    var head = el("div","row");
    var dot = el("span","dot " + (p.online?"on":"off"));
    head.appendChild(dot);
    head.appendChild(el("span","peerhost", p.host));
    head.appendChild(el("span","sub", p.ip + "  ·  " + p.os));
    head.appendChild(el("span","spacer"));
    if (!p.online) head.appendChild(el("span","chip","offline"));
    else if (p.serving) head.appendChild(el("span","chip good","serving synckit"));
    else head.appendChild(el("span","chip warn","not serving"));
    card.appendChild(head);
    (p.bundles||[]).forEach(function(b){
      var fetchBtn = el("button",null,"Fetch");
      fetchBtn.onclick = function(){ doFetch(p.ip, b.name, false); };
      var impBtn = el("button","primary","Fetch & import");
      impBtn.onclick = function(){
        if (confirm("Fetch "+b.name+" from "+p.host+" and restore it here?")) doFetch(p.ip, b.name, true);
      };
      var row = bundleRow(b, [fetchBtn, impBtn]);
      row.style.marginTop = "10px";
      card.appendChild(row);
    });
    if (p.online && p.serving && (!p.bundles || !p.bundles.length))
      card.appendChild(el("div","sub","(no bundles offered)"));
    wrap.appendChild(card);
  });
}

function selectedSnapApps() {
  var out=[];
  document.querySelectorAll("#snapApps input:checked").forEach(function(c){ out.push(c.dataset.app); });
  return out;
}

function doSnapshot() {
  setBusy(true);
  api("/api/snapshot", { apps: selectedSnapApps() }).then(function(res){
    setBusy(false);
    if (!res.ok) { toast("Snapshot failed: "+res.error, "err"); return; }
    var n = (res.apps||[]).length;
    var msg = "Snapshot created: "+res.bundle+" ("+n+" instance(s))";
    if (res.skipped && res.skipped.length) msg += "\nSkipped: "+res.skipped.join("; ");
    toast(msg, "ok");
    refresh();
  });
}

function doRestore(name, dry) {
  setBusy(true);
  api("/api/restore", { bundle: name, dryRun: dry }).then(function(res){
    setBusy(false);
    if (!res.ok) { toast("Restore failed: "+res.error, "err"); return; }
    toast((dry?"Dry-run: ":"Restored: ")+summarize(res.outcomes), dry?"":"ok");
  });
}

function doFetch(ip, name, apply) {
  setBusy(true);
  api("/api/fetch", { peer: ip, name: name, apply: apply }).then(function(res){
    setBusy(false);
    if (!res.ok) { toast("Fetch failed: "+res.error, "err"); return; }
    var msg = "Fetched "+name;
    if (apply && res.outcomes) msg += "\n"+summarize(res.outcomes);
    toast(msg, "ok");
    refresh();
  });
}

function summarize(outcomes) {
  return (outcomes||[]).map(function(o){
    var s = o.app+"/"+o.instance+": ";
    if (o.skipped) return s+"SKIPPED ("+o.skipped+")";
    if (o.restored) return s+"ok"+(o.backup?" [backed up]":"");
    return s+"—";
  }).join("\n");
}

function setBusy(b) {
  document.querySelectorAll("button").forEach(function(x){ x.disabled=b; });
}

function refresh() {
  document.getElementById("refreshBtn").disabled = true;
  api("/api/overview").then(function(s){
    state = s;
    document.getElementById("machine").textContent =
      s.machine.hostname + " · " + s.machine.os + "/" + s.machine.arch;
    renderApps(); renderLocal(); renderPeers();
    document.getElementById("refreshBtn").disabled = false;
  }).catch(function(e){
    toast("Failed to load: "+e, "err");
    document.getElementById("refreshBtn").disabled = false;
  });
}

refresh();
setInterval(refresh, 15000);
</script>
</body>
</html>`

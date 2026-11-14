package main

import (
	"io"
	"net/http"
	"strings"
)

// The web UI: a single-page wiki (list, search, read, edit) talking to the
// /v1/wiki and /v1/memory endpoints behind Basic auth. No external assets.

const uiHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>mem — wiki</title>
<style>
  :root {
    --bg:#101114; --bg2:#17191e; --card:#1d2026; --border:#2b2e36; --border2:#3a3d46;
    --text:#f1f2f4; --text2:#c5c8d0; --muted:#999eaa; --accent:#a9c7ff; --green:#8ed8ad; --red:#ff9b9b;
  }
  * { box-sizing:border-box; margin:0; padding:0; }
  body { background:var(--bg); color:var(--text); font:14px/1.6 ui-sans-serif,system-ui,sans-serif; height:100vh; display:flex; flex-direction:column; }
  header { display:flex; align-items:center; gap:12px; padding:10px 16px; background:var(--bg2); border-bottom:1px solid var(--border); }
  header h1 { font-size:15px; font-weight:600; color:var(--accent); }
  header input { flex:1; max-width:420px; background:var(--card); border:1px solid var(--border); border-radius:8px; padding:7px 12px; color:var(--text); outline:none; }
  header input:focus { border-color:var(--border2); }
  button { background:var(--card); border:1px solid var(--border); color:var(--text2); border-radius:8px; padding:7px 14px; cursor:pointer; font-size:13px; }
  button:hover { border-color:var(--border2); color:var(--text); }
  button.primary { background:#2b3b58; border-color:#3d527c; color:var(--accent); }
  button.danger { color:var(--red); }
  main { flex:1; display:flex; overflow:hidden; }
  #sidebar { width:280px; border-right:1px solid var(--border); overflow-y:auto; padding:8px; }
  .page-item { padding:8px 10px; border-radius:8px; cursor:pointer; margin-bottom:2px; }
  .page-item:hover { background:var(--card); }
  .page-item.active { background:var(--card); border-left:2px solid var(--accent); }
  .page-item .t { font-weight:500; }
  .page-item .m { font-size:11px; color:var(--muted); }
  .tag { display:inline-block; font-size:10px; background:var(--card); border:1px solid var(--border); border-radius:6px; padding:0 6px; margin-right:3px; color:var(--muted); }
  #content { flex:1; overflow-y:auto; padding:24px 32px; }
  #content .meta { color:var(--muted); font-size:12px; margin-bottom:14px; }
  #content h2, #content h3 { margin:14px 0 8px; color:var(--accent); }
  #content ul { margin:6px 0 6px 20px; }
  #content code { background:var(--card); border:1px solid var(--border); border-radius:5px; padding:1px 5px; font-size:12px; }
  textarea { width:100%; min-height:60vh; background:var(--card); color:var(--text); border:1px solid var(--border); border-radius:10px; padding:14px; font:13px/1.6 ui-monospace,monospace; outline:none; resize:vertical; }
  .actions { display:flex; gap:8px; margin-bottom:14px; }
  .toast { position:fixed; bottom:16px; right:16px; background:var(--card); border:1px solid var(--border2); border-radius:10px; padding:10px 16px; font-size:13px; display:none; }
  .empty { color:var(--muted); padding:40px; text-align:center; }
  dialog { background:var(--bg2); color:var(--text); border:1px solid var(--border); border-radius:12px; padding:20px; }
  dialog input { background:var(--card); border:1px solid var(--border); border-radius:8px; padding:7px 12px; color:var(--text); width:100%; }
</style>
</head>
<body>
<header>
  <h1>mem wiki</h1>
  <input id="q" placeholder="ค้นหา (search)… " autocomplete="off">
  <button id="new-btn">+ New page</button>
</header>
<main>
  <nav id="sidebar"></nav>
  <section id="content"><div class="empty">Select a page or search.</div></section>
</main>
<dialog id="new-dlg">
  <p style="margin-bottom:10px">New page title</p>
  <form id="new-form"><input id="new-title" placeholder="title"><br><br>
  <button class="primary" type="submit">Create</button>
  <button type="button" onclick="dlg.close()">Cancel</button></form>
</dialog>
<div class="toast" id="toast"></div>
<script nonce="__CSP_NONCE__">
const $ = id => document.getElementById(id);
let current = null;

function toast(msg) {
  const t = $('toast'); t.textContent = msg; t.style.display = 'block';
  setTimeout(() => t.style.display = 'none', 2500);
}
async function api(path, opts) {
  const r = await fetch(path, opts);
  if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || r.status);
  return r.json();
}
function esc(s) { const d = document.createElement('div'); d.textContent = s; return d.innerHTML; }
function md(src) {
  let out = esc(src);
  out = out.replace(/&lt;(https?:[^&\s]+)&gt;/g, '<a href="$1">$1</a>');
  out = out.replace(/\*\*([^*]+)\*\*/g, '<b>$1</b>');
  const lines = out.split('\n'); let html = '', inList = false;
  for (const line of lines) {
    if (line.startsWith('### ')) { if (inList){html+='</ul>';inList=false;} html += '<h3>' + line.slice(4) + '</h3>'; }
    else if (line.startsWith('## ')) { if (inList){html+='</ul>';inList=false;} html += '<h2>' + line.slice(3) + '</h2>'; }
    else if (line.startsWith('- ')) { if (!inList){html+='<ul>';inList=true;} html += '<li>' + line.slice(2) + '</li>'; }
    else if (line.trim() === '') { if (inList){html+='</ul>';inList=false;} }
    else { if (inList){html+='</ul>';inList=false;} html += '<p>' + line + '</p>'; }
  }
  if (inList) html += '</ul>';
  return html;
}
async function loadList() {
  const data = await api('/v1/wiki/pages');
  const sb = $('sidebar'); sb.innerHTML = '';
  if (!data.pages.length) sb.innerHTML = '<div class="empty">No pages yet</div>';
  for (const p of data.pages) {
    const d = document.createElement('div');
    d.className = 'page-item' + (current === p.slug ? ' active' : '');
    d.innerHTML = '<div class="t">' + esc(p.title) + '</div><div class="m">' +
      esc((p.updated_at || '').slice(0, 16).replace('T', ' ')) + ' · ' + esc(p.updated_by || '') + '</div>';
    d.onclick = () => showPage(p.slug);
    sb.appendChild(d);
  }
}
async function showPage(slug) {
  try {
    const p = await api('/v1/wiki/pages/' + encodeURIComponent(slug));
    current = slug;
    const tags = (p.tags || []).map(t => '<span class="tag">' + esc(t) + '</span>').join(' ');
    $('content').innerHTML =
      '<div class="actions"><button id="edit-btn">Edit</button>' +
      '<button id="del-btn" class="danger">Delete</button></div>' +
      '<h1 style="margin-bottom:4px">' + esc(p.title) + '</h1>' +
      '<div class="meta">' + tags + ' · updated ' + esc((p.updated_at || '').slice(0, 19).replace('T', ' ')) +
      ' by ' + esc(p.updated_by || '?') + '</div><div id="body">' + md(p.body) + '</div>';
    $('edit-btn').onclick = () => editPage(slug, p.body);
    $('del-btn').onclick = async () => {
      if (!confirm('Move "' + p.title + '" to trash?')) return;
      await api('/v1/wiki/pages/' + encodeURIComponent(slug), { method: 'DELETE' });
      current = null; toast('Moved to trash'); $('content').innerHTML = '<div class="empty">Deleted.</div>';
      loadList();
    };
    loadList();
  } catch (e) { toast('Error: ' + e.message); }
}
function editPage(slug, body) {
  $('content').innerHTML =
    '<div class="actions"><button id="save-btn" class="primary">Save</button>' +
    '<button id="cancel-btn">Cancel</button></div>' +
    '<textarea id="ta"></textarea>';
  $('ta').value = body;
  $('cancel-btn').onclick = () => showPage(slug);
  $('save-btn').onclick = async () => {
    try {
      await api('/v1/wiki/pages/' + encodeURIComponent(slug), {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ body: $('ta').value })
      });
      toast('Saved'); showPage(slug);
    } catch (e) { toast('Error: ' + e.message); }
  };
}
async function search(q) {
  try {
    const data = await api('/v1/memory/search?q=' + encodeURIComponent(q) + '&limit=25');
    current = null; loadList();
    if (!data.results.length) { $('content').innerHTML = '<div class="empty">No matches.</div>'; return; }
    let html = '<h2 style="margin-bottom:12px">Search: ' + esc(q) + '</h2>';
    for (const r of data.results) {
      html += '<div class="page-item" onclick="showPage(\'' + esc(r.page) + '\')" style="margin-bottom:10px">' +
        '<div class="t">' + esc(r.title || r.topic || r.page) + '</div><div class="m">' + esc(r.body) + '</div></div>';
    }
    $('content').innerHTML = html;
  } catch (e) { toast('Error: ' + e.message); }
}
$('q').addEventListener('keydown', e => { if (e.key === 'Enter' && e.target.value.trim()) search(e.target.value.trim()); });
$('new-btn').onclick = () => { $('new-dlg').showModal(); $('new-title').focus(); };
$('new-form').onsubmit = async e => {
  e.preventDefault();
  const title = $('new-title').value.trim();
  if (!title) return;
  try {
    await api('/v1/wiki/pages', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title, body: '## Facts\n' })
    });
    $('new-dlg').close(); $('new-title').value = '';
    await loadList(); showPage(await firstSlug(title));
  } catch (err) { toast('Error: ' + err.message); }
};
async function firstSlug(title) {
  const d = await api('/v1/wiki/pages');
  const hit = d.pages.find(p => p.title.toLowerCase() === title.toLowerCase());
  return hit ? hit.slug : '';
}
loadList();
</script>
</body>
</html>
`

// handleUI serves the wiki UI.
func handleUI(w http.ResponseWriter, r *http.Request) {
	nonce := newNonce()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", contentSecurityPolicy(nonce))
	w.Header().Set("Cache-Control", "no-store")
	io.WriteString(w, strings.ReplaceAll(uiHTML, "__CSP_NONCE__", nonce))
}

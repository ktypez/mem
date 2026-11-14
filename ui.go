package main

import (
	"io"
	"net/http"
	"strings"
)

// The web UI: a single-page wiki (list, search, read, edit) talking to the
// /v1/wiki and /v1/memory endpoints behind Basic auth. Styled with the V5
// "Paper Pop" design (warm paper, hard sticker shadows, four accent palettes)
// from data-ledger/design/v5, and laid out mobile-first: an off-canvas drawer
// on phones, a fixed sidebar on wide screens. No external assets, no build
// step. The whole page is one Go raw string, so it must not contain backticks.

const uiHTML = `<!DOCTYPE html>
<html lang="th" data-palette="matcha" data-mode="auto">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover">
<meta name="color-scheme" content="light dark">
<title>mem — wiki</title>
<style>
/* ---------- V5 Paper Pop tokens ---------- */
:root{
  --paper:#f6f2e9; --paper-2:#fdfaf3; --card:#fffdf6;
  --ink:#26251f; --ink-soft:#55534b; --ink-faint:#8c8880;
  --rule:#e3ddd0; --rule-2:#efe9dd;
  --acc:#158068; --acc-strong:#0e5f4d; --acc-ink:#ffffff; --tint:#e2f2e8;
  --pop:#f2593f; --pop-ink:#ffffff; --pop-tint:#fbe4de;
  --danger:#b43434; --danger-tint:#f6dede;
  --shadow:3px 3px 0 var(--rule-2); --shadow-sm:2px 2px 0 var(--rule-2);
  --r-sm:10px; --r-md:16px; --r-lg:24px; --pad:16px;
  --font-ui:"Sora","Noto Sans Thai",ui-sans-serif,system-ui,-apple-system,sans-serif;
  --font-mono:"JetBrains Mono",ui-monospace,"SF Mono",Menlo,monospace;
}
:root[data-palette="coral"]{ --acc:#e8503a; --acc-strong:#c23a26; --tint:#fce7e1; --pop:#17918c; --pop-tint:#d9efee; }
:root[data-palette="blueberry"]{ --acc:#3e63dd; --acc-strong:#2b48a8; --tint:#e7ecfc; --pop:#f2a93b; --pop-tint:#faecd2; }
:root[data-palette="butter"]{ --acc:#c77d0a; --acc-strong:#9c6107; --tint:#faedd3; --pop:#6d5bd0; --pop-tint:#e7e2f8; }
@media (prefers-color-scheme:dark){
  :root[data-mode="auto"]{
    --paper:#232219; --paper-2:#2b2a22; --card:#2f2e25;
    --ink:#efeade; --ink-soft:#c3bdae; --ink-faint:#938d80;
    --rule:#413f34; --rule-2:#35342b; --shadow:3px 3px 0 #17160f; --shadow-sm:2px 2px 0 #17160f;
  }
  :root[data-mode="auto"][data-palette="matcha"]{ --tint:#1d322a; }
  :root[data-mode="auto"][data-palette="coral"]{ --tint:#3a231d; --pop-tint:#1b2f2e; }
  :root[data-mode="auto"][data-palette="blueberry"]{ --tint:#232a44; --pop-tint:#3a2f18; }
  :root[data-mode="auto"][data-palette="butter"]{ --tint:#3a3218; --pop-tint:#2a2544; }
}
*{box-sizing:border-box;margin:0;padding:0}
html{height:100%}
body{
  height:100dvh; display:flex; flex-direction:column; background:var(--paper); color:var(--ink);
  font:15px/1.6 var(--font-ui); overflow:hidden;
  background-image:radial-gradient(var(--rule) 1px,transparent 1px); background-size:22px 22px;
}
button{
  font:inherit; font-weight:600; font-size:14px; cursor:pointer;
  background:var(--card); color:var(--ink); border:1px solid var(--rule);
  border-radius:var(--r-sm); padding:10px 14px; box-shadow:var(--shadow-sm);
  min-height:42px; transition:transform .08s ease,box-shadow .08s ease,background .15s;
}
button:hover{background:var(--paper-2)}
button:active{transform:translate(2px,2px);box-shadow:none}
button:focus-visible{outline:2px solid var(--acc);outline-offset:2px}
button.sticker{background:var(--acc);color:var(--acc-ink);border-color:var(--acc-strong);box-shadow:var(--shadow)}
button.sticker:hover{background:var(--acc-strong)}
button.pop{background:var(--pop);color:var(--pop-ink);border-color:var(--pop);box-shadow:var(--shadow)}
button.ghost{background:transparent;border-color:transparent;box-shadow:none}
button.ghost:hover{background:var(--rule-2)}
button.icon{padding:10px;min-width:42px}
button:disabled{opacity:.5;cursor:not-allowed}
input,textarea{font:inherit;color:var(--ink);background:var(--card);border:1px solid var(--rule);border-radius:var(--r-sm);padding:9px 12px;min-height:42px;outline:none}
input:focus,textarea:focus{border-color:var(--acc);box-shadow:var(--shadow-sm)}
a{color:var(--acc);text-decoration:none}
a:hover{text-decoration:underline}
::-webkit-scrollbar{width:10px;height:10px}
::-webkit-scrollbar-thumb{background:var(--rule);border-radius:10px}

/* ---------- header ---------- */
header{
  display:flex;align-items:center;gap:8px;padding:10px 12px;
  background:var(--paper-2);border-bottom:1.5px solid var(--rule);z-index:20;flex-wrap:wrap;
}
.brand{display:flex;align-items:center;gap:8px;font-weight:800;font-size:16px;letter-spacing:-.01em}
.brand .dot{width:12px;height:12px;border-radius:50%;background:var(--acc);box-shadow:var(--shadow-sm);flex-shrink:0}
.brand .sub{font-weight:500;font-size:12px;color:var(--ink-faint)}
.grow{flex:1}
.search{position:relative;flex:1;min-width:150px;max-width:520px;display:flex}
.search input{width:100%;padding-left:34px}
.search .ico{position:absolute;left:11px;top:50%;transform:translateY(-50%);color:var(--ink-faint);pointer-events:none;font-size:13px}
.palette-bar{display:flex;gap:6px}
.palette-bar .p{width:22px;height:22px;border-radius:50%;border:2px solid var(--rule);cursor:pointer;padding:0;min-height:0;box-shadow:none}
.palette-bar .p[aria-pressed="true"]{border-color:var(--ink)}

/* ---------- layout ---------- */
.main{flex:1;display:flex;min-height:0;position:relative}
nav.sidebar{
  width:280px;flex-shrink:0;background:var(--card);border-right:1.5px solid var(--rule);
  display:flex;flex-direction:column;min-height:0;
}
.sidebar .tools{display:flex;gap:6px;padding:10px;border-bottom:1px solid var(--rule)}
.sidebar .tools button{flex:1;padding:8px;min-height:38px;font-size:13px}
.count{font-size:12px;color:var(--ink-faint);font-weight:600;text-transform:uppercase;letter-spacing:.05em;padding:10px 12px 4px}
.list{flex:1;overflow-y:auto;padding:0 8px 12px}
.item{padding:10px 12px;border-radius:var(--r-sm);cursor:pointer;border:1px solid transparent;margin-bottom:2px}
.item:hover{background:var(--paper-2)}
.item.active{background:var(--tint);border-color:var(--acc)}
.item .t{font-weight:600;font-size:14px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.item .m{font-size:12px;color:var(--ink-faint);display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin-top:2px}
.tag{display:inline-block;font-size:11px;background:var(--tint);color:var(--acc);border-radius:999px;padding:1px 8px;font-weight:600}

section.content{flex:1;overflow-y:auto;min-width:0;padding:22px}
.empty{color:var(--ink-faint);text-align:center;padding:60px 20px;font-size:14px}
.meta{display:flex;flex-wrap:wrap;gap:8px;align-items:center;color:var(--ink-faint);font-size:13px;margin-bottom:14px}
.actions{display:flex;gap:8px;margin-bottom:16px;flex-wrap:wrap}
.prose{background:var(--card);border:1px solid var(--rule);border-radius:var(--r-md);box-shadow:var(--shadow);padding:20px 24px;max-width:900px}
.prose h1{font-size:24px;font-weight:800;margin:4px 0 10px;letter-spacing:-.02em}
.prose h2{font-size:18px;font-weight:700;color:var(--acc);margin:20px 0 8px}
.prose h3{font-size:15px;font-weight:700;margin:16px 0 6px}
.prose p{margin:8px 0}
.prose ul,.prose ol{margin:8px 0 8px 22px}
.prose li{margin:3px 0}
.prose code{font-family:var(--font-mono);font-size:13px;background:var(--paper-2);border:1px solid var(--rule);border-radius:6px;padding:1px 6px}
.prose pre{background:var(--paper-2);border:1px solid var(--rule);border-radius:var(--r-sm);padding:12px;overflow-x:auto;margin:10px 0}
.prose pre code{background:none;border:none;padding:0}
.prose a{text-decoration:underline}
textarea.editor{width:100%;min-height:60vh;font:14px/1.65 var(--font-mono);resize:vertical}
.hit{background:var(--card);border:1px solid var(--rule);border-radius:var(--r-sm);padding:12px 14px;margin-bottom:10px;cursor:pointer;box-shadow:var(--shadow-sm)}
.hit:hover{border-color:var(--acc)}
.hit .t{font-weight:600;margin-bottom:2px}
.hit .x{font-size:13px;color:var(--ink-soft);font-family:var(--font-mono);white-space:pre-wrap;word-break:break-word;max-height:5.2em;overflow:hidden}
.scrim{position:fixed;inset:0;background:rgba(20,20,16,.45);z-index:29;display:none}
.toast{position:fixed;left:50%;bottom:20px;transform:translateX(-50%) translateY(80px);background:var(--ink);color:var(--paper);padding:11px 20px;border-radius:var(--r-md);font-size:14px;font-weight:600;box-shadow:var(--shadow);z-index:60;opacity:0;transition:transform .2s,opacity .2s}
.toast.show{transform:translateX(-50%) translateY(0);opacity:1}
dialog{border:1px solid var(--rule);border-radius:var(--r-md);box-shadow:var(--shadow);background:var(--paper-2);color:var(--ink);padding:20px;width:min(92vw,420px)}
dialog::backdrop{background:rgba(20,20,16,.45)}
dialog h2{font-size:17px;margin-bottom:12px}
dialog input{width:100%;margin-bottom:14px}
dialog .row{display:flex;gap:8px;justify-content:flex-end}

/* ---------- mobile ---------- */
.hamburger{display:none}
@media (max-width:760px){
  .main{display:block}
  nav.sidebar{
    position:fixed;top:0;left:0;bottom:0;width:min(85vw,320px);z-index:30;
    transform:translateX(-102%);transition:transform .22s ease;box-shadow:var(--shadow);
  }
  nav.sidebar.open{transform:none}
  .scrim.show{display:block}
  .hamburger{display:inline-flex}
  section.content{padding:16px}
  .prose{padding:14px 16px;border-radius:var(--r-sm)}
  .search{order:3;flex-basis:100%;max-width:none}
  .brand .sub{display:none}
}
@media (max-width:420px){ .brand span.name{display:none} }
</style>
</head>
<body>
<header>
  <button class="ghost icon hamburger" id="menu-btn" aria-label="เมนู" aria-expanded="false">☰</button>
  <div class="brand"><span class="dot"></span><span class="name">mem</span><span class="sub">wiki</span></div>
  <div class="grow"></div>
  <div class="search">
    <span class="ico">⌕</span>
    <input id="q" type="search" placeholder="ค้นหา…" autocomplete="off" aria-label="ค้นหา">
  </div>
  <div class="palette-bar" id="palettes" role="group" aria-label="ชุดสี"></div>
  <button class="sticker" id="new-btn">+ สร้าง</button>
</header>

<div class="main">
  <nav class="sidebar" id="sidebar">
    <div class="tools">
      <button id="refresh-btn" title="โหลดใหม่">↻ โหลดใหม่</button>
      <button id="all-btn" title="หน้าแรก">ทั้งหมด</button>
    </div>
    <div class="count" id="count">หน้า</div>
    <div class="list" id="list"></div>
  </nav>
  <div class="scrim" id="scrim"></div>
  <section class="content" id="content"><div class="empty">เลือกหน้าจากเมนู หรือกด + สร้าง เพื่อเริ่มเขียน</div></section>
</div>

<dialog id="new-dlg">
  <h2>หน้าใหม่</h2>
  <form id="new-form">
    <input id="new-title" placeholder="ชื่อหน้า…" aria-label="ชื่อหน้า" required>
    <div class="row"><button type="button" id="new-cancel">ยกเลิก</button><button class="sticker" type="submit">สร้าง</button></div>
  </form>
</dialog>

<div class="toast" id="toast" role="status" aria-live="polite"></div>

<script nonce="__CSP_NONCE__">
"use strict";
const $ = id => document.getElementById(id);
const PALETTES = ["matcha", "coral", "blueberry", "butter"];
const PCOLOR = { matcha:"#158068", coral:"#e8503a", blueberry:"#3e63dd", butter:"#c77d0a" };
let current = null;

function toast(msg){
  const t = $("toast"); t.textContent = msg; t.classList.add("show");
  clearTimeout(toast._t); toast._t = setTimeout(() => t.classList.remove("show"), 2600);
}
async function api(path, opts){
  const r = await fetch(path, opts);
  if(!r.ok){ let e = r.status; try{ e = (await r.json()).error || e; }catch(_){} throw new Error(e); }
  if(r.status === 204) return null;
  return r.json();
}
function esc(s){ const d = document.createElement("div"); d.textContent = s == null ? "" : String(s); return d.innerHTML; }
function md(src){
  let out = esc(src);
  out = out.replace(/&lt;(https?:[^&\s]+)&gt;/g, "<a href=\"$1\">$1</a>");
  out = out.replace(/\*\*([^*]+)\*\*/g, "<b>$1</b>");
  out = out.replace(/(^|[^*])\*([^*\n]+)\*(?=[^*]|$)/gm, "$1<i>$2</i>");
  const lines = out.split("\n"); let html = "", inList = false;
  for(const line of lines){
    if(line.startsWith("### ")){ if(inList){html+="</ul>";inList=false;} html += "<h3>" + line.slice(4) + "</h3>"; }
    else if(line.startsWith("## ")){ if(inList){html+="</ul>";inList=false;} html += "<h2>" + line.slice(3) + "</h2>"; }
    else if(line.startsWith("# ")){ if(inList){html+="</ul>";inList=false;} html += "<h1>" + line.slice(2) + "</h1>"; }
    else if(line.startsWith("- ")){ if(!inList){html+="<ul>";inList=true;} html += "<li>" + line.slice(2) + "</li>"; }
    else if(line.trim() === ""){ if(inList){html+="</ul>";inList=false;} }
    else { if(inList){html+="</ul>";inList=false;} html += "<p>" + line + "</p>"; }
  }
  if(inList) html += "</ul>";
  return html;
}

function openMenu(){ $("sidebar").classList.add("open"); $("scrim").classList.add("show"); $("menu-btn").setAttribute("aria-expanded","true"); }
function closeMenu(){ $("sidebar").classList.remove("open"); $("scrim").classList.remove("show"); $("menu-btn").setAttribute("aria-expanded","false"); }

async function loadList(){
  try{
    const data = await api("/v1/wiki/pages");
    const el = $("list"); el.innerHTML = "";
    $("count").textContent = data.pages.length + " หน้า";
    if(!data.pages.length){ el.innerHTML = "<div class=\"empty\">ยังไม่มีหน้า</div>"; return; }
    for(const p of data.pages){
      const d = document.createElement("div");
      d.className = "item" + (current === p.slug ? " active" : "");
      const tags = (p.tags || []).map(t => "<span class=\"tag\">" + esc(t) + "</span>").join("");
      d.innerHTML = "<div class=\"t\">" + esc(p.title) + "</div><div class=\"m\">" +
        esc((p.updated_at || "").slice(0,10)) + (p.updated_by ? " · " + esc(p.updated_by) : "") + tags + "</div>";
      d.onclick = () => { showPage(p.slug); closeMenu(); };
      el.appendChild(d);
    }
  }catch(e){ toast("Error: " + e.message); }
}

async function showPage(slug){
  try{
    const p = await api("/v1/wiki/pages/" + encodeURIComponent(slug));
    current = slug;
    const tags = (p.tags || []).map(t => "<span class=\"tag\">" + esc(t) + "</span>").join(" ");
    $("content").innerHTML =
      "<div class=\"actions\"><button class=\"sticker\" id=\"edit-btn\">แก้ไข</button>" +
      "<button id=\"del-btn\">ย้ายไปถังขยะ</button></div>" +
      "<h1 style=\"font-size:26px;letter-spacing:-.02em;margin-bottom:6px\">" + esc(p.title) + "</h1>" +
      "<div class=\"meta\">" + tags + " <span>" + esc((p.updated_at||"").slice(0,16).replace("T"," ")) + "</span>" +
      (p.updated_by ? "<span>โดย " + esc(p.updated_by) + "</span>" : "") + "</div>" +
      "<div class=\"prose\">" + md(p.body) + "</div>";
    $("edit-btn").onclick = () => editPage(slug, p.body);
    $("del-btn").onclick = async () => {
      if(!confirm("ย้าย \"" + p.title + "\" ไปถังขยะ?")) return;
      try{ await api("/v1/wiki/pages/" + encodeURIComponent(slug), { method:"DELETE" });
        current = null; toast("ย้ายไปถังขยะแล้ว"); loadList();
        $("content").innerHTML = "<div class=\"empty\">ย้ายไปถังขยะแล้ว</div>";
      }catch(e){ toast("Error: " + e.message); }
    };
    loadList();
  }catch(e){ toast("Error: " + e.message); }
}

function editPage(slug, body){
  $("content").innerHTML =
    "<div class=\"actions\"><button class=\"sticker\" id=\"save-btn\">บันทึก</button>" +
    "<button id=\"cancel-btn\">ยกเลิก</button></div>" +
    "<textarea class=\"editor\" id=\"ta\" aria-label=\"เนื้อหา\"></textarea>";
  $("ta").value = body;
  $("ta").focus();
  $("cancel-btn").onclick = () => showPage(slug);
  $("save-btn").onclick = async () => {
    try{ await api("/v1/wiki/pages/" + encodeURIComponent(slug),
      { method:"PUT", headers:{ "Content-Type":"application/json" }, body: JSON.stringify({ body: $("ta").value }) });
      toast("บันทึกแล้ว"); showPage(slug);
    }catch(e){ toast("Error: " + e.message); }
  };
}

async function doSearch(q){
  try{
    const data = await api("/v1/memory/search?q=" + encodeURIComponent(q) + "&limit=25");
    current = null; loadList();
    if(!data.results.length){ $("content").innerHTML = "<div class=\"empty\">ไม่พบผลลัพธ์สำหรับ “" + esc(q) + "”</div>"; return; }
    let html = "<h2 style=\"margin-bottom:14px\">ค้นหา: “" + esc(q) + "” <span class=\"meta\" style=\"display:inline;font-size:13px\">(" + data.results.length + ")</span></h2>";
    for(const r of data.results){
      const slug = r.page || r.slug;
      html += "<div class=\"hit\" data-slug=\"" + esc(slug) + "\"><div class=\"t\">" + esc(r.title || r.topic || slug) + "</div>" +
        "<div class=\"x\">" + esc((r.body || "").slice(0,300)) + "</div></div>";
    }
    $("content").innerHTML = html;
    $("content").querySelectorAll(".hit").forEach(el => el.onclick = () => {
      const s = el.getAttribute("data-slug"); if(s) showPage(s);
    });
  }catch(e){ toast("Error: " + e.message); }
}

function buildPalettes(){
  const bar = $("palettes");
  for(const p of PALETTES){
    const b = document.createElement("button");
    b.className = "p"; b.style.background = PCOLOR[p];
    b.title = p; b.setAttribute("aria-label", "ชุดสี " + p);
    b.setAttribute("aria-pressed", String(document.documentElement.dataset.palette === p));
    b.onclick = () => { document.documentElement.dataset.palette = p; localStorage.setItem("mem-palette", p);
      bar.querySelectorAll(".p").forEach(x => x.setAttribute("aria-pressed","false")); b.setAttribute("aria-pressed","true"); };
    bar.appendChild(b);
  }
}

$("menu-btn").onclick = () => { $("sidebar").classList.contains("open") ? closeMenu() : openMenu(); };
$("scrim").onclick = closeMenu;
$("refresh-btn").onclick = () => { loadList(); toast("โหลดใหม่แล้ว"); };
$("all-btn").onclick = () => { current = null; loadList(); $("content").innerHTML = "<div class=\"empty\">เลือกหน้าจากเมนู</div>"; closeMenu(); };
$("q").addEventListener("keydown", e => { if(e.key === "Enter" && e.target.value.trim()){ doSearch(e.target.value.trim()); closeMenu(); } });
$("new-btn").onclick = () => { $("new-dlg").showModal(); $("new-title").focus(); };
$("new-cancel").onclick = () => $("new-dlg").close();
$("new-form").onsubmit = async e => {
  e.preventDefault();
  const title = $("new-title").value.trim(); if(!title) return;
  try{
    const created = await api("/v1/wiki/pages", { method:"POST", headers:{ "Content-Type":"application/json" },
      body: JSON.stringify({ title: title, body: "## Facts\n- " + title + "\n" }) });
    $("new-dlg").close(); $("new-title").value = "";
    await loadList(); toast("สร้างแล้ว");
    if(created && created.slug) showPage(created.slug);
  }catch(err){ toast("Error: " + err.message); }
};
document.addEventListener("keydown", e => {
  if(e.key === "Escape") closeMenu();
  if(e.key === "/" && !/^(INPUT|TEXTAREA)$/.test(document.activeElement.tagName)){ e.preventDefault(); $("q").focus(); }
});

(function initTheme(){
  const saved = localStorage.getItem("mem-palette");
  if(saved && PALETTES.indexOf(saved) >= 0) document.documentElement.dataset.palette = saved;
  buildPalettes();
})();
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

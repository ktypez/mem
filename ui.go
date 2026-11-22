package main

import (
	"io"
	"net/http"
	"strings"
)

// The web UI: a single-page wiki (list, search, read, edit) talking to the
// /v1/wiki and /v1/memory endpoints behind Basic auth. Styled with the MCKY UI
// V6 "Work Shell" design (sky canvas, cream window shell, ink outlines, hard
// offset shadows, Space Grotesk / Sarabun / DM Mono) and laid out mobile-first:
// an off-canvas drawer on phones, a fixed sidebar on wide screens. The webfonts
// are embedded in the binary and served from /ui/fonts (fonts.go), so there are
// no external assets and no build step. The whole page is one Go raw string, so
// it must not contain backticks.

const uiHTML = `<!DOCTYPE html>
<html lang="th" data-palette="blue" data-mode="auto">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover">
<meta name="color-scheme" content="light dark">
<title>mem — wiki</title>
<style>
/* ---------- V6 webfonts (self-hosted, thai + latin subsets) ---------- */
@font-face{font-family:'DM Mono';font-style:normal;font-weight:400;font-display:swap;src:url('/ui/fonts/dm-mono-400-latin.woff2') format('woff2');unicode-range:U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD}
@font-face{font-family:'DM Mono';font-style:normal;font-weight:500;font-display:swap;src:url('/ui/fonts/dm-mono-500-latin.woff2') format('woff2');unicode-range:U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD}
@font-face{font-family:'Sarabun';font-style:normal;font-weight:400;font-display:swap;src:url('/ui/fonts/sarabun-400-thai.woff2') format('woff2');unicode-range:U+02D7, U+0303, U+0331, U+0E01-0E5B, U+200C-200D, U+25CC}
@font-face{font-family:'Sarabun';font-style:normal;font-weight:400;font-display:swap;src:url('/ui/fonts/sarabun-400-latin.woff2') format('woff2');unicode-range:U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD}
@font-face{font-family:'Sarabun';font-style:normal;font-weight:600;font-display:swap;src:url('/ui/fonts/sarabun-600-thai.woff2') format('woff2');unicode-range:U+02D7, U+0303, U+0331, U+0E01-0E5B, U+200C-200D, U+25CC}
@font-face{font-family:'Sarabun';font-style:normal;font-weight:600;font-display:swap;src:url('/ui/fonts/sarabun-600-latin.woff2') format('woff2');unicode-range:U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD}
@font-face{font-family:'Space Grotesk';font-style:normal;font-weight:500;font-display:swap;src:url('/ui/fonts/space-grotesk-500-latin.woff2') format('woff2');unicode-range:U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD}
@font-face{font-family:'Space Grotesk';font-style:normal;font-weight:700;font-display:swap;src:url('/ui/fonts/space-grotesk-700-latin.woff2') format('woff2');unicode-range:U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD}

/* ---------- MCKY UI V6 "Work Shell" tokens ---------- */
:root{
  --v6-sky:#9dd2f7; --v6-cream:#f8f6f0; --v6-cream-2:#efece2; --v6-paper:#ffffff;
  --v6-ink:#111111; --v6-muted:#4a4a4a; --v6-faint:#6b6b6b; --v6-rule:#ece7d9;
  --v6-yellow:#ffd21f; --v6-yellow-tint:#fff4c2;
  --v6-blue:#4d83ff; --v6-blue-tint:#e4edff;
  --v6-purple:#8b73e8; --v6-purple-tint:#ede7ff;
  --v6-orange:#f47b20; --v6-orange-tint:#ffe4ce;
  --v6-red:#e34e4e; --v6-red-tint:#fce6e6;
  --outline:2.5px solid var(--v6-ink);
  --shadow:5px 5px 0 var(--v6-ink); --shadow-sm:3px 3px 0 var(--v6-ink);
  --r:20px; --r-sm:12px;
  --font-display:"Space Grotesk","Sarabun",system-ui,sans-serif;
  --font-body:"Sarabun","Noto Sans Thai",system-ui,sans-serif;
  --font-mono:"DM Mono",ui-monospace,Menlo,monospace;
  --acc:#1f4fd8; --acc-hue:var(--v6-blue); --acc-tint:var(--v6-blue-tint); --acc-ink:#ffffff;
  --danger:#b42318; --danger-tint:var(--v6-red-tint);
}
:root[data-palette="purple"]{ --acc:#5a45c9; --acc-hue:var(--v6-purple); --acc-tint:var(--v6-purple-tint); }
:root[data-palette="orange"]{ --acc:#b3540a; --acc-hue:var(--v6-orange); --acc-tint:var(--v6-orange-tint); }
:root[data-palette="yellow"]{ --acc:#8a6100; --acc-hue:var(--v6-yellow); --acc-tint:var(--v6-yellow-tint); }
@media (prefers-color-scheme:dark){
  :root[data-mode="auto"]{
    --v6-sky:#20252b; --v6-cream:#242a30; --v6-cream-2:#2a3138; --v6-paper:#2e353c;
    --v6-ink:#f4f1e8; --v6-muted:#c3c0b6; --v6-faint:#9c9a92; --v6-rule:#3a424a;
    --v6-yellow-tint:#3a3524; --v6-blue-tint:#26324a; --v6-purple-tint:#322b4a;
    --v6-orange-tint:#3d2f22; --v6-red-tint:#3d2626;
    --v6-blue:#7fa8ff; --v6-purple:#a995f2; --v6-orange:#ff9a4d; --v6-red:#ff7a72;
    --shadow:5px 5px 0 #0b0d10; --shadow-sm:3px 3px 0 #0b0d10;
    --acc:#9db9ff; --acc-ink:#111111; --danger:#ff938a;
  }
  :root[data-mode="auto"][data-palette="purple"]{ --acc:#b6a4ff; }
  :root[data-mode="auto"][data-palette="orange"]{ --acc:#ffab6b; }
  :root[data-mode="auto"][data-palette="yellow"]{ --acc:#ffe27a; }
}
*{box-sizing:border-box;margin:0;padding:0}
html{height:100%}
body{
  height:100dvh; overflow:hidden; background-color:var(--v6-sky); color:var(--v6-ink);
  font:15px/1.6 var(--font-body);
  background-image:radial-gradient(rgb(17 17 17 / .13) 1px,transparent 1.2px);
  background-size:24px 24px; background-position:-6px -6px;
}
@media (prefers-color-scheme:dark){
  :root[data-mode="auto"] body{ background-image:radial-gradient(rgb(244 241 232 / .08) 1px,transparent 1.2px); }
}
::selection{background:var(--v6-yellow);color:#111111}
:focus-visible{outline:3px solid var(--v6-ink);outline-offset:3px;border-radius:6px}
a{color:var(--acc);text-decoration:none}
a:hover{text-decoration:underline}
::-webkit-scrollbar{width:10px;height:10px}
::-webkit-scrollbar-thumb{background:var(--v6-rule);border-radius:10px}

button{
  font:500 11px/1.2 var(--font-mono); letter-spacing:.08em; text-transform:uppercase;
  cursor:pointer; background:var(--v6-paper); color:var(--v6-ink);
  border:2px solid var(--v6-ink); border-radius:999px; padding:9px 13px; min-height:40px;
  transition:transform .12s ease,box-shadow .12s ease,background .12s ease;
}
button:hover{background:var(--v6-yellow);transform:translate(-1px,-1px);box-shadow:2px 2px 0 var(--v6-ink)}
button:active{transform:translate(1px,1px);box-shadow:none}
button:disabled{opacity:.45;cursor:not-allowed;transform:none;box-shadow:none}
button.sticker{
  font:700 12.5px/1.2 var(--font-display); letter-spacing:.02em; text-transform:none;
  background:var(--v6-yellow); color:#111111; border:var(--outline); border-radius:13px;
  box-shadow:var(--shadow-sm); padding:10px 16px; min-height:42px;
}
button.sticker:hover{background:var(--v6-yellow);transform:translate(-2px,-2px);box-shadow:5px 5px 0 var(--v6-ink)}
button.sticker:active{transform:translate(1px,1px);box-shadow:1px 1px 0 var(--v6-ink)}
button.ghost{background:var(--v6-paper)}
button.icon{padding:9px;min-width:40px}
input,textarea{
  font:inherit; font:12.5px/1.5 var(--font-mono); color:var(--v6-ink); background:var(--v6-paper);
  border:var(--outline); border-radius:var(--r-sm); padding:9px 12px; min-height:42px; outline:none;
  transition:box-shadow .12s ease,transform .12s ease;
}
input:focus,textarea:focus{box-shadow:4px 4px 0 var(--v6-ink)}
input::placeholder,textarea::placeholder{color:var(--v6-faint)}

/* ---------- desk + window shell ---------- */
.desk{height:100dvh;padding:clamp(0px,2.2vw,24px);display:flex}
.shell{
  flex:1; min-width:0; min-height:0; display:flex; flex-direction:column;
  background:var(--v6-cream); border:var(--outline); border-radius:var(--r);
  box-shadow:8px 8px 0 rgb(17 17 17 / .9); overflow:clip;
}
@media (prefers-color-scheme:dark){
  :root[data-mode="auto"] .shell{ box-shadow:8px 8px 0 #0b0d10; }
}

/* ---------- window bar ---------- */
header{
  position:sticky;top:0;z-index:20;display:flex;align-items:center;gap:10px;flex-wrap:wrap;
  padding:9px 14px;background:var(--v6-paper);border-bottom:var(--outline);
}
.dots{display:inline-flex;gap:5px;flex:none}
.dots i{display:block;width:11px;height:11px;border-radius:50%;border:2px solid var(--v6-ink)}
.dots i:nth-child(1){background:var(--v6-yellow)}
.dots i:nth-child(2){background:var(--v6-blue)}
.dots i:nth-child(3){background:var(--v6-red)}
.brand{display:flex;align-items:center;gap:9px;font:700 16px/1.1 var(--font-display);letter-spacing:-.02em}
.brand .dot{width:13px;height:13px;border-radius:50%;background:var(--acc-hue);border:2px solid var(--v6-ink);flex-shrink:0}
.brand .sub{font:500 9px/1 var(--font-mono);letter-spacing:.16em;text-transform:uppercase;color:var(--v6-faint)}
.grow{flex:1}
.search{position:relative;flex:1;min-width:150px;max-width:440px;display:flex}
.search input{width:100%;padding-left:36px}
.search .ico{position:absolute;left:12px;top:50%;transform:translateY(-50%);color:var(--v6-faint);pointer-events:none;font-size:14px}
.palette-bar{display:flex;gap:6px}
.palette-bar .p{
  width:22px;height:22px;border-radius:50%;border:2px solid var(--v6-ink);cursor:pointer;
  padding:0;min-height:0;box-shadow:none;background:var(--v6-paper);
}
.palette-bar .p:hover{background:var(--v6-paper);transform:none;box-shadow:none}
.palette-bar .p[aria-pressed="true"]{box-shadow:0 0 0 2px var(--v6-cream),0 0 0 4px var(--v6-ink)}

/* ---------- layout ---------- */
.main{flex:1;display:flex;min-height:0;position:relative;background:var(--v6-cream)}
nav.sidebar{
  width:290px;flex-shrink:0;background:var(--v6-paper);border-right:var(--outline);
  display:flex;flex-direction:column;min-height:0;
}
.sidebar .tools{display:flex;gap:8px;padding:10px;border-bottom:2px solid var(--v6-rule)}
.sidebar .tools button{flex:1;padding:8px;min-height:38px;font-size:10px}
.count{
  font:500 9px/1.4 var(--font-mono);color:var(--v6-muted);text-transform:uppercase;
  letter-spacing:.14em;padding:12px 12px 6px;
}
.list{flex:1;overflow-y:auto;padding:0 8px 12px}
.item{
  padding:10px 12px;border-radius:var(--r-sm);cursor:pointer;border:2px solid transparent;
  margin-bottom:2px;transition:background .12s ease,box-shadow .12s ease;
}
.item:hover{background:var(--v6-yellow-tint);box-shadow:inset 4px 0 0 var(--v6-ink)}
.item.active{background:var(--acc-tint);border-color:var(--v6-ink)}
.item .t{font:700 14px/1.35 var(--font-display);letter-spacing:-.01em;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.item .m{font:500 9.5px/1.5 var(--font-mono);color:var(--v6-faint);display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin-top:3px;letter-spacing:.06em;text-transform:uppercase}
.tag{
  display:inline-block;font:500 9.5px/1.5 var(--font-mono);background:var(--acc-tint);color:var(--acc);
  border:1.5px solid var(--v6-ink);border-radius:999px;padding:1px 8px;letter-spacing:.06em;text-transform:uppercase;
}

section.content{flex:1;overflow-y:auto;min-width:0;min-height:0;padding:26px 28px 48px}
.empty{color:var(--v6-muted);text-align:center;padding:60px 20px;font-size:14px}
.page-title{font:700 26px/1.15 var(--font-display);letter-spacing:-.03em;margin-bottom:6px}
.meta{
  display:flex;flex-wrap:wrap;gap:8px;align-items:center;color:var(--v6-muted);
  font:500 10px/1.5 var(--font-mono);letter-spacing:.08em;text-transform:uppercase;margin-bottom:16px;
}
.actions{display:flex;gap:8px;margin-bottom:18px;flex-wrap:wrap}
.prose{
  background:var(--v6-paper);border:var(--outline);border-radius:var(--r);
  box-shadow:var(--shadow);padding:22px 26px;max-width:900px;
}
.prose h1{font:700 24px/1.2 var(--font-display);margin:4px 0 10px;letter-spacing:-.03em}
.prose h2{font:700 18px/1.3 var(--font-display);color:var(--acc);margin:22px 0 8px;letter-spacing:-.02em}
.prose h3{font:700 15px/1.35 var(--font-display);margin:16px 0 6px}
.prose p{margin:8px 0}
.prose ul,.prose ol{margin:8px 0 8px 22px}
.prose li{margin:3px 0}
.prose code{font:12.5px/1.5 var(--font-mono);background:var(--v6-cream-2);border:1.5px solid var(--v6-ink);border-radius:7px;padding:1px 6px}
.prose pre{background:var(--v6-cream-2);border:1.5px solid var(--v6-ink);border-radius:var(--r-sm);padding:12px;overflow-x:auto;margin:10px 0}
.prose pre code{background:none;border:none;padding:0}
.prose a{text-decoration:underline}
textarea.editor{width:100%;min-height:60vh;font:13px/1.7 var(--font-mono);resize:vertical}
.hit{
  background:var(--v6-paper);border:var(--outline);border-radius:var(--r-sm);padding:13px 15px;
  margin-bottom:10px;cursor:pointer;box-shadow:var(--shadow-sm);
  transition:transform .12s ease,box-shadow .12s ease,background .12s ease;
}
.hit:hover{background:var(--v6-yellow-tint);transform:translate(-1px,-1px);box-shadow:4px 4px 0 var(--v6-ink)}
.hit .t{font:700 15px/1.3 var(--font-display);letter-spacing:-.02em;margin-bottom:3px}
.hit .x{font:12.5px/1.6 var(--font-mono);color:var(--v6-muted);white-space:pre-wrap;word-break:break-word;max-height:5.2em;overflow:hidden}
.search-head{font:700 20px/1.3 var(--font-display);letter-spacing:-.03em;margin-bottom:14px}
.search-head .meta{display:inline;margin:0}

/* ---------- scrim, toast, dialog ---------- */
.scrim{position:fixed;inset:0;background:rgb(17 17 17 / .5);z-index:29;display:none}
.toast{
  position:fixed;left:50%;bottom:20px;transform:translateX(-50%) translateY(80px);
  background:var(--v6-ink);color:var(--v6-cream);padding:11px 18px;border:var(--outline);
  border-radius:13px;font:500 11.5px/1.4 var(--font-mono);letter-spacing:.04em;
  box-shadow:4px 4px 0 var(--acc-hue);z-index:60;opacity:0;transition:transform .2s,opacity .2s;
}
.toast.show{transform:translateX(-50%) translateY(0);opacity:1}
@media (prefers-color-scheme:dark){
  :root[data-mode="auto"] .toast{ color:#111111; }
}
dialog{
  border:var(--outline);border-radius:var(--r);box-shadow:var(--shadow);background:var(--v6-paper);
  color:var(--v6-ink);padding:22px;width:min(92vw,440px);
}
dialog::backdrop{background:rgb(17 17 17 / .5)}
dialog h2{font:700 20px/1.3 var(--font-display);letter-spacing:-.03em;margin-bottom:14px}
dialog input{width:100%;margin-bottom:16px}
dialog .row{display:flex;gap:8px;justify-content:flex-end}

/* ---------- mobile ---------- */
.hamburger{display:none}
@media (max-width:760px){
  .desk{padding:0}
  .shell{border-radius:0;border-inline:0;box-shadow:none}
  nav.sidebar{
    position:fixed;top:0;left:0;bottom:0;width:min(85vw,320px);z-index:30;
    transform:translateX(-102%);transition:transform .22s ease;box-shadow:var(--shadow);
  }
  nav.sidebar.open{transform:none}
  .scrim.show{display:block}
  .hamburger{display:inline-flex}
  section.content{padding:18px 16px 40px}
  .prose{padding:16px 18px;border-radius:var(--r-sm)}
  .search{order:3;flex-basis:100%;max-width:none}
  .brand .sub{display:none}
}
@media (max-width:420px){ .brand span.name{display:none} }
@media (prefers-reduced-motion:reduce){ *,*::before,*::after{ animation-duration:.01ms !important; transition-duration:.01ms !important; } }
</style>
</head>
<body>
<div class="desk">
<div class="shell">
<header>
  <button class="ghost icon hamburger" id="menu-btn" aria-label="เมนู" aria-expanded="false">☰</button>
  <span class="dots" aria-hidden="true"><i></i><i></i><i></i></span>
  <div class="brand"><span class="dot"></span><span class="name">mem</span><span class="sub">wiki</span></div>
  <div class="grow"></div>
  <div class="search">
    <span class="ico">⌕</span>
    <input id="q" type="search" placeholder="ค้นหา..." autocomplete="off" aria-label="ค้นหา">
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
    <div class="list" id="list" tabindex="0" aria-label="รายการหน้า"></div>
  </nav>
  <div class="scrim" id="scrim"></div>
  <section class="content" id="content" tabindex="0" aria-label="เนื้อหาหน้า"><div class="empty">เลือกหน้าจากเมนู หรือกด + สร้าง เพื่อเริ่มเขียน</div></section>
</div>
</div>
</div>

<dialog id="new-dlg">
  <h2>หน้าใหม่</h2>
  <form id="new-form">
    <input id="new-title" placeholder="ชื่อหน้า..." aria-label="ชื่อหน้า" required>
    <div class="row"><button type="button" id="new-cancel">ยกเลิก</button><button class="sticker" type="submit">สร้าง</button></div>
  </form>
</dialog>

<div class="toast" id="toast" role="status" aria-live="polite"></div>

<script nonce="__CSP_NONCE__">
"use strict";
const $ = id => document.getElementById(id);
const PALETTES = ["blue", "purple", "orange", "yellow"];
const PCOLOR = { blue:"#4d83ff", purple:"#8b73e8", orange:"#f47b20", yellow:"#ffd21f" };
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
      "<h1 class=\"page-title\">" + esc(p.title) + "</h1>" +
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
    if(!data.results.length){ $("content").innerHTML = "<div class=\"empty\">ไม่พบผลลัพธ์สําหรับ “" + esc(q) + "”</div>"; return; }
    let html = "<h2 class=\"search-head\">ค้นหา: “" + esc(q) + "” <span class=\"meta\">(" + data.results.length + ")</span></h2>";
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

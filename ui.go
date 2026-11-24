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
<meta name="theme-color" content="#9dd2f7">
<title>mem — wiki</title>
<script nonce="__CSP_NONCE__">
/* Paint the persisted mode before first paint; the main script keeps it in sync. */
(function(){
  try{
    var m = localStorage.getItem("mem-mode");
    if(m !== "light" && m !== "dark" && m !== "auto") m = "auto";
    var root = document.documentElement;
    root.dataset.mode = m;
    var dark = m === "dark" || (m === "auto" && window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches);
    root.dataset.v6Mode = dark ? "dark" : "light";
    root.style.colorScheme = dark ? "dark" : "light";
    document.querySelector('meta[name="theme-color"]').setAttribute("content", dark ? "#1b2440" : "#9dd2f7");
  }catch(e){}
})();
</script>
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

/* ---------- MCKY UI V6 canonical tokens + local aliases ----------
 * Canonical --mcky-v6-* values are copied from v6-base.css / v6-theme.css
 * (no build step, no @import). The --v6-* aliases keep this single-file UI's
 * existing rule names working without a neutral-charcoal dark theme. */
:root{
  --mcky-v6-sky:#9DD2F7; --mcky-v6-cream:#F8F6F0; --mcky-v6-paper:#FFFFFF;
  --mcky-v6-ink:#111111; --mcky-v6-muted:#4A4A4A; --mcky-v6-subtle:#7A7A7A;
  --mcky-v6-yellow:#FFD21F; --mcky-v6-blue:#4D83FF; --mcky-v6-purple:#8B73E8;
  --mcky-v6-orange:#F47B20; --mcky-v6-red:#E34E4E; --mcky-v6-success:#2E9E73;
  --mcky-v6-on-accent:#111111;
  --mcky-v6-outline:2.5px solid var(--mcky-v6-ink);
  --mcky-v6-shadow:5px 5px 0 var(--mcky-v6-ink);
  --mcky-v6-shadow-sm:3px 3px 0 var(--mcky-v6-ink);
  --mcky-v6-shadow-shell:8px 8px 0 rgb(17 17 17 / .9);
  --mcky-v6-overlay:rgba(157,210,247,.85);
  --mcky-v6-radius:20px; --mcky-v6-radius-sm:12px;
  --mcky-v6-scenery-sun:#FFD21F; --mcky-v6-scenery-hill:#8CCB7A;
  --mcky-v6-scenery-hill-alt:#69B45F; --mcky-v6-scenery-meadow:#3F8447;
  --mcky-v6-scenery-star:#FFF6D8; --mcky-v6-scenery-moon:#F6EFD2;
  --mcky-v6-scenery-city:#16203C; --mcky-v6-scenery-city-edge:#42557F;
  --mcky-v6-scenery-window:#FFD84D;
  /* local-only tints and aliases */
  --v6-sky:var(--mcky-v6-sky); --v6-cream:var(--mcky-v6-cream);
  --v6-cream-2:#efece2; --v6-paper:var(--mcky-v6-paper);
  --v6-ink:var(--mcky-v6-ink); --v6-muted:var(--mcky-v6-muted);
  --v6-faint:#6b6b6b; --v6-rule:rgba(17 17 17 / .12);
  --v6-yellow:var(--mcky-v6-yellow); --v6-yellow-tint:#fff4c2;
  --v6-blue:var(--mcky-v6-blue); --v6-blue-tint:#e4edff;
  --v6-purple:var(--mcky-v6-purple); --v6-purple-tint:#ede7ff;
  --v6-orange:var(--mcky-v6-orange); --v6-orange-tint:#ffe4ce;
  --v6-red:var(--mcky-v6-red); --v6-red-tint:#fce6e6;
  --outline:var(--mcky-v6-outline); --shadow:var(--mcky-v6-shadow); --shadow-sm:var(--mcky-v6-shadow-sm);
  --r:var(--mcky-v6-radius); --r-sm:var(--mcky-v6-radius-sm);
  --font-display:"Space Grotesk","Sarabun",system-ui,sans-serif;
  --font-body:"Sarabun","Noto Sans Thai",system-ui,sans-serif;
  --font-mono:"DM Mono",ui-monospace,Menlo,monospace;
  --acc:#1f4fd8; --acc-hue:var(--v6-blue); --acc-tint:var(--v6-blue-tint); --acc-ink:#ffffff;
  --danger:#b42318; --danger-tint:var(--v6-red-tint);
}
:root[data-palette="purple"]{ --acc:#5a45c9; --acc-hue:var(--v6-purple); --acc-tint:var(--v6-purple-tint); }
:root[data-palette="orange"]{ --acc:#b3540a; --acc-hue:var(--v6-orange); --acc-tint:var(--v6-orange-tint); }
:root[data-palette="yellow"]{ --acc:#8a6100; --acc-hue:var(--v6-yellow); --acc-tint:var(--v6-yellow-tint); }

/* Canonical twilight night ramp — explicit dark ... */
:root[data-mode="dark"]{
  color-scheme:dark;
  --mcky-v6-sky:#1b2440; --mcky-v6-cream:#263354; --mcky-v6-paper:#314066;
  --mcky-v6-ink:#f4f1e8; --mcky-v6-muted:#c9d4ec; --mcky-v6-subtle:#a6b5d6;
  --mcky-v6-yellow:#ffd84d; --mcky-v6-blue:#9ec1ff; --mcky-v6-purple:#c9b8ff;
  --mcky-v6-orange:#ffb77e; --mcky-v6-red:#ff9a92; --mcky-v6-success:#7fe3b8;
  --mcky-v6-on-accent:#1b2440;
  --mcky-v6-outline:2.5px solid #b9c8ea;
  --mcky-v6-shadow:5px 5px 0 #101a30; --mcky-v6-shadow-sm:3px 3px 0 #101a30;
  --mcky-v6-shadow-shell:8px 8px 0 #101a30;
  --mcky-v6-overlay:rgba(27,36,64,.88);
  --mcky-v6-scenery-sun:#ffd86b; --mcky-v6-scenery-hill:#263354;
  --mcky-v6-scenery-hill-alt:#1b2440; --mcky-v6-scenery-meadow:#16203c;
  --mcky-v6-scenery-star:#fff6d8; --mcky-v6-scenery-moon:#f6efd2;
  --mcky-v6-scenery-city:#16203c; --mcky-v6-scenery-city-edge:#42557f;
  --mcky-v6-scenery-window:#ffd84d;
  --v6-cream-2:#2b395e; --v6-faint:#a6b5d6; --v6-rule:rgba(185,200,234,.28);
  --v6-yellow-tint:#3a3524; --v6-blue-tint:#26324a; --v6-purple-tint:#322b4a;
  --v6-orange-tint:#3d2f22; --v6-red-tint:#3d2626;
  --acc:#9ec1ff; --acc-ink:#1b2440; --danger:#ff9a92;
}
:root[data-mode="dark"][data-palette="purple"]{ --acc:#c9b8ff; }
:root[data-mode="dark"][data-palette="orange"]{ --acc:#ffb77e; }
:root[data-mode="dark"][data-palette="yellow"]{ --acc:#ffd84d; }
/* ... and the same ramp when auto resolves to a dark OS preference. */
@media (prefers-color-scheme:dark){
  :root[data-mode="auto"]{
    color-scheme:dark;
    --mcky-v6-sky:#1b2440; --mcky-v6-cream:#263354; --mcky-v6-paper:#314066;
    --mcky-v6-ink:#f4f1e8; --mcky-v6-muted:#c9d4ec; --mcky-v6-subtle:#a6b5d6;
    --mcky-v6-yellow:#ffd84d; --mcky-v6-blue:#9ec1ff; --mcky-v6-purple:#c9b8ff;
    --mcky-v6-orange:#ffb77e; --mcky-v6-red:#ff9a92; --mcky-v6-success:#7fe3b8;
    --mcky-v6-on-accent:#1b2440;
    --mcky-v6-outline:2.5px solid #b9c8ea;
    --mcky-v6-shadow:5px 5px 0 #101a30; --mcky-v6-shadow-sm:3px 3px 0 #101a30;
    --mcky-v6-shadow-shell:8px 8px 0 #101a30;
    --mcky-v6-overlay:rgba(27,36,64,.88);
    --mcky-v6-scenery-sun:#ffd86b; --mcky-v6-scenery-hill:#263354;
    --mcky-v6-scenery-hill-alt:#1b2440; --mcky-v6-scenery-meadow:#16203c;
    --mcky-v6-scenery-star:#fff6d8; --mcky-v6-scenery-moon:#f6efd2;
    --mcky-v6-scenery-city:#16203c; --mcky-v6-scenery-city-edge:#42557f;
    --mcky-v6-scenery-window:#ffd84d;
    --v6-cream-2:#2b395e; --v6-faint:#a6b5d6; --v6-rule:rgba(185,200,234,.28);
    --v6-yellow-tint:#3a3524; --v6-blue-tint:#26324a; --v6-purple-tint:#322b4a;
    --v6-orange-tint:#3d2f22; --v6-red-tint:#3d2626;
    --acc:#9ec1ff; --acc-ink:#1b2440; --danger:#ff9a92;
  }
  :root[data-mode="auto"][data-palette="purple"]{ --acc:#c9b8ff; }
  :root[data-mode="auto"][data-palette="orange"]{ --acc:#ffb77e; }
  :root[data-mode="auto"][data-palette="yellow"]{ --acc:#ffd84d; }
}
*{box-sizing:border-box;margin:0;padding:0}
html{height:100%}
body{
  height:100dvh; overflow:hidden; background:var(--mcky-v6-sky); color:var(--v6-ink);
  font:15px/1.6 var(--font-body);
}
::selection{background:var(--mcky-v6-yellow);color:var(--mcky-v6-on-accent)}
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
  background:var(--v6-yellow); color:var(--mcky-v6-on-accent); border:var(--outline); border-radius:13px;
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
  box-shadow:var(--mcky-v6-shadow-shell); overflow:clip;
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
.palette-bar .p[data-palette-value="blue"]{background:var(--v6-blue)}
.palette-bar .p[data-palette-value="purple"]{background:var(--v6-purple)}
.palette-bar .p[data-palette-value="orange"]{background:var(--v6-orange)}
.palette-bar .p[data-palette-value="yellow"]{background:var(--v6-yellow)}
.palette-bar .p:hover{transform:none;box-shadow:none}
.palette-bar .p[aria-pressed="true"]{box-shadow:0 0 0 2px var(--v6-cream),0 0 0 4px var(--v6-ink)}
.mcky-v6-mode-toggle{
  display:inline-grid;place-items:center;position:relative;flex:none;
  width:36px;height:36px;min-height:36px;padding:0;
  color:var(--v6-ink);background:var(--v6-paper);border:var(--outline);
  border-radius:var(--r-sm);box-shadow:var(--shadow-sm);cursor:pointer;
}
.mcky-v6-mode-toggle:hover{background:var(--v6-yellow-tint);transform:translate(-1px,-1px);box-shadow:2px 2px 0 var(--v6-ink)}
.mcky-v6-mode-toggle:active{transform:translate(1px,1px);box-shadow:none}
.mcky-v6-mode-toggle svg{display:block;width:18px;height:18px}
.mcky-v6-mode-toggle[data-pref="auto"]::after{
  content:"";position:absolute;right:3px;bottom:3px;width:6px;height:6px;
  border-radius:50%;background:var(--mcky-v6-yellow);border:1px solid var(--v6-ink);
}

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
.scrim{position:fixed;inset:0;background:var(--mcky-v6-overlay);z-index:29;display:none}
.toast{
  position:fixed;left:50%;bottom:20px;transform:translateX(-50%) translateY(80px);
  background:var(--v6-ink);color:var(--v6-cream);padding:11px 18px;border:var(--outline);
  border-radius:13px;font:500 11.5px/1.4 var(--font-mono);letter-spacing:.04em;
  box-shadow:4px 4px 0 var(--acc-hue);z-index:60;opacity:0;transition:transform .2s,opacity .2s;
}
.toast.show{transform:translateX(-50%) translateY(0);opacity:1}
dialog{
  border:var(--outline);border-radius:var(--r);box-shadow:var(--shadow);background:var(--v6-paper);
  color:var(--v6-ink);padding:22px;width:min(92vw,440px);
}
dialog::backdrop{background:var(--mcky-v6-overlay)}
dialog h2{font:700 20px/1.3 var(--font-display);letter-spacing:-.03em;margin-bottom:14px}
dialog input{width:100%;margin-bottom:16px}
dialog .row{display:flex;gap:8px;justify-content:flex-end}

/* ---------- Painted scenery ----------
 * Static port of MCKY UI V6 Scenery + ModeToggle from mcky-ui main
 * d208f003f106ed33c714c9acd0ca6fdac76c6b99. Same positions, sizes, delays and
 * colours as the shared component; Svelte scoping is unavailable here, so every
 * selector is nested under .mcky-v6-scenery. */
.mcky-v6-scenery{
  position:fixed;inset:0;z-index:0;overflow:hidden;pointer-events:none;
  background:var(--mcky-v6-sky);
}
.mcky-v6-scenery-content{position:relative;z-index:1}
.mcky-v6-scenery .scene{position:absolute;inset:0}
.mcky-v6-scenery .scene-night{display:none}
:root[data-mode="dark"] .mcky-v6-scenery .scene-day{display:none}
:root[data-mode="dark"] .mcky-v6-scenery .scene-night{display:block}
@media (prefers-color-scheme:dark){
  :root[data-mode="auto"] .mcky-v6-scenery .scene-day{display:none}
  :root[data-mode="auto"] .mcky-v6-scenery .scene-night{display:block}
}
.mcky-v6-scenery .sky{position:absolute;inset:0}
.mcky-v6-scenery .sky-day{
  background:
    radial-gradient(120% 70% at 82% 16%, rgba(255,246,206,.5), transparent 58%),
    linear-gradient(180deg,#86c2ec 0%,var(--mcky-v6-sky) 48%,#d9ecf8 100%);
}
.mcky-v6-scenery .sky-night{
  background:
    radial-gradient(90% 50% at 50% 108%, rgba(255,190,130,.09), transparent 70%),
    linear-gradient(180deg,#101a33 0%,var(--mcky-v6-sky) 56%,#2a3558 100%);
}

/* Day — soft light, layered distance, no ink outlines. */
.mcky-v6-scenery .sun{
  position:absolute;top:8%;right:13%;width:64px;height:64px;border-radius:50%;
  background:radial-gradient(circle at 38% 34%,#fff3bd 0%,var(--mcky-v6-yellow) 68%,#f5c412 100%);
  box-shadow:0 0 0 16px rgba(255,224,130,.12),0 0 60px 24px rgba(255,220,120,.16);
}
.mcky-v6-scenery .cloud{
  position:absolute;height:58px;opacity:.9;transform-origin:center;
  filter:drop-shadow(0 8px 9px rgba(54,91,120,.09));
  animation:mcky-scenery-cloud-drift 30s ease-in-out infinite alternate;
}
.mcky-v6-scenery .puff,
.mcky-v6-scenery .cloud-base,
.mcky-v6-scenery .cloud-shade{position:absolute;display:block}
.mcky-v6-scenery .puff{
  border-radius:50%;
  background:linear-gradient(180deg,#ffffff 0%,#e6f0f8 100%);
}
.mcky-v6-scenery .puff-a{width:48%;height:76%;left:5%;bottom:24%}
.mcky-v6-scenery .puff-b{width:58%;height:92%;left:28%;bottom:18%}
.mcky-v6-scenery .puff-c{width:34%;height:60%;left:62%;bottom:25%}
.mcky-v6-scenery .cloud-base{
  left:2%;right:4%;bottom:6%;height:42%;border-radius:999px;
  background:linear-gradient(180deg,#f7fbff 0%,#dceaf5 100%);
}
.mcky-v6-scenery .cloud-shade{
  left:12%;right:12%;bottom:4%;height:22%;border-radius:999px;
  background:rgba(152,183,205,.24);filter:blur(4px);
}
.mcky-v6-scenery .ridge{
  position:absolute;left:-12%;width:124%;
  border-radius:50% 50% 0 0 / 38% 42% 0 0;
}
.mcky-v6-scenery .ridge-far{bottom:-118px;height:220px;background:linear-gradient(180deg,#bcdcb8,#a4cba2);opacity:.72}
.mcky-v6-scenery .ridge-mid{bottom:-132px;height:218px;background:linear-gradient(180deg,#8dc083,#79af76)}
.mcky-v6-scenery .ridge-near{bottom:-160px;height:230px;background:linear-gradient(180deg,#63a164,#4f8a57)}
.mcky-v6-scenery .meadow{
  position:absolute;inset:auto 0 0;height:78px;
  background:linear-gradient(180deg,rgba(86,148,91,.35),#447a4c 78%,#3c7046);
}
.mcky-v6-scenery .meadow::before{
  content:"";position:absolute;inset:0;
  background:repeating-linear-gradient(97deg,transparent 0 13px,rgba(29,79,43,.16) 13px 14px,transparent 14px 27px);
  -webkit-mask-image:linear-gradient(180deg,transparent,#000 62%);
  mask-image:linear-gradient(180deg,transparent,#000 62%);
}
.mcky-v6-scenery .tree{position:absolute;bottom:70px;width:54px;height:92px;opacity:.92}
.mcky-v6-scenery .tree-left{left:13%}
.mcky-v6-scenery .tree-right{right:15%;transform:scale(.82)}
.mcky-v6-scenery .trunk{
  position:absolute;bottom:0;left:24px;width:7px;height:40px;border-radius:4px 4px 1px 1px;
  background:linear-gradient(90deg,#6f5138,#8a6848 55%,#5f452f);
}
.mcky-v6-scenery .leaf{
  position:absolute;border-radius:50%;
  background:radial-gradient(circle at 36% 28%,#79b374,#4e8c58 78%);
}
.mcky-v6-scenery .leaf-a{width:34px;height:34px;left:4px;top:16px}
.mcky-v6-scenery .leaf-b{width:40px;height:40px;left:12px;top:4px}
.mcky-v6-scenery .leaf-c{width:30px;height:30px;left:22px;top:20px}

/* Night — low-contrast depth, warm windows, a soft crescent. */
.mcky-v6-scenery .horizon-glow{
  position:absolute;inset:auto 0 0;height:34%;
  background:linear-gradient(180deg,transparent,rgba(96,118,168,.16) 72%,rgba(34,47,82,.4));
}
.mcky-v6-scenery .star{
  position:absolute;border-radius:50%;background:var(--mcky-v6-scenery-star);
  animation:mcky-scenery-star-breathe 4.2s ease-in-out infinite;
}
.mcky-v6-scenery .star-warm{background:#fff1cf}
.mcky-v6-scenery .moon{
  position:absolute;top:11%;right:14%;width:56px;height:56px;border-radius:50%;
  background:radial-gradient(circle at 36% 30%,#fffaf0,#e8e0c8 78%);
  box-shadow:0 0 34px 10px rgba(246,239,210,.12);
}
.mcky-v6-scenery .moon::after{
  content:"";position:absolute;top:-8px;left:13px;width:52px;height:52px;border-radius:50%;
  background:radial-gradient(circle at 32% 22%,#142039,#1b2440 82%);filter:blur(.4px);
}
.mcky-v6-scenery .city-back,
.mcky-v6-scenery .city{position:absolute;inset:auto 0 0;height:220px}
.mcky-v6-scenery .city-back span{
  position:absolute;bottom:0;border-radius:2px 2px 0 0;
  background:linear-gradient(180deg,#2b385d,#24304f);opacity:.62;
}
.mcky-v6-scenery .city{height:210px}
.mcky-v6-scenery .building{
  position:absolute;bottom:0;border-radius:2px 2px 0 0;
  background:linear-gradient(180deg,#1b2542 0%,#121a31 100%);
  box-shadow:inset 0 1px 0 rgba(255,255,255,.05);
}
.mcky-v6-scenery .building::before{
  content:"";position:absolute;top:-5px;left:16%;width:68%;height:5px;
  border-radius:2px 2px 0 0;background:#1b2542;
}
.mcky-v6-scenery .windows{
  display:grid;grid-template-columns:repeat(3,1fr);gap:8px 9px;padding:16px 10px;
}
.mcky-v6-scenery .windows span{
  width:7px;height:9px;border-radius:1px;background:rgba(187,205,240,.05);
}
.mcky-v6-scenery .windows span.lit{
  background:linear-gradient(180deg,#ffd98a,#e2a955);opacity:.88;
}
.mcky-v6-scenery .windows span.lit.dim{
  opacity:.5;background:linear-gradient(180deg,#cbd9f5,#9db3dc);
}
.mcky-v6-scenery .night-ground{
  position:absolute;inset:auto 0 0;height:26px;
  background:linear-gradient(180deg,transparent,rgba(8,13,27,.7));
}

@keyframes mcky-scenery-cloud-drift{
  from{transform:scale(var(--cloud-scale)) translate(-6px,2px)}
  to{transform:scale(var(--cloud-scale)) translate(18px,-7px)}
}
@keyframes mcky-scenery-star-breathe{
  0%,100%{filter:brightness(.78)}
  50%{filter:brightness(1.25)}
}
@media (prefers-reduced-motion:reduce){
  .mcky-v6-scenery .cloud,
  .mcky-v6-scenery .star{animation:none}
}
@media (max-width:620px){
  .mcky-v6-scenery .sun{top:7%;right:9%;width:52px;height:52px}
  .mcky-v6-scenery .cloud{opacity:.72}
  .mcky-v6-scenery .tree-left{left:5%}
  .mcky-v6-scenery .tree-right{right:5%}
  .mcky-v6-scenery .building{opacity:.94}
}

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
<div class="mcky-v6-scenery" aria-hidden="true">
  <div class="scene scene-day">
    <div class="sky sky-day"></div>
    <div class="sun"></div>
    <div class="clouds">
      <div class="cloud" style="left:5%;top:9%;width:196px;--cloud-scale:1;animation-delay:-3s;animation-duration:28s">
        <span class="puff puff-a"></span><span class="puff puff-b"></span><span class="puff puff-c"></span><span class="cloud-base"></span><span class="cloud-shade"></span>
      </div>
      <div class="cloud" style="left:66%;top:15%;width:152px;--cloud-scale:0.82;animation-delay:-11s;animation-duration:33s">
        <span class="puff puff-a"></span><span class="puff puff-b"></span><span class="puff puff-c"></span><span class="cloud-base"></span><span class="cloud-shade"></span>
      </div>
      <div class="cloud" style="left:35%;top:25%;width:112px;--cloud-scale:0.62;animation-delay:-19s;animation-duration:39s">
        <span class="puff puff-a"></span><span class="puff puff-b"></span><span class="puff puff-c"></span><span class="cloud-base"></span><span class="cloud-shade"></span>
      </div>
    </div>
    <div class="ridge ridge-far"></div>
    <div class="ridge ridge-mid"></div>
    <div class="tree tree-left"><span class="trunk"></span><span class="leaf leaf-a"></span><span class="leaf leaf-b"></span><span class="leaf leaf-c"></span></div>
    <div class="tree tree-right"><span class="trunk"></span><span class="leaf leaf-a"></span><span class="leaf leaf-b"></span><span class="leaf leaf-c"></span></div>
    <div class="ridge ridge-near"></div>
    <div class="meadow"></div>
  </div>
  <div class="scene scene-night">
    <div class="sky sky-night"></div>
    <div class="horizon-glow"></div>
    <div class="stars">
      <span class="star star-warm" style="left:7%;top:9%;width:2.2px;height:2.2px;opacity:0.8;animation-delay:0s"></span>
      <span class="star" style="left:18%;top:18%;width:1.4px;height:1.4px;opacity:0.55;animation-delay:0.8s"></span>
      <span class="star star-warm" style="left:29%;top:7%;width:2.6px;height:2.6px;opacity:0.75;animation-delay:1.6s"></span>
      <span class="star" style="left:41%;top:15%;width:1.6px;height:1.6px;opacity:0.6;animation-delay:0.4s"></span>
      <span class="star star-warm" style="left:53%;top:6%;width:2.1px;height:2.1px;opacity:0.85;animation-delay:1.2s"></span>
      <span class="star" style="left:64%;top:21%;width:1.3px;height:1.3px;opacity:0.5;animation-delay:2s"></span>
      <span class="star star-warm" style="left:76%;top:10%;width:2.4px;height:2.4px;opacity:0.7;animation-delay:0.6s"></span>
      <span class="star" style="left:87%;top:19%;width:1.5px;height:1.5px;opacity:0.55;animation-delay:1.9s"></span>
      <span class="star star-warm" style="left:94%;top:8%;width:2px;height:2px;opacity:0.75;animation-delay:1.1s"></span>
      <span class="star" style="left:12%;top:32%;width:1.4px;height:1.4px;opacity:0.5;animation-delay:2.4s"></span>
      <span class="star star-warm" style="left:59%;top:34%;width:1.7px;height:1.7px;opacity:0.6;animation-delay:0.9s"></span>
      <span class="star" style="left:82%;top:39%;width:1.2px;height:1.2px;opacity:0.45;animation-delay:1.7s"></span>
    </div>
    <div class="moon"></div>
    <div class="city-back">
      <span style="left:3%;width:84px;height:106px"></span>
      <span style="left:19%;width:66px;height:78px"></span>
      <span style="left:34%;width:92px;height:124px"></span>
      <span style="left:51%;width:72px;height:92px"></span>
      <span style="left:67%;width:98px;height:132px"></span>
      <span style="left:86%;width:78px;height:100px"></span>
    </div>
    <div class="city">
      <div class="building" style="left:0%;width:78px;height:132px">
        <div class="windows">
          <span class="lit"></span>
          <span></span>
          <span></span>
          <span class="dim"></span>
          <span class="lit"></span>
          <span></span>
          <span></span>
          <span></span>
          <span class="dim"></span>
          <span class="lit"></span>
          <span></span>
          <span></span>
        </div>
      </div>
      <div class="building" style="left:8%;width:58px;height:96px">
        <div class="windows">
          <span></span>
          <span></span>
          <span class="lit"></span>
          <span class="dim"></span>
          <span></span>
          <span></span>
          <span></span>
          <span class="lit"></span>
          <span class="dim"></span>
          <span></span>
          <span></span>
          <span></span>
        </div>
      </div>
      <div class="building" style="left:15%;width:92px;height:158px">
        <div class="windows">
          <span></span>
          <span class="lit"></span>
          <span></span>
          <span class="dim"></span>
          <span></span>
          <span class="lit"></span>
          <span class="lit"></span>
          <span></span>
          <span class="dim"></span>
          <span></span>
          <span></span>
          <span class="lit"></span>
        </div>
      </div>
      <div class="building" style="left:27%;width:66px;height:112px">
        <div class="windows">
          <span></span>
          <span></span>
          <span></span>
          <span class="lit dim"></span>
          <span></span>
          <span></span>
          <span></span>
          <span></span>
          <span class="lit dim"></span>
          <span></span>
          <span></span>
          <span></span>
        </div>
      </div>
      <div class="building" style="left:36%;width:84px;height:144px">
        <div class="windows">
          <span class="lit"></span>
          <span></span>
          <span class="lit"></span>
          <span class="dim"></span>
          <span></span>
          <span></span>
          <span></span>
          <span class="lit"></span>
          <span class="dim"></span>
          <span></span>
          <span class="lit"></span>
          <span></span>
        </div>
      </div>
      <div class="building" style="left:48%;width:62px;height:92px">
        <div class="windows">
          <span></span>
          <span class="lit"></span>
          <span></span>
          <span class="dim"></span>
          <span></span>
          <span></span>
          <span class="lit"></span>
          <span></span>
          <span class="dim"></span>
          <span></span>
          <span></span>
          <span></span>
        </div>
      </div>
      <div class="building" style="left:57%;width:96px;height:168px">
        <div class="windows">
          <span></span>
          <span></span>
          <span></span>
          <span class="lit dim"></span>
          <span class="lit"></span>
          <span></span>
          <span></span>
          <span></span>
          <span class="dim"></span>
          <span class="lit"></span>
          <span></span>
          <span class="lit"></span>
        </div>
      </div>
      <div class="building" style="left:70%;width:70px;height:118px">
        <div class="windows">
          <span class="lit"></span>
          <span></span>
          <span></span>
          <span class="dim"></span>
          <span></span>
          <span class="lit"></span>
          <span></span>
          <span></span>
          <span class="lit dim"></span>
          <span></span>
          <span></span>
          <span></span>
        </div>
      </div>
      <div class="building" style="left:79%;width:88px;height:150px">
        <div class="windows">
          <span></span>
          <span></span>
          <span class="lit"></span>
          <span class="dim"></span>
          <span></span>
          <span></span>
          <span class="lit"></span>
          <span></span>
          <span class="dim"></span>
          <span></span>
          <span class="lit"></span>
          <span></span>
        </div>
      </div>
      <div class="building" style="left:91%;width:62px;height:104px">
        <div class="windows">
          <span></span>
          <span class="lit"></span>
          <span></span>
          <span class="dim"></span>
          <span></span>
          <span></span>
          <span></span>
          <span class="lit"></span>
          <span class="dim"></span>
          <span></span>
          <span></span>
          <span></span>
        </div>
      </div>
    </div>
    <div class="night-ground"></div>
  </div>
</div>
<div class="mcky-v6-scenery-content">
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
  <button class="mcky-v6-mode-toggle" id="mode-btn" type="button" data-pref="auto" aria-pressed="false" aria-label="โหมดอัตโนมัติ คลิกเพื่อโหมดกลางวัน" title="โหมดอัตโนมัติ">
    <svg aria-hidden="true" width="18" height="18" viewBox="0 0 256 256" fill="currentColor"><path d="M236.37,139.4a12,12,0,0,0-12-3A84.07,84.07,0,0,1,119.6,31.59a12,12,0,0,0-15-15A108.86,108.86,0,0,0,49.69,55.07,108,108,0,0,0,136,228a107.09,107.09,0,0,0,64.93-21.69,108.86,108.86,0,0,0,38.44-54.94A12,12,0,0,0,236.37,139.4Zm-49.88,47.74A84,84,0,0,1,68.86,69.51,84.93,84.93,0,0,1,92.27,48.29Q92,52.13,92,56A108.12,108.12,0,0,0,200,164q3.87,0,7.71-.27A84.79,84.79,0,0,1,186.49,187.14Z"></path></svg>
  </button>
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
</div><!-- /.mcky-v6-scenery-content -->

<script nonce="__CSP_NONCE__">
"use strict";
const $ = id => document.getElementById(id);
const PALETTES = ["blue", "purple", "orange", "yellow"];
const MODES = ["auto", "light", "dark"];
const MODE_NAMES = { auto:"อัตโนมัติ", light:"กลางวัน", dark:"กลางคืน" };
const MOON_PATH = "M236.37,139.4a12,12,0,0,0-12-3A84.07,84.07,0,0,1,119.6,31.59a12,12,0,0,0-15-15A108.86,108.86,0,0,0,49.69,55.07,108,108,0,0,0,136,228a107.09,107.09,0,0,0,64.93-21.69,108.86,108.86,0,0,0,38.44-54.94A12,12,0,0,0,236.37,139.4Zm-49.88,47.74A84,84,0,0,1,68.86,69.51,84.93,84.93,0,0,1,92.27,48.29Q92,52.13,92,56A108.12,108.12,0,0,0,200,164q3.87,0,7.71-.27A84.79,84.79,0,0,1,186.49,187.14Z";
const SUN_PATH = "M116,36V20a12,12,0,0,1,24,0V36a12,12,0,0,1-24,0Zm80,92a68,68,0,1,1-68-68A68.07,68.07,0,0,1,196,128Zm-24,0a44,44,0,1,0-44,44A44.05,44.05,0,0,0,172,128ZM51.51,68.49a12,12,0,1,0,17-17l-12-12a12,12,0,0,0-17,17Zm0,119-12,12a12,12,0,0,0,17,17l12-12a12,12,0,0,0-17-17ZM196,72a12,12,0,0,0,8.49-3.51l12-12a12,12,0,0,0-17-17l-12,12A12,12,0,0,0,196,72Zm8.49,115.51a12,12,0,0,0-17,17l12,12a12,12,0,0,0,17-17ZM48,128a12,12,0,0,0-12-12H20a12,12,0,0,0,0,24H36A12,12,0,0,0,48,128Zm80,80a12,12,0,0,0-12,12v16a12,12,0,0,0,24,0V220A12,12,0,0,0,128,208Zm108-92H220a12,12,0,0,0,0,24h16a12,12,0,0,0,0-24Z";
let current = null;

function toast(msg){
  const t = $("toast"); t.textContent = msg; t.classList.add("show");
  clearTimeout(toast._t); toast._t = setTimeout(() => t.classList.remove("show"), 2600);
}
function systemPrefersDark(){
  return !!(window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches);
}
function applyMode(pref, persist){
  if(MODES.indexOf(pref) < 0) pref = "auto";
  const dark = pref === "dark" || (pref === "auto" && systemPrefersDark());
  const root = document.documentElement;
  root.dataset.mode = pref;
  root.dataset.v6Mode = dark ? "dark" : "light";
  root.style.colorScheme = dark ? "dark" : "light";
  const meta = document.querySelector('meta[name="theme-color"]');
  if(meta) meta.setAttribute("content", dark ? "#1b2440" : "#9dd2f7");
  const btn = $("mode-btn");
  if(btn){
    const next = MODES[(MODES.indexOf(pref) + 1) % MODES.length];
    const label = "โหมด" + MODE_NAMES[pref] + " คลิกเพื่อโหมด" + MODE_NAMES[next];
    btn.title = label;
    btn.setAttribute("aria-label", label);
    btn.setAttribute("aria-pressed", String(dark));
    btn.dataset.pref = pref;
    const path = btn.querySelector("path");
    if(path) path.setAttribute("d", dark ? SUN_PATH : MOON_PATH);
  }
  if(persist){ try{ localStorage.setItem("mem-mode", pref); }catch(e){} }
  return dark;
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
    b.className = "p"; b.dataset.paletteValue = p;
    b.title = p; b.setAttribute("aria-label", "ชุดสี " + p);
    b.setAttribute("aria-pressed", String(document.documentElement.dataset.palette === p));
    b.onclick = () => { document.documentElement.dataset.palette = p;
      try{ localStorage.setItem("mem-palette", p); }catch(e){}
      bar.querySelectorAll(".p").forEach(x => x.setAttribute("aria-pressed","false")); b.setAttribute("aria-pressed","true"); };
    bar.appendChild(b);
  }
}

$("menu-btn").onclick = () => { $("sidebar").classList.contains("open") ? closeMenu() : openMenu(); };
$("scrim").onclick = closeMenu;
$("mode-btn").onclick = () => {
  const pref = document.documentElement.dataset.mode || "auto";
  applyMode(MODES[(MODES.indexOf(pref) + 1) % MODES.length], true);
};
(function watchSystemMode(){
  if(!window.matchMedia) return;
  const mq = window.matchMedia("(prefers-color-scheme: dark)");
  const onChange = () => { if(document.documentElement.dataset.mode === "auto") applyMode("auto", false); };
  if(mq.addEventListener) mq.addEventListener("change", onChange);
  else if(mq.addListener) mq.addListener(onChange);
})();
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
  let palette = "blue", mode = "auto";
  try{
    palette = localStorage.getItem("mem-palette") || "blue";
    mode = localStorage.getItem("mem-mode") || "auto";
  }catch(e){}
  if(PALETTES.indexOf(palette) >= 0) document.documentElement.dataset.palette = palette;
  applyMode(mode, false);
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

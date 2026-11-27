package main

import (
	"io"
	"net/http"
	"strings"
)

// The web UI: a single-page wiki (list, search, read, edit) talking to the
// /v1/wiki and /v1/memory endpoints behind Basic auth. Styling is the local
// "mochi pastel" system vendored from DATA Ledger's tokens.css, with self-hosted
// Itim/Mali/Prompt webfonts embedded in the binary and served from /ui/fonts
// (fonts.go), so there are no external assets and no build step. Layout is
// mobile-first: an off-canvas drawer on phones, a sidebar on wide screens, and
// independently scrollable list/content panes. The whole page is one Go raw
// string, so it must not contain backticks.

const uiHTML = `<!DOCTYPE html>
<html lang="th" data-palette="mochi" data-theme="light">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover">
<meta name="color-scheme" content="light dark">
<meta name="theme-color" content="#FFFDF8">
<title>mem — wiki</title>
<script nonce="__CSP_NONCE__">
/* Paint the persisted palette/theme before first paint; the main script keeps them in sync. */
(function(){
  try{
    var root = document.documentElement;
    var palettes = ["mochi","matcha","soda","sakura"];
    var p = localStorage.getItem("mochi.palette");
    if(palettes.indexOf(p) < 0){
      var oldP = localStorage.getItem("mem-palette");
      p = ({blue:"soda",purple:"mochi",orange:"sakura",yellow:"matcha"})[oldP] || "mochi";
    }
    var t = localStorage.getItem("mochi.theme");
    if(["auto","light","dark"].indexOf(t) < 0){
      var oldT = localStorage.getItem("mem-mode");
      t = ["auto","light","dark"].indexOf(oldT) >= 0 ? oldT : "auto";
    }
    var dark = t === "dark" || (t === "auto" && window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches);
    root.setAttribute("data-palette", p);
    root.setAttribute("data-theme", dark ? "dark" : "light");
    root.style.colorScheme = dark ? "dark" : "light";
    document.querySelector('meta[name="theme-color"]').setAttribute("content", dark ? "#17131F" : "#FFFDF8");
  }catch(e){}
})();
</script>
<style>
/* ---------- mochi webfonts (self-hosted, thai + latin subsets) ---------- */
@font-face{font-family:'Itim';font-style:normal;font-weight:400;font-display:swap;src:url('/ui/fonts/itim-400-thai.woff2') format('woff2');unicode-range:U+02D7, U+0303, U+0331, U+0E01-0E5B, U+200C-200D, U+25CC}
@font-face{font-family:'Itim';font-style:normal;font-weight:400;font-display:swap;src:url('/ui/fonts/itim-400-latin.woff2') format('woff2');unicode-range:U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD}
@font-face{font-family:'Mali';font-style:normal;font-weight:500;font-display:swap;src:url('/ui/fonts/mali-500-thai.woff2') format('woff2');unicode-range:U+02D7, U+0303, U+0331, U+0E01-0E5B, U+200C-200D, U+25CC}
@font-face{font-family:'Mali';font-style:normal;font-weight:500;font-display:swap;src:url('/ui/fonts/mali-500-latin.woff2') format('woff2');unicode-range:U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD}
@font-face{font-family:'Mali';font-style:normal;font-weight:600;font-display:swap;src:url('/ui/fonts/mali-600-thai.woff2') format('woff2');unicode-range:U+02D7, U+0303, U+0331, U+0E01-0E5B, U+200C-200D, U+25CC}
@font-face{font-family:'Mali';font-style:normal;font-weight:600;font-display:swap;src:url('/ui/fonts/mali-600-latin.woff2') format('woff2');unicode-range:U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD}
@font-face{font-family:'Prompt';font-style:normal;font-weight:400;font-display:swap;src:url('/ui/fonts/prompt-400-thai.woff2') format('woff2');unicode-range:U+02D7, U+0303, U+0331, U+0E01-0E5B, U+200C-200D, U+25CC}
@font-face{font-family:'Prompt';font-style:normal;font-weight:400;font-display:swap;src:url('/ui/fonts/prompt-400-latin.woff2') format('woff2');unicode-range:U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD}
@font-face{font-family:'Prompt';font-style:normal;font-weight:600;font-display:swap;src:url('/ui/fonts/prompt-600-thai.woff2') format('woff2');unicode-range:U+02D7, U+0303, U+0331, U+0E01-0E5B, U+200C-200D, U+25CC}
@font-face{font-family:'Prompt';font-style:normal;font-weight:600;font-display:swap;src:url('/ui/fonts/prompt-600-latin.woff2') format('woff2');unicode-range:U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD}
@font-face{font-family:'Prompt';font-style:normal;font-weight:700;font-display:swap;src:url('/ui/fonts/prompt-700-thai.woff2') format('woff2');unicode-range:U+02D7, U+0303, U+0331, U+0E01-0E5B, U+200C-200D, U+25CC}
@font-face{font-family:'Prompt';font-style:normal;font-weight:700;font-display:swap;src:url('/ui/fonts/prompt-700-latin.woff2') format('woff2');unicode-range:U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD}

/* ---------- Vendored mochi tokens ----------
 * Provenance: /home/admin/projects/data-ledger/src/lib/tokens.css (palettes,
 * themes, elevation, dot grid, radii, fonts) and the dashboard's local
 * guardrails in /home/admin/projects/dashboard/src/app.css. Kept here verbatim
 * where practical; no @import and no external font/CDN request. */
:root{
  --hue-pri:256; --hue-acc:330; --hue-sec:165;
  --s-pri:58%; --l-pri:55%; --l-pri-fill:80%; --l-pri-fill-hov:74%; --l-pri-soft:94%; --l-pri-ink:30%;
  --s-acc:65%; --l-acc:56%; --l-acc-fill:84%; --l-acc-fill-hov:78%; --l-acc-soft:94%; --l-acc-ink:32%;
  --s-sec:55%; --l-sec:58%;
  --pri:hsl(var(--hue-pri) var(--s-pri) var(--l-pri));
  --pri-strong:hsl(var(--hue-pri) var(--s-pri) calc(var(--l-pri) - 8%));
  --pri-fill:hsl(var(--hue-pri) calc(var(--s-pri) + 4%) var(--l-pri-fill));
  --pri-fill-hov:hsl(var(--hue-pri) calc(var(--s-pri) + 4%) var(--l-pri-fill-hov));
  --pri-soft:hsl(var(--hue-pri) calc(var(--s-pri) - 4%) var(--l-pri-soft));
  --pri-ink:hsl(var(--hue-pri) calc(var(--s-pri) + 2%) var(--l-pri-ink));
  --acc:hsl(var(--hue-acc) var(--s-acc) var(--l-acc));
  --acc-fill:hsl(var(--hue-acc) calc(var(--s-acc) + 5%) var(--l-acc-fill));
  --acc-fill-hov:hsl(var(--hue-acc) calc(var(--s-acc) + 5%) var(--l-acc-fill-hov));
  --acc-soft:hsl(var(--hue-acc) calc(var(--s-acc) + 5%) var(--l-acc-soft));
  --acc-ink:hsl(var(--hue-acc) calc(var(--s-acc) - 8%) var(--l-acc-ink));
  --sec:hsl(var(--hue-sec) var(--s-sec) var(--l-sec));
  --font-sans:"Prompt","Itim",ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,"Noto Sans Thai",sans-serif;
  --font-display:"Itim","Prompt",ui-sans-serif,system-ui,"Noto Sans Thai",sans-serif;
  --font-fun:"Mali","Itim",cursive;
  --fs-2xs:11px; --fs-xs:12px; --fs-sm:13px; --fs-md:15px; --fs-lg:17px; --fs-xl:20px; --fs-2xl:26px; --fs-3xl:34px;
  --r-scale:1;
  --r-xs:calc(8px * var(--r-scale)); --r-sm:calc(12px * var(--r-scale)); --r-md:calc(16px * var(--r-scale));
  --r-lg:calc(22px * var(--r-scale)); --r-xl:calc(28px * var(--r-scale)); --r-2xl:calc(36px * var(--r-scale)); --r-pill:999px;
  --sp-1:4px; --sp-2:8px; --sp-3:12px; --sp-4:16px; --sp-5:20px; --sp-6:24px; --sp-7:32px; --sp-8:40px;
  --ease-spring:cubic-bezier(.34,1.56,.64,1); --ease-out:cubic-bezier(.22,1,.36,1);
  --dur-fast:120ms; --dur:200ms;
  --elev:1;
  --shadow-sm:0 1px 2px rgb(var(--shadow-rgb) / calc(.06 * var(--elev))),0 1px 3px rgb(var(--shadow-rgb) / calc(.05 * var(--elev)));
  --shadow-md:0 calc(6px * var(--elev)) calc(18px * var(--elev)) rgb(var(--shadow-rgb) / calc(.10 * var(--elev))),0 calc(2px * var(--elev)) calc(5px * var(--elev)) rgb(var(--shadow-rgb) / calc(.06 * var(--elev)));
  --shadow-pop:0 calc(4px * var(--elev)) 0 var(--line-2),var(--shadow-md);
  --shadow-pop-lg:0 calc(7px * var(--elev)) 0 var(--line-2),var(--shadow-md);
  --paper-dot:radial-gradient(circle at center, var(--line) 1px, transparent 1.5px);
  --ring:0 0 0 3px var(--paper-2),0 0 0 5px var(--pri);
}
[data-palette="mochi"]{ --hue-pri:256; --hue-acc:330; --hue-sec:165; --swatch:hsl(256 58% 72%); }
[data-palette="matcha"]{ --hue-pri:158; --hue-acc:84; --hue-sec:200; --swatch:hsl(158 52% 62%); }
[data-palette="soda"]{ --hue-pri:222; --hue-acc:190; --hue-sec:280; --swatch:hsl(222 72% 74%); }
[data-palette="sakura"]{ --hue-pri:338; --hue-acc:16; --hue-sec:300; --swatch:hsl(338 72% 78%); }
[data-theme="light"]{
  --s-pri:58%; --l-pri:55%; --l-pri-fill:80%; --l-pri-fill-hov:74%; --l-pri-soft:94%; --l-pri-ink:30%;
  --s-acc:65%; --l-acc:56%; --l-acc-fill:84%; --l-acc-fill-hov:78%; --l-acc-soft:94%; --l-acc-ink:32%;
  --s-sec:55%; --l-sec:58%;
  --paper:#FFFDF8; --paper-2:#FFFFFF; --paper-3:#F7F2EA;
  --ink:#322A42; --ink-2:#6B6383; --ink-3:#A39BB8;
  --line:#EFE9DF; --line-2:#E2D9CB; --scrim:rgba(50,42,66,.40); --shadow-rgb:50 42 66;
  color-scheme:light;
}
[data-theme="dark"]{
  --s-pri:55%; --l-pri:76%; --l-pri-fill:72%; --l-pri-fill-hov:66%; --l-pri-soft:24%; --l-pri-ink:26%;
  --s-acc:62%; --l-acc:78%; --l-acc-fill:74%; --l-acc-fill-hov:68%; --l-acc-soft:25%; --l-acc-ink:28%;
  --s-sec:55%; --l-sec:66%;
  --paper:#17131F; --paper-2:#211B2C; --paper-3:#2A2338;
  --ink:#F4EFF9; --ink-2:#C9C1DC; --ink-3:#948BAA;
  --line:#332B45; --line-2:#443A5C; --scrim:rgba(8,5,16,.60); --shadow-rgb:0 0 0;
  color-scheme:dark;
}
/* Dashboard-style AA guardrails: darker text on pastel fills in every palette. */
[data-theme="light"]{
  --ink-3:#706B84;
  --pri-text:hsl(var(--hue-pri) 62% 30%); --acc-text:hsl(var(--hue-acc) 65% 28%);
  --on-fill:hsl(var(--hue-pri) 60% 22%); --on-acc-fill:hsl(var(--hue-acc) 65% 22%);
  --danger-text:hsl(4 70% 38%); --danger-fill:hsl(4 85% 93%);
}
[data-theme="dark"]{
  --pri-text:var(--pri); --acc-text:var(--acc);
  --on-fill:hsl(var(--hue-pri) 60% 18%); --on-acc-fill:hsl(var(--hue-acc) 65% 18%);
  --danger-text:hsl(4 85% 76%); --danger-fill:hsl(4 45% 24%);
}

*,*::before,*::after{box-sizing:border-box}
html{height:100%;-webkit-text-size-adjust:100%}
body{
  display:flex;flex-direction:column;
  height:100dvh;overflow:hidden;margin:0;
  font:400 var(--fs-md)/1.5 var(--font-sans);color:var(--ink);background-color:var(--paper);
  background-image:var(--paper-dot);background-size:22px 22px;background-position:-1px -1px;
  -webkit-font-smoothing:antialiased;
  transition:background-color var(--dur) var(--ease-out),color var(--dur) var(--ease-out);
}
::selection{background:var(--pri-soft);color:var(--pri-text)}
:focus-visible{outline:none;box-shadow:var(--ring);border-radius:var(--r-sm)}
a{color:var(--pri-text);text-decoration:underline;text-underline-offset:3px}
a:hover{color:var(--acc-text)}
*{scrollbar-width:thin;scrollbar-color:var(--line-2) transparent}
*::-webkit-scrollbar{width:10px;height:10px}
*::-webkit-scrollbar-thumb{background:var(--line-2);border-radius:999px;border:3px solid transparent;background-clip:content-box}
*::-webkit-scrollbar-track{background:transparent}
.skip{position:absolute;left:12px;top:-60px;z-index:80;background:var(--paper-2);padding:8px 12px;border-radius:var(--r-sm)}
.skip:focus{top:12px}

/* ---------- shared mochi primitives ---------- */
.btn{
  display:inline-flex;align-items:center;justify-content:center;gap:var(--sp-2);
  min-height:40px;padding:8px 15px;border:1px solid transparent;border-radius:var(--r-pill);
  background:var(--paper-2);color:var(--ink-2);font:600 var(--fs-sm)/1.4 var(--font-sans);
  box-shadow:var(--shadow-sm);cursor:pointer;white-space:nowrap;
  transition:transform var(--dur-fast) var(--ease-spring),background-color var(--dur) var(--ease-out),color var(--dur) var(--ease-out);
}
.btn:hover{transform:translateY(-1px);color:var(--pri-text);background:var(--paper-3)}
.btn:active{transform:translateY(1px)}
.btn:disabled{opacity:.55;cursor:not-allowed;transform:none}
.btn.pri{background:var(--pri-fill);color:var(--on-fill)}
.btn.pri:hover{background:var(--pri-fill-hov);color:var(--on-fill)}
.btn.danger{background:var(--danger-fill);color:var(--danger-text)}
.btn.danger:hover{background:var(--danger-fill);color:var(--danger-text)}
.btn.icon{width:40px;min-width:40px;padding:0;font-size:var(--fs-lg)}
.eyebrow{
  display:inline-flex;align-items:center;gap:6px;font:700 var(--fs-xs)/1.3 var(--font-sans);
  color:var(--pri-text);text-transform:uppercase;letter-spacing:.08em;
}
.tag,.badge{
  display:inline-flex;align-items:center;padding:4px 10px;border-radius:var(--r-pill);
  font:600 var(--fs-2xs)/1.4 var(--font-sans);white-space:nowrap;
}
.tag{background:var(--acc-soft);color:var(--acc-text)}
.badge{background:var(--pri-soft);color:var(--pri-text)}
.card{
  background:var(--paper-2);border:1px solid var(--line);border-radius:var(--r-lg);
  box-shadow:var(--shadow-pop);padding:var(--sp-5);
}

/* ---------- app shell ---------- */
header{
  position:relative;z-index:20;flex:none;display:flex;align-items:center;gap:var(--sp-3);flex-wrap:wrap;
  padding:12px clamp(14px,2.4vw,26px);background:var(--paper-2);border-bottom:1px solid var(--line);
  box-shadow:var(--shadow-sm);
}
.hamburger{display:none}
.brand{display:flex;align-items:center;gap:10px;flex:none}
.brand .mark{
  display:grid;place-items:center;width:36px;height:36px;border-radius:var(--r-md);
  background:var(--pri-fill);color:var(--on-fill);font:700 var(--fs-xl)/1 var(--font-display);
}
.brand .name{font:700 var(--fs-xl)/1.1 var(--font-display);letter-spacing:-.01em}
.brand .sub{font:700 var(--fs-2xs)/1 var(--font-sans);color:var(--ink-2);text-transform:uppercase;letter-spacing:.14em}
.grow{flex:1;min-width:0}
.search{position:relative;flex:1;min-width:150px;max-width:390px;display:flex}
.search input{
  width:100%;min-height:40px;padding:8px 14px 8px 36px;
  font:400 var(--fs-sm)/1.5 var(--font-sans);color:var(--ink);background:var(--paper-3);
  border:1px solid var(--line);border-radius:var(--r-pill);outline:none;
}
.search input:focus{background:var(--paper-2);box-shadow:var(--ring)}
.search input::placeholder{color:var(--ink-2)}
.search .ico{position:absolute;left:13px;top:50%;transform:translateY(-50%);color:var(--ink-2);pointer-events:none}
.palette-bar{display:flex;gap:6px;flex:none}
.palette-bar .p{
  width:26px;height:26px;min-height:0;padding:0;border-radius:50%;flex:none;
  border:2px solid var(--paper-2);background:var(--swatch);box-shadow:var(--shadow-sm);
}
.palette-bar .p:hover{transform:translateY(-1px)}
.palette-bar .p[aria-pressed="true"]{box-shadow:var(--ring)}
#mode-btn{min-width:100px}

.main{flex:1;display:flex;min-height:0;position:relative}
nav.sidebar{
  width:288px;flex-shrink:0;display:flex;flex-direction:column;min-height:0;
  background:var(--paper-2);border-right:1px solid var(--line);
}
.sidebar .tools{display:flex;gap:8px;padding:var(--sp-3);border-bottom:1px solid var(--line)}
.sidebar .tools .btn{flex:1;padding:7px 10px;min-height:36px;font-size:var(--fs-xs)}
.count{
  display:flex;align-items:center;justify-content:space-between;
  padding:var(--sp-4) var(--sp-4) var(--sp-2);font:700 var(--fs-2xs)/1.4 var(--font-sans);
  color:var(--ink-2);font-family:var(--font-fun);text-transform:uppercase;letter-spacing:.12em;
}
.count strong{color:var(--pri-text);font-family:var(--font-sans);font-weight:700;letter-spacing:0;text-transform:none}
.list{flex:1;min-height:0;overflow-y:auto;padding:0 var(--sp-2) var(--sp-4)}
.item{
  padding:10px 12px;margin-bottom:6px;border-radius:var(--r-md);cursor:pointer;
  border:1px solid transparent;transition:background-color var(--dur) var(--ease-out),border-color var(--dur) var(--ease-out);
}
.item:hover{background:var(--paper-3)}
.item.active{background:var(--pri-soft);border-color:var(--pri-fill-hov)}
.item .t{font:600 var(--fs-sm)/1.35 var(--font-display);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.item .m{
  display:flex;gap:6px;align-items:center;flex-wrap:wrap;margin-top:4px;
  font:400 var(--fs-2xs)/1.5 var(--font-sans);color:var(--ink-2);
}
.item .m .tag{padding:2px 7px;font-size:10px}

section.content{flex:1;min-width:0;min-height:0;overflow-y:auto;padding:clamp(18px,3vw,32px)}
.empty{color:var(--ink-2);text-align:center;padding:56px 20px;font-size:var(--fs-md)}
.page-title{font:700 clamp(24px,3vw,34px)/1.15 var(--font-display);margin:0 0 6px;letter-spacing:-.01em}
.meta{
  display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin:0 0 var(--sp-4);
  color:var(--ink-2);font:400 var(--fs-xs)/1.5 var(--font-sans);
}
.actions{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:var(--sp-4)}
.prose{max-width:900px;line-height:1.7}
.prose h1{font:700 var(--fs-2xl)/1.2 var(--font-display);margin:0 0 10px}
.prose h2{font:700 var(--fs-xl)/1.3 var(--font-display);color:var(--pri-text);margin:22px 0 8px}
.prose h3{font:600 var(--fs-lg)/1.35 var(--font-display);margin:16px 0 6px}
.prose p{margin:8px 0}
.prose ul,.prose ol{margin:8px 0 8px 22px}
.prose li{margin:3px 0}
.prose code{font:400 var(--fs-sm)/1.5 var(--font-sans);background:var(--paper-3);border-radius:var(--r-xs);padding:2px 6px}
.prose pre{background:var(--paper-3);border:1px solid var(--line);border-radius:var(--r-sm);padding:12px;overflow-x:auto;margin:10px 0}
.prose pre code{background:none;padding:0}
textarea.editor{
  width:100%;min-height:60vh;padding:14px 16px;resize:vertical;
  font:400 var(--fs-sm)/1.7 var(--font-sans);color:var(--ink);background:var(--paper-2);
  border:1px solid var(--line);border-radius:var(--r-md);outline:none;
}
textarea.editor:focus{box-shadow:var(--ring)}
.hit{
  background:var(--paper-2);border:1px solid var(--line);border-radius:var(--r-md);
  box-shadow:var(--shadow-sm);padding:13px 15px;margin-bottom:10px;cursor:pointer;
  transition:transform var(--dur-fast) var(--ease-spring),background-color var(--dur) var(--ease-out);
}
.hit:hover{background:var(--pri-soft);transform:translateY(-1px)}
.hit .t{font:600 var(--fs-md)/1.3 var(--font-display);margin-bottom:3px}
.hit .x{font:400 var(--fs-sm)/1.6 var(--font-sans);color:var(--ink-2);white-space:pre-wrap;word-break:break-word;max-height:5.2em;overflow:hidden}
.search-head{font:700 var(--fs-2xl)/1.3 var(--font-display);margin:0 0 var(--sp-4)}

/* ---------- scrim, toast, dialog ---------- */
.scrim{position:fixed;inset:0;background:var(--scrim);z-index:29;display:none}
.toast{
  position:fixed;left:50%;bottom:20px;transform:translateX(-50%) translateY(80px);
  background:var(--pri-soft);color:var(--pri-text);border:1px solid var(--line-2);
  padding:11px 18px;border-radius:var(--r-pill);box-shadow:var(--shadow-pop);
  font:600 var(--fs-sm)/1.4 var(--font-sans);z-index:60;opacity:0;
  transition:transform var(--dur) var(--ease-out),opacity var(--dur) var(--ease-out);
}
.toast.show{transform:translateX(-50%) translateY(0);opacity:1}
dialog{
  border:1px solid var(--line);border-radius:var(--r-xl);box-shadow:var(--shadow-pop);
  background:var(--paper-2);color:var(--ink);padding:var(--sp-6);width:min(92vw,440px);
}
dialog::backdrop{background:var(--scrim)}
dialog h2{font:700 var(--fs-2xl)/1.3 var(--font-display);margin:0 0 var(--sp-4)}
dialog input{
  width:100%;min-height:44px;margin-bottom:var(--sp-4);padding:9px 14px;
  font:400 var(--fs-md)/1.5 var(--font-sans);color:var(--ink);background:var(--paper-3);
  border:1px solid var(--line);border-radius:var(--r-sm);outline:none;
}
dialog input:focus{box-shadow:var(--ring)}
dialog .row{display:flex;gap:8px;justify-content:flex-end}

/* ---------- mobile: off-canvas drawer ---------- */
@media (max-width:760px){
  .hamburger{display:inline-flex}
  nav.sidebar{
    position:fixed;top:0;left:0;bottom:0;width:min(85vw,320px);z-index:30;
    transform:translateX(-102%);transition:transform var(--dur) var(--ease-out);
    box-shadow:var(--shadow-pop-lg);border-right:1px solid var(--line);
  }
  nav.sidebar.open{transform:none}
  .scrim.show{display:block}
  .search{order:4;flex-basis:100%;max-width:none}
  section.content{padding:18px 16px 40px}
  .prose{padding:var(--sp-4);border-radius:var(--r-md)}
  .brand .sub{display:none}
}
@media (max-width:420px){ .brand .name{display:none} #mode-btn{min-width:0;padding:8px 11px} }
@media (prefers-reduced-motion:reduce){
  *,*::before,*::after{animation-duration:.01ms !important;transition-duration:.01ms !important}
}
</style>
</head>
<body>
<a class="skip" href="#content">ข้ามไปเนื้อหา</a>
<header>
  <button class="btn icon hamburger" id="menu-btn" type="button" aria-label="เปิดเมนู" aria-expanded="false">☰</button>
  <div class="brand"><span class="mark" aria-hidden="true">m</span><span class="name">mem</span><span class="sub">wiki</span></div>
  <div class="grow"></div>
  <div class="search">
    <span class="ico" aria-hidden="true">⌕</span>
    <input id="q" type="search" placeholder="ค้นหา..." autocomplete="off" aria-label="ค้นหา">
  </div>
  <div class="palette-bar" id="palettes" role="group" aria-label="ชุดสี"></div>
  <button class="btn" id="mode-btn" type="button" data-pref="auto" aria-pressed="false" aria-label="โหมดอัตโนมัติ" title="โหมดอัตโนมัติ">
    <span id="mode-icon" aria-hidden="true">☀</span><span id="mode-label">อัตโนมัติ</span>
  </button>
  <button class="btn pri" id="new-btn" type="button">+ สร้างหน้า</button>
</header>

<div class="main">
  <nav class="sidebar" id="sidebar" aria-label="รายการหน้า">
    <div class="tools">
      <button class="btn" id="refresh-btn" type="button">↻ โหลดใหม่</button>
      <button class="btn" id="all-btn" type="button">ทั้งหมด</button>
    </div>
    <div class="count" id="count">หน้า</div>
    <div class="list" id="list" tabindex="0" aria-label="รายการหน้า"></div>
  </nav>
  <div class="scrim" id="scrim"></div>
  <section class="content" id="content" tabindex="0" aria-label="เนื้อหาหน้า"><div class="empty">เลือกหน้าจากเมนู หรือกด + สร้าง เพื่อเริ่มเขียน</div></section>
</div>

<dialog id="new-dlg" aria-labelledby="new-title-h">
  <h2 id="new-title-h">หน้าใหม่</h2>
  <form id="new-form">
    <input id="new-title" placeholder="ชื่อหน้า..." aria-label="ชื่อหน้า" required>
    <div class="row"><button class="btn" type="button" id="new-cancel">ยกเลิก</button><button class="btn pri" type="submit">สร้าง</button></div>
  </form>
</dialog>

<div class="toast" id="toast" role="status" aria-live="polite"></div>

<script nonce="__CSP_NONCE__">
"use strict";
const $ = id => document.getElementById(id);
const PALETTES = ["mochi", "matcha", "soda", "sakura"];
const MODES = ["auto", "light", "dark"];
const MODE_NAMES = {auto:"อัตโนมัติ", light:"สว่าง", dark:"มืด"};
const KEY_THEME = "mochi.theme";
const KEY_PALETTE = "mochi.palette";
const OLD_PALETTE = {blue:"soda", purple:"mochi", orange:"sakura", yellow:"matcha"};
let current = null;

function toast(msg){
  const t = $("toast"); t.textContent = msg; t.classList.add("show");
  clearTimeout(toast._t); toast._t = setTimeout(() => t.classList.remove("show"), 2600);
}
function systemPrefersDark(){
  return !!(window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches);
}
function paintTheme(pref){
  if(MODES.indexOf(pref) < 0) pref = "auto";
  const dark = pref === "dark" || (pref === "auto" && systemPrefersDark());
  const root = document.documentElement;
  root.setAttribute("data-theme", dark ? "dark" : "light");
  root.dataset.pref = pref;
  root.style.colorScheme = dark ? "dark" : "light";
  const meta = document.querySelector('meta[name="theme-color"]');
  if(meta) meta.setAttribute("content", dark ? "#17131F" : "#FFFDF8");
  const next = MODES[(MODES.indexOf(pref) + 1) % MODES.length];
  const btn = $("mode-btn");
  if(btn){
    btn.dataset.pref = pref;
    btn.setAttribute("aria-pressed", String(dark));
    btn.title = "โหมด" + MODE_NAMES[pref] + " คลิกเพื่อโหมด" + MODE_NAMES[next];
    btn.setAttribute("aria-label", btn.title);
  }
  const label = $("mode-label"); if(label) label.textContent = MODE_NAMES[pref];
  const icon = $("mode-icon"); if(icon) icon.textContent = dark ? "☾" : "☀";
  return dark;
}
function applyTheme(pref, persist){
  if(MODES.indexOf(pref) < 0) pref = "auto";
  paintTheme(pref);
  if(persist){ try{ localStorage.setItem(KEY_THEME, pref); }catch(e){} }
}
function applyPalette(p, persist){
  if(PALETTES.indexOf(p) < 0) p = "mochi";
  document.documentElement.setAttribute("data-palette", p);
  const bar = $("palettes");
  if(bar) bar.querySelectorAll(".p").forEach(x => x.setAttribute("aria-pressed", String(x.dataset.paletteValue === p)));
  if(persist){ try{ localStorage.setItem(KEY_PALETTE, p); }catch(e){} }
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
    $("count").innerHTML = "<span>หน้าทั้งหมด</span><strong>" + data.pages.length + "</strong>";
    if(!data.pages.length){ el.innerHTML = "<div class=\"empty\">ยังไม่มีหน้า</div>"; return; }
    for(const p of data.pages){
      const d = document.createElement("div");
      d.className = "item" + (current === p.slug ? " active" : "");
      d.setAttribute("role", "button");
      d.setAttribute("tabindex", "0");
      const tags = (p.tags || []).map(t => "<span class=\"tag\">" + esc(t) + "</span>").join("");
      d.innerHTML = "<div class=\"t\">" + esc(p.title) + "</div><div class=\"m\">" +
        esc((p.updated_at || "").slice(0,10)) + (p.updated_by ? " · " + esc(p.updated_by) : "") + tags + "</div>";
      d.onclick = () => { showPage(p.slug); closeMenu(); };
      d.onkeydown = e => { if(e.key === "Enter" || e.key === " "){ e.preventDefault(); showPage(p.slug); closeMenu(); } };
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
      "<div class=\"actions\"><button class=\"btn pri\" id=\"edit-btn\" type=\"button\">แก้ไข</button>" +
      "<button class=\"btn danger\" id=\"del-btn\" type=\"button\">ย้ายไปถังขยะ</button></div>" +
      "<h1 class=\"page-title\">" + esc(p.title) + "</h1>" +
      "<div class=\"meta\">" + tags + " <span>" + esc((p.updated_at||"").slice(0,16).replace("T"," ")) + "</span>" +
      (p.updated_by ? "<span>โดย " + esc(p.updated_by) + "</span>" : "") + "</div>" +
      "<div class=\"prose card\">" + md(p.body) + "</div>";
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
    "<div class=\"actions\"><button class=\"btn pri\" id=\"save-btn\" type=\"button\">บันทึก</button>" +
    "<button class=\"btn\" id=\"cancel-btn\" type=\"button\">ยกเลิก</button></div>" +
    "<label class=\"eyebrow\" for=\"ta\">เนื้อหา</label>" +
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
    if(!data.results.length){ $("content").innerHTML = "<div class=\"empty\">ไม่พบผลลัพธ์สําหรับ " + esc(q) + "</div>"; return; }
    let html = "<h2 class=\"search-head\">ค้นหา: " + esc(q) + " <span class=\"badge\">" + data.results.length + "</span></h2>";
    for(const r of data.results){
      const slug = r.page || r.slug;
      html += "<div class=\"hit\" role=\"button\" tabindex=\"0\" data-slug=\"" + esc(slug) + "\"><div class=\"t\">" + esc(r.title || r.topic || slug) + "</div>" +
        "<div class=\"x\">" + esc((r.body || "").slice(0,300)) + "</div></div>";
    }
    $("content").innerHTML = html;
    $("content").querySelectorAll(".hit").forEach(el => {
      const go = () => { const s = el.getAttribute("data-slug"); if(s) showPage(s); };
      el.onclick = go;
      el.onkeydown = e => { if(e.key === "Enter" || e.key === " "){ e.preventDefault(); go(); } };
    });
  }catch(e){ toast("Error: " + e.message); }
}

function buildPalettes(){
  const bar = $("palettes");
  for(const p of PALETTES){
    const b = document.createElement("button");
    b.className = "p"; b.type = "button"; b.dataset.paletteValue = p;
    b.title = "ชุดสี " + p; b.setAttribute("aria-label", "ชุดสี " + p);
    b.setAttribute("aria-pressed", String(document.documentElement.getAttribute("data-palette") === p));
    b.onclick = () => applyPalette(p, true);
    bar.appendChild(b);
  }
}

$("menu-btn").onclick = () => { $("sidebar").classList.contains("open") ? closeMenu() : openMenu(); };
$("scrim").onclick = closeMenu;
$("mode-btn").onclick = () => {
  const pref = document.documentElement.dataset.pref || "auto";
  applyTheme(MODES[(MODES.indexOf(pref) + 1) % MODES.length], true);
};
(function watchSystemMode(){
  if(!window.matchMedia) return;
  const mq = window.matchMedia("(prefers-color-scheme: dark)");
  const onChange = () => { if(($("mode-btn").dataset.pref || "auto") === "auto") paintTheme("auto"); };
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
  let palette = "mochi", mode = "auto";
  try{
    palette = localStorage.getItem(KEY_PALETTE) || OLD_PALETTE[localStorage.getItem("mem-palette")] || "mochi";
    mode = localStorage.getItem(KEY_THEME) || localStorage.getItem("mem-mode") || "auto";
  }catch(e){}
  paintTheme(mode);
  applyPalette(palette, false);
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

package main

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// uiFonts carries the self-hosted mochi webfonts (thai + latin subsets of
// Itim, Mali and Prompt, vendored from truck/apps/web-svelte/public/fonts)
// inside the binary, so the wiki UI keeps its "no external assets" property
// while the strict CSP can stay same-origin.
//
//go:embed fonts/*.woff2
var uiFonts embed.FS

// handleUIFont serves /ui/fonts/<file> from the embedded font set.
func handleUIFont(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/ui/fonts/")
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	b, err := fs.ReadFile(uiFonts, "fonts/"+name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "font/woff2")
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b)
}

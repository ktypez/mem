package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HTTP surface: the wiki API (/v1/wiki/*) plus compatibility endpoints
// (/v1/memory/*) so existing clients — the mem MCP wrapper, the Hermes plugin,
// dashboards — keep working unchanged on top of the wiki store.

// writerIdentity labels who is writing. Accepted: X-Mem-Source header, JSON
// "source", JSON "bot_id". It is provenance only; there is no write gate.
func writerIdentity(r *http.Request, data map[string]interface{}) string {
	if v := strings.TrimSpace(r.Header.Get("X-Mem-Source")); v != "" {
		return v
	}
	if data != nil {
		if v, ok := data["source"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		if v, ok := data["bot_id"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return "agent"
}

func jsonResp(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// --- health & stats ---

func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	pages, _ := listPages()
	jsonResp(w, http.StatusOK, map[string]interface{}{
		"status": "ok", "service": "mem", "mode": "wiki", "pages": len(pages),
	})
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	pages, err := listPages()
	if err != nil {
		jsonResp(w, http.StatusServiceUnavailable, map[string]string{"error": "unable to list pages"})
		return
	}
	// Compat: older clients read nodes/edges.
	jsonResp(w, http.StatusOK, map[string]interface{}{
		"pages":  len(pages),
		"nodes":  len(pages),
		"edges":  0,
		"layers": map[string]int{},
		"status": map[string]int{"active": len(pages)},
	})
}

// --- wiki API ---

func tagsFromBody(data map[string]interface{}) []string {
	var tags []string
	switch v := data["tags"].(type) {
	case []interface{}:
		for _, t := range v {
			if s, ok := t.(string); ok && strings.TrimSpace(s) != "" {
				tags = append(tags, strings.TrimSpace(s))
			}
		}
	case string:
		for _, t := range strings.Split(v, ",") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
	}
	return tags
}

func handleWikiPages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		pages, err := listPages()
		if err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		// Trim the bodies: the list view does not need full content.
		type summary struct {
			Page
			Body string `json:"body"`
		}
		out := make([]summary, 0, len(pages))
		for _, p := range pages {
			p.Body = excerpt(p.Body, "", 160)
			out = append(out, summary{Page: p})
		}
		jsonResp(w, 200, map[string]interface{}{"pages": out, "count": len(out)})

	case http.MethodPost:
		var data map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			jsonResp(w, 400, map[string]string{"error": "invalid JSON"})
			return
		}
		title, _ := data["title"].(string)
		body, _ := data["body"].(string)
		if strings.TrimSpace(title) == "" && strings.TrimSpace(body) == "" {
			jsonResp(w, 400, map[string]string{"error": "title or body required"})
			return
		}
		by := writerIdentity(r, data)
		var p Page
		var err error
		if slug, _ := data["slug"].(string); strings.TrimSpace(slug) != "" {
			p, err = upsertPage(slug, title, body, tagsFromBody(data), by)
		} else {
			slug, slugErr := slugForTitle(title)
			if slugErr != nil {
				jsonResp(w, 500, map[string]string{"error": slugErr.Error()})
				return
			}
			p, err = upsertPage(slug, title, body, tagsFromBody(data), by)
		}
		if err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		jsonResp(w, http.StatusCreated, p)

	default:
		w.Header().Set("Allow", "GET, POST")
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func handleWikiPage(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/v1/wiki/pages/")
	if slug == "" {
		jsonResp(w, 400, map[string]string{"error": "missing slug"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		p, err := loadPage(slug)
		if err != nil {
			jsonResp(w, 404, map[string]string{"error": "page not found"})
			return
		}
		jsonResp(w, 200, p)

	case http.MethodPut:
		var data map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			jsonResp(w, 400, map[string]string{"error": "invalid JSON"})
			return
		}
		body, _ := data["body"].(string)
		title, _ := data["title"].(string)
		p, err := upsertPage(slug, title, body, tagsFromBody(data), writerIdentity(r, data))
		if err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		jsonResp(w, 200, p)

	case http.MethodDelete:
		dst, err := deletePage(slug)
		if err != nil {
			jsonResp(w, 404, map[string]string{"error": err.Error()})
			return
		}
		jsonResp(w, 200, map[string]string{"status": "trashed", "path": dst})

	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func handleWikiReindex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	n, err := rescanWiki()
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResp(w, 200, map[string]interface{}{"status": "ok", "synced": n})
}

// --- compat: /v1/memory/* ---

// searchResultsNode shapes a search hit like an old memory node so existing
// clients keep parsing the same fields.
func searchResultsNode(h searchHit) map[string]interface{} {
	return map[string]interface{}{
		"id":          h.Slug,
		"body":        h.Excerpt,
		"topic":       h.Title,
		"subject":     h.Title,
		"title":       h.Title,
		"page":        h.Slug,
		"tags":        h.Tags,
		"layer":       "semantic",
		"status":      "active",
		"captured_at": time.Now().Format("2006-01-02 15:04:05"),
		"updated_at":  time.Now().Format(time.RFC3339),
	}
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" && r.Method == http.MethodPost {
		var data map[string]interface{}
		json.NewDecoder(r.Body).Decode(&data)
		q, _ = data["query"].(string)
	}
	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if r.Method == http.MethodPost {
		var data map[string]interface{}
		json.NewDecoder(r.Body).Decode(&data)
		if v, ok := data["limit"].(float64); ok && int(v) > 0 {
			limit = int(v)
		}
	}
	if limit < 1 || limit > 200 {
		limit = 10
	}
	hits := searchWiki(q, limit)
	results := make([]map[string]interface{}, 0, len(hits))
	for _, h := range hits {
		results = append(results, searchResultsNode(h))
	}
	jsonResp(w, 200, map[string]interface{}{"results": results, "count": len(results), "semantic": false})
}

func handleContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var query string
	limit := 5
	if r.Method == http.MethodGet {
		query = r.URL.Query().Get("q")
		if l := r.URL.Query().Get("limit"); l != "" {
			fmt.Sscanf(l, "%d", &limit)
		}
	} else {
		var data map[string]interface{}
		json.NewDecoder(r.Body).Decode(&data)
		query, _ = data["query"].(string)
		if v, ok := data["limit"].(float64); ok {
			limit = int(v)
		}
	}
	if limit < 1 || limit > 50 {
		limit = 5
	}
	hits := searchWiki(query, limit)
	parts := make([]string, 0, len(hits))
	for _, h := range hits {
		if strings.TrimSpace(h.Excerpt) != "" {
			parts = append(parts, fmt.Sprintf("[%s] %s", h.Title, h.Excerpt))
		}
	}
	jsonResp(w, 200, map[string]interface{}{"context": strings.Join(parts, "\n---\n")})
}

// handleNodesCompat keeps POST /v1/memory/nodes working: the body becomes a
// fact line appended to the topic page (subject, then "Inbox", as fallbacks).
func handleNodesCompat(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		pages, err := listPages()
		if err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		nodes := make([]map[string]interface{}, 0, len(pages))
		for _, p := range pages {
			nodes = append(nodes, searchResultsNode(searchHit{
				Slug: p.Slug, Title: p.Title, Tags: p.Tags, Body: p.Body, Excerpt: excerpt(p.Body, "", 160),
			}))
		}
		jsonResp(w, 200, map[string]interface{}{"nodes": nodes, "count": len(nodes)})

	case http.MethodPost:
		var data map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			jsonResp(w, 400, map[string]string{"error": "invalid JSON"})
			return
		}
		body, _ := data["body"].(string)
		if strings.TrimSpace(body) == "" {
			jsonResp(w, http.StatusBadRequest, map[string]string{"error": "body is required and must be a non-empty string"})
			return
		}
		topic, _ := data["topic"].(string)
		subject, _ := data["subject"].(string)
		by := writerIdentity(r, data)
		p, created, err := appendFact(topic, subject, body, by)
		if err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		jsonResp(w, http.StatusCreated, map[string]interface{}{
			"id": p.Slug, "slug": p.Slug, "page": p.Title, "body": body,
			"source": by, "status": "active", "created": created, "appended": true,
		})

	default:
		w.Header().Set("Allow", "GET, POST")
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleNodeCompat serves single-page operations under the old node path.
// Old promote/reject inbox calls are accepted and answered as no-ops.
func handleNodeCompat(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/memory/nodes/")
	id = strings.TrimSuffix(id, "/promote")
	id = strings.TrimSuffix(id, "/reject")
	id = strings.TrimPrefix(id, "/api/nodes/")
	if id == "" {
		jsonResp(w, 400, map[string]string{"error": "missing id"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		p, err := loadPage(id)
		if err != nil {
			jsonResp(w, 404, map[string]string{"error": "not found"})
			return
		}
		jsonResp(w, 200, searchResultsNode(searchHit{Slug: p.Slug, Title: p.Title, Tags: p.Tags, Body: p.Body, Excerpt: p.Body}))
	case http.MethodDelete:
		dst, err := deletePage(id)
		if err != nil {
			jsonResp(w, 404, map[string]string{"error": err.Error()})
			return
		}
		jsonResp(w, 200, map[string]string{"status": "trashed", "path": dst})
	case http.MethodPost:
		// Compat for the retired inbox flow.
		jsonResp(w, 200, map[string]string{"status": "noop", "reason": "wiki mode has no inbox; all writes are active"})
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func handleInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	jsonResp(w, 200, map[string]interface{}{"nodes": []Node{}, "count": 0})
}

// Node exists only for compat shapes (empty inbox responses).
type Node struct {
	ID string `json:"id"`
}

func handleExtract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var data map[string]interface{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&data); err != nil {
		jsonResp(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON request"})
		return
	}
	text, _ := data["text"].(string)
	if strings.TrimSpace(text) == "" {
		jsonResp(w, 400, map[string]string{"error": "text required"})
		return
	}
	topic, _ := data["topic"].(string)
	subject, _ := data["subject"].(string)
	by := writerIdentity(r, data)
	p, created, err := appendFact(topic, subject, text, by)
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResp(w, http.StatusCreated, map[string]interface{}{
		"extracted": 1,
		"status":    "active",
		"source":    by,
		"page":      p.Title,
		"slug":      p.Slug,
		"created":   created,
		"nodes":     []map[string]string{{"id": p.Slug, "body": text}},
	})
}

// handleBrief returns a one-screen summary: the most recently updated pages
// with their first lines. It replaces the old system-facts brief.
func handleBrief(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	pages, err := listPages()
	if err != nil {
		jsonResp(w, http.StatusServiceUnavailable, map[string]string{"error": "unable to list pages"})
		return
	}
	limit := 8
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	type pageBrief struct {
		Title     string   `json:"title"`
		Slug      string   `json:"slug"`
		Tags      []string `json:"tags"`
		UpdatedAt string   `json:"updated_at"`
		UpdatedBy string   `json:"updated_by"`
		Preview   string   `json:"preview"`
	}
	out := make([]pageBrief, 0, len(pages))
	for i, p := range pages {
		if i >= limit {
			break
		}
		out = append(out, pageBrief{
			Title: p.Title, Slug: p.Slug, Tags: p.Tags,
			UpdatedAt: p.UpdatedAt, UpdatedBy: p.UpdatedBy,
			Preview: excerpt(p.Body, "", 200),
		})
	}
	jsonResp(w, 200, map[string]interface{}{"pages": out, "count": len(out), "total": len(pages)})
}

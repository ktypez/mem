package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var pool *pgxpool.Pool

const port = "3200"

// defaultListenAddr is where the server binds when MEM_LISTEN_ADDR is unset:
// loopback only.
func defaultListenAddr() string {
	return "127.0.0.1:" + port
}

// splitList splits a comma-separated environment value, dropping blanks.
func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Node represents a memory node (no bot_id)
type Node struct {
	ID         string  `json:"id"`
	Body       string  `json:"body"`
	Layer      string  `json:"layer"`
	Topic      string  `json:"topic"`
	Subject    string  `json:"subject"`
	Confidence float64 `json:"confidence"`
	FactType   string  `json:"fact_type"`
	Metadata   string  `json:"metadata"`
	CapturedAt string  `json:"captured_at"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
	Source     string  `json:"source"`
	Status     string  `json:"status"`
	FactKey    string  `json:"fact_key,omitempty"`
}

// Edge represents a relationship between nodes
type Edge struct {
	ID        int64   `json:"id"`
	Src       string  `json:"src"`
	Dst       string  `json:"dst"`
	Rel       string  `json:"rel"`
	Weight    float64 `json:"weight"`
	CreatedAt string  `json:"created_at"`
}

// Backup represents a full database export
type Backup struct {
	Version   string `json:"version"`
	Timestamp string `json:"timestamp"`
	Nodes     []Node `json:"nodes"`
	Edges     []Edge `json:"edges"`
}

func initDB() {
	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required; refusing to use a built-in database credential")
	}
	var err error
	pool, err = pgxpool.New(context.Background(), dbURL)
	if err != nil {
		log.Fatalf("Unable to configure database connection: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		log.Fatalf("Unable to connect to database: %v", err)
	}

	// Create tables
	_, err = pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS memory_nodes (
			id TEXT PRIMARY KEY,
			body TEXT NOT NULL,
			hash TEXT NOT NULL,
			layer TEXT NOT NULL DEFAULT 'semantic',
			fact_type TEXT NOT NULL DEFAULT 'fact',
			subject TEXT,
			confidence REAL NOT NULL DEFAULT 1.0,
			metadata TEXT DEFAULT '{}',
			topic TEXT,
			captured_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS memory_edges (
			id BIGSERIAL PRIMARY KEY,
			src TEXT NOT NULL REFERENCES memory_nodes(id) ON DELETE CASCADE,
			dst TEXT NOT NULL REFERENCES memory_nodes(id) ON DELETE CASCADE,
			rel TEXT NOT NULL,
			weight REAL NOT NULL DEFAULT 1.0,
			metadata TEXT DEFAULT '{}',
			created_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_nodes_layer ON memory_nodes(layer);
		CREATE INDEX IF NOT EXISTS idx_nodes_topic ON memory_nodes(topic);
		CREATE INDEX IF NOT EXISTS idx_edges_src ON memory_edges(src);
		CREATE INDEX IF NOT EXISTS idx_edges_dst ON memory_edges(dst);

		-- Steward columns (migration-safe for existing databases)
		ALTER TABLE memory_nodes ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'agent';
		ALTER TABLE memory_nodes ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active';
		ALTER TABLE memory_nodes ADD COLUMN IF NOT EXISTS fact_key TEXT;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_nodes_fact_key ON memory_nodes(fact_key) WHERE fact_key IS NOT NULL;
		CREATE INDEX IF NOT EXISTS idx_nodes_status ON memory_nodes(status);
	`)
	if err != nil {
		log.Fatalf("Unable to create tables: %v", err)
	}
	log.Println("Database initialized")
}

func now() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

func hashBody(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func jsonResp(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM memory_nodes").Scan(&count); err != nil {
		jsonResp(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "service": "mem", "error": "database query failed"})
		return
	}
	jsonResp(w, http.StatusOK, map[string]interface{}{"status": "ok", "service": "mem", "nodes": count})
}

func handleNodes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		// Support filters: limit, layer, topic, subject, status
		q := r.URL.Query()
		limit := 100
		if l := q.Get("limit"); l != "" {
			fmt.Sscanf(l, "%d", &limit)
		}
		if limit < 1 {
			limit = 100
		}
		if limit > 5000 {
			limit = 5000
		}
		layer := q.Get("layer")
		topic := q.Get("topic")
		subject := q.Get("subject")
		status := strings.TrimSpace(q.Get("status"))

		query := `SELECT id, body, layer, topic, subject, confidence, fact_type, metadata, captured_at, created_at, updated_at, source, status, COALESCE(fact_key, '') FROM memory_nodes WHERE 1=1`
		args := []interface{}{}
		argIdx := 1
		if layer != "" {
			query += fmt.Sprintf(" AND layer = $%d", argIdx)
			args = append(args, layer)
			argIdx++
		}
		if topic != "" {
			query += fmt.Sprintf(" AND topic = $%d", argIdx)
			args = append(args, topic)
			argIdx++
		}
		if subject != "" {
			query += fmt.Sprintf(" AND subject = $%d", argIdx)
			args = append(args, subject)
			argIdx++
		}
		// Default view is the trusted set: retired and proposed nodes stay out
		// of normal reads unless explicitly asked for.
		switch status {
		case "", statusActive:
			query += fmt.Sprintf(" AND status = $%d", argIdx)
			args = append(args, statusActive)
			argIdx++
		case "all":
			// no status filter
		default:
			query += fmt.Sprintf(" AND status = $%d", argIdx)
			args = append(args, status)
			argIdx++
		}
		query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", argIdx)
		args = append(args, limit)

		rows, err := pool.Query(context.Background(), query, args...)
		if err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		nodes := []Node{}
		for rows.Next() {
			var n Node
			var captured, created, updated interface{}
			if err := rows.Scan(&n.ID, &n.Body, &n.Layer, &n.Topic, &n.Subject, &n.Confidence, &n.FactType, &n.Metadata, &captured, &created, &updated, &n.Source, &n.Status, &n.FactKey); err != nil {
				continue
			}
			n.CapturedAt = fmt.Sprint(captured)
			n.CreatedAt = fmt.Sprint(created)
			n.UpdatedAt = fmt.Sprint(updated)
			nodes = append(nodes, n)
		}
		jsonResp(w, 200, map[string]interface{}{"nodes": nodes, "count": len(nodes)})

	case "POST":
		var data map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			jsonResp(w, 400, map[string]string{"error": err.Error()})
			return
		}
		body, _ := data["body"].(string)
		if strings.TrimSpace(body) == "" {
			jsonResp(w, http.StatusBadRequest, map[string]string{"error": "body is required and must be a non-empty string"})
			return
		}
		layer := "semantic"
		if v, ok := data["layer"].(string); ok && v != "" {
			layer = v
		}
		topic := ""
		if v, ok := data["topic"].(string); ok {
			topic = v
		}
		subject := ""
		if v, ok := data["subject"].(string); ok {
			subject = v
		}
		confidence := 1.0
		if v, ok := data["confidence"].(float64); ok {
			confidence = v
		}
		factType := "fact"
		if v, ok := data["fact_type"].(string); ok && v != "" {
			factType = v
		}
		// Always a JSON object: the steward SQL assumes one.
		metadata := metadataObject(data["metadata"])

		// Steward model: only the steward writes trusted nodes directly.
		// Anyone else lands in the inbox as "proposed".
		identity, status := resolveWriter(r, data)

		id := uuid.New().String()
		nowStr := now()
		_, err := pool.Exec(context.Background(),
			`INSERT INTO memory_nodes (id, body, hash, layer, fact_type, subject, confidence, metadata, topic, captured_at, created_at, updated_at, source, status)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			id, body, hashBody(body), layer, factType, subject, confidence, metadata, topic, nowStr, nowStr, nowStr, identity, status)
		if err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}

		node := Node{
			ID: id, Body: body, Layer: layer, Topic: topic, Subject: subject,
			Confidence: confidence, FactType: factType, Metadata: metadata,
			CapturedAt: nowStr, CreatedAt: nowStr, UpdatedAt: nowStr,
			Source: identity, Status: status,
		}
		code := http.StatusCreated
		if status == statusProposed {
			code = http.StatusAccepted
		}
		jsonResp(w, code, node)

	case "DELETE":
		if !bulkDeleteAllowed(r) {
			jsonResp(w, http.StatusForbidden, map[string]string{"error": "bulk deletion disabled; set MEM_ENABLE_BULK_DELETE=true and send X-Confirm-Delete: all"})
			return
		}
		_, err := pool.Exec(context.Background(), "DELETE FROM memory_nodes")
		if err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		jsonResp(w, 200, map[string]string{"status": "deleted_all"})
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func handleNode(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/memory/nodes/")
	id = strings.TrimPrefix(id, "/api/nodes/")

	// Steward decisions ride on the node path: /nodes/<id>/promote|reject
	switch {
	case strings.HasSuffix(id, "/promote"):
		handleNodeDecision(w, r, "promote")
		return
	case strings.HasSuffix(id, "/reject"):
		handleNodeDecision(w, r, "reject")
		return
	}

	if id == "" {
		jsonResp(w, 400, map[string]string{"error": "missing id"})
		return
	}

	switch r.Method {
	case "GET":
		var n Node
		var captured, created, updated interface{}
		err := pool.QueryRow(context.Background(),
			`SELECT id, body, layer, topic, subject, confidence, fact_type, metadata, captured_at, created_at, updated_at, source, status, COALESCE(fact_key, '')
			 FROM memory_nodes WHERE id = $1`, id).
			Scan(&n.ID, &n.Body, &n.Layer, &n.Topic, &n.Subject, &n.Confidence, &n.FactType, &n.Metadata, &captured, &created, &updated, &n.Source, &n.Status, &n.FactKey)
		if err != nil {
			jsonResp(w, 404, map[string]string{"error": "not found"})
			return
		}
		n.CapturedAt = fmt.Sprint(captured)
		n.CreatedAt = fmt.Sprint(created)
		n.UpdatedAt = fmt.Sprint(updated)
		jsonResp(w, 200, n)

	case "DELETE":
		var src string
		if err := pool.QueryRow(context.Background(), `SELECT source FROM memory_nodes WHERE id = $1`, id).Scan(&src); err == nil && src == sourceSystem {
			jsonResp(w, http.StatusForbidden, map[string]string{"error": "system facts are owned by the live-system refresher; they are retired, not deleted"})
			return
		}
		_, err := pool.Exec(context.Background(), "DELETE FROM memory_nodes WHERE id = $1", id)
		if err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		jsonResp(w, 200, map[string]string{"status": "deleted", "id": id})
	default:
		w.Header().Set("Allow", "GET, DELETE")
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func handleEdges(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		rows, err := pool.Query(context.Background(),
			`SELECT id, src, dst, rel, weight, created_at FROM memory_edges ORDER BY created_at DESC`)
		if err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		edges := []Edge{}
		for rows.Next() {
			var e Edge
			var created interface{}
			if err := rows.Scan(&e.ID, &e.Src, &e.Dst, &e.Rel, &e.Weight, &created); err != nil {
				jsonResp(w, 500, map[string]string{"error": err.Error()})
				return
			}
			e.CreatedAt = fmt.Sprint(created)
			edges = append(edges, e)
		}
		jsonResp(w, 200, map[string]interface{}{"edges": edges, "count": len(edges)})

	case "POST":
		var data map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			jsonResp(w, 400, map[string]string{"error": err.Error()})
			return
		}
		edgesRaw, ok := data["edges"].([]interface{})
		if !ok {
			edgesRaw = []interface{}{data}
		}
		nowStr := now()
		count := 0
		for _, e := range edgesRaw {
			em, _ := e.(map[string]interface{})
			src := fmt.Sprint(em["src"])
			dst := fmt.Sprint(em["dst"])
			rel := "related"
			if v, ok := em["rel"].(string); ok && v != "" {
				rel = v
			}
			if v, ok := em["kind"].(string); ok && v != "" {
				rel = v
			}
			weight := 1.0
			if v, ok := em["weight"].(float64); ok {
				weight = v
			}
			_, err := pool.Exec(context.Background(),
				`INSERT INTO memory_edges (src, dst, rel, weight, created_at) VALUES ($1, $2, $3, $4, $5)`,
				src, dst, rel, weight, nowStr)
			if err == nil {
				count++
			}
		}
		jsonResp(w, 200, map[string]interface{}{"status": "ok", "count": count})

	case "DELETE":
		if !bulkDeleteAllowed(r) {
			jsonResp(w, http.StatusForbidden, map[string]string{"error": "bulk deletion disabled; set MEM_ENABLE_BULK_DELETE=true and send X-Confirm-Delete: all"})
			return
		}
		_, err := pool.Exec(context.Background(), "DELETE FROM memory_edges")
		if err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		jsonResp(w, 200, map[string]string{"status": "deleted_all_edges"})
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
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
		q = fmt.Sprint(data["query"])
	}
	limit := 10
	rows, err := pool.Query(context.Background(),
		`SELECT id, body, layer, topic, subject, confidence, fact_type, metadata, created_at, updated_at, source, status, COALESCE(fact_key, '')
		 FROM memory_nodes WHERE body ILIKE $1 AND status = $3 ORDER BY created_at DESC LIMIT $2`,
		"%"+q+"%", limit, statusActive)
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	nodes := []Node{}
	for rows.Next() {
		var n Node
		var created, updated interface{}
		if err := rows.Scan(&n.ID, &n.Body, &n.Layer, &n.Topic, &n.Subject, &n.Confidence, &n.FactType, &n.Metadata, &created, &updated, &n.Source, &n.Status, &n.FactKey); err != nil {
			continue
		}
		n.CreatedAt = fmt.Sprint(created)
		n.UpdatedAt = fmt.Sprint(updated)
		nodes = append(nodes, n)
	}
	jsonResp(w, 200, map[string]interface{}{"results": nodes, "count": len(nodes)})
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var nodes, edges int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM memory_nodes").Scan(&nodes); err != nil {
		jsonResp(w, http.StatusServiceUnavailable, map[string]string{"error": "unable to read memory count"})
		return
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM memory_edges").Scan(&edges); err != nil {
		jsonResp(w, http.StatusServiceUnavailable, map[string]string{"error": "unable to read relationship count"})
		return
	}
	rows, err := pool.Query(ctx, "SELECT layer, COUNT(*) FROM memory_nodes GROUP BY layer")
	if err != nil {
		jsonResp(w, http.StatusServiceUnavailable, map[string]string{"error": "unable to read layer counts"})
		return
	}
	defer rows.Close()
	layers := map[string]int{}
	for rows.Next() {
		var layer string
		var count int
		if err := rows.Scan(&layer, &count); err != nil {
			jsonResp(w, http.StatusInternalServerError, map[string]string{"error": "unable to read layer row"})
			return
		}
		layers[layer] = count
	}
	if err := rows.Err(); err != nil {
		jsonResp(w, http.StatusServiceUnavailable, map[string]string{"error": "unable to finish reading layer counts"})
		return
	}

	// Steward visibility: how much is waiting in the inbox.
	statusCounts := map[string]int{}
	statusRows, err := pool.Query(ctx, "SELECT status, COUNT(*) FROM memory_nodes GROUP BY status")
	if err == nil {
		for statusRows.Next() {
			var status string
			var count int
			if statusRows.Scan(&status, &count) == nil {
				statusCounts[status] = count
			}
		}
		statusRows.Close()
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"nodes": nodes, "edges": edges, "layers": layers, "status": statusCounts,
	})
}

func handleContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var query string
	var limit int = 5
	if r.Method == http.MethodGet {
		query = r.URL.Query().Get("q")
		if l := r.URL.Query().Get("limit"); l != "" {
			fmt.Sscanf(l, "%d", &limit)
		}
	} else {
		var data map[string]interface{}
		json.NewDecoder(r.Body).Decode(&data)
		query = fmt.Sprint(data["query"])
		if l, ok := data["limit"].(float64); ok {
			limit = int(l)
		}
	}
	rows, err := pool.Query(context.Background(),
		`SELECT body FROM memory_nodes WHERE body ILIKE $1 AND status = $3 ORDER BY created_at DESC LIMIT $2`,
		"%"+query+"%", limit, statusActive)
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	var bodies []string
	for rows.Next() {
		var b string
		rows.Scan(&b)
		bodies = append(bodies, b)
	}
	ctx := strings.Join(bodies, "\n---\n")
	jsonResp(w, 200, map[string]interface{}{"context": ctx})
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
	id := "mem_" + strings.ReplaceAll(uuid.New().String()[:12], "-", "")
	layer := "semantic"
	if l, ok := data["layer"].(string); ok && l != "" {
		layer = l
	}
	topic := ""
	if t, ok := data["topic"].(string); ok {
		topic = t
	}
	subject := ""
	if s, ok := data["subject"].(string); ok {
		subject = s
	}
	// Extraction is still a write, so it goes through the same identity gate as
	// every other write path: only the steward's extracted nodes are trusted.
	identity, status := resolveWriter(r, data)
	nowStr := now()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO memory_nodes (id, body, hash, layer, fact_type, subject, confidence, metadata, topic, captured_at, created_at, updated_at, source, status)
		 VALUES ($1, $2, $3, $4, 'fact', $5, 1.0, '{}', $6, $7, $7, $7, $8, $9)`,
		id, text, hashBody(text), layer, subject, topic, nowStr, identity, status)
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResp(w, 201, map[string]interface{}{
		"extracted": 1,
		"status":    status,
		"source":    identity,
		"nodes":     []map[string]string{{"id": id, "body": text}},
	})
}

func handleRebuildEdges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !requireSteward(w, r) {
		return
	}
	_, _ = pool.Exec(context.Background(), "DELETE FROM memory_edges WHERE rel IN ('same_topic', 'same_subject')")

	count := 0
	rows, err := pool.Query(context.Background(),
		`SELECT a.id, b.id FROM memory_nodes a JOIN memory_nodes b ON a.topic = b.topic AND a.topic != '' AND a.id < b.id`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var src, dst string
			rows.Scan(&src, &dst)
			pool.Exec(context.Background(), "INSERT INTO memory_edges (src, dst, rel, weight, created_at) VALUES ($1, $2, 'same_topic', 1.0, $3)", src, dst, now())
			count++
		}
	}
	rows2, err2 := pool.Query(context.Background(),
		`SELECT a.id, b.id FROM memory_nodes a JOIN memory_nodes b ON a.subject = b.subject AND a.subject != '' AND a.id < b.id`)
	if err2 == nil {
		defer rows2.Close()
		for rows2.Next() {
			var src, dst string
			rows2.Scan(&src, &dst)
			pool.Exec(context.Background(), "INSERT INTO memory_edges (src, dst, rel, weight, created_at) VALUES ($1, $2, 'same_subject', 1.0, $3)", src, dst, now())
			count++
		}
	}
	jsonResp(w, 200, map[string]interface{}{"status": "ok", "edges_created": count})
}

// loadServerConfig reads the listen address and credentials from the environment
// and refuses to start in a configuration that would expose the API by accident.
func loadServerConfig() (string, authConfig) {
	listenAddr := strings.TrimSpace(os.Getenv("MEM_LISTEN_ADDR"))
	if listenAddr == "" {
		listenAddr = defaultListenAddr()
	}
	cfg := authConfig{
		APIToken:       strings.TrimSpace(os.Getenv("MEM_API_TOKEN")),
		StewardToken:   strings.TrimSpace(os.Getenv("MEM_STEWARD_TOKEN")),
		UIUser:         strings.TrimSpace(os.Getenv("MEM_UI_USER")),
		UIPass:         strings.TrimSpace(os.Getenv("MEM_UI_PASSWORD")),
		AllowedOrigins: splitList(os.Getenv("MEM_ALLOWED_ORIGINS")),
	}
	if cfg.UIUser == "" {
		cfg.UIUser = "admin"
	}
	if cfg.StewardToken != "" && cfg.StewardToken == cfg.APIToken {
		log.Fatal("MEM_STEWARD_TOKEN must differ from MEM_API_TOKEN")
	}
	if cfg.APIToken == "" && cfg.StewardToken == "" && !isLoopbackListenAddress(listenAddr) {
		log.Fatal("MEM_API_TOKEN (or MEM_STEWARD_TOKEN) is required when listening on a non-loopback address")
	}
	if !cfg.enabled() {
		// A reverse proxy or socket proxy in front of a loopback listener makes
		// every remote client look local, so running open must be a deliberate choice.
		if os.Getenv("MEM_ALLOW_UNAUTHENTICATED") != "true" {
			log.Fatal("no authentication configured: set MEM_API_TOKEN (and MEM_UI_PASSWORD for the web UI), or set MEM_ALLOW_UNAUTHENTICATED=true for local development only")
		}
		log.Printf("WARNING: running without authentication (MEM_ALLOW_UNAUTHENTICATED=true); never put a proxy in front of this mode")
	}
	if !cfg.strictStewardMode() {
		log.Printf("WARNING: MEM_STEWARD_TOKEN is unset; writer identity is self-declared via X-Mem-Source, so any API client can claim to be the steward")
	}
	return listenAddr, cfg
}

func handleUI(w http.ResponseWriter, r *http.Request) {
	page := dashboardHTML
	if r.URL.Path == "/graph" {
		page = graphHTML
	}
	nonce := newNonce()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", contentSecurityPolicy(nonce))
	w.Header().Set("Cache-Control", "no-store")
	io.WriteString(w, strings.ReplaceAll(page, "__CSP_NONCE__", nonce))
}

func bulkDeleteAllowed(r *http.Request) bool {
	return os.Getenv("MEM_ENABLE_BULK_DELETE") == "true" && r.Header.Get("X-Confirm-Delete") == "all"
}

func isLoopbackListenAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func main() {
	// Parse CLI arguments
	osArgs := os.Args[1:]
	if len(osArgs) == 0 {
		osArgs = []string{"help"}
	}
	cmd := osArgs[0]
	rest := osArgs[1:]

	var showHelp bool
	var backupFile string
	var restoreFile string
	var fileFlag string
	var useGzip bool

	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.BoolVar(&showHelp, "help", false, "show help")
	fs.BoolVar(&showHelp, "h", false, "show help (shorthand)")
	fs.StringVar(&backupFile, "backup", "", "backup database to file (JSON)")
	fs.StringVar(&restoreFile, "restore", "", "restore database from file (JSON or .gz)")
	fs.StringVar(&fileFlag, "file", "", "file path for backup/restore (use - for stdin/stdout)")
	fs.BoolVar(&useGzip, "gzip", false, "use gzip compression for backup")
	if err := fs.Parse(rest); err != nil {
		fmt.Println(err)
		return
	}
	extra := fs.Args()

	// Handle help
	if showHelp || cmd == "help" {
		fmt.Print(`mem - Memory Service CLI

Usage:
  mem server              Start the HTTP server
  mem backup --file FILE  Export database to file (use - for stdout)
  mem restore --file FILE Import database from file (use - for stdin)
  mem help                Show this help

Environment variables:
  DATABASE_URL          PostgreSQL connection string (required)
  MEM_API_TOKEN         Bearer token for agents; their writes land in the inbox
  MEM_STEWARD_TOKEN     Bearer token for the steward: active writes, inbox review, restore
  MEM_LISTEN_ADDR       Bind address (default: 127.0.0.1:3200)
  MEM_UI_USER           Web UI basic auth username (default: admin)
  MEM_UI_PASSWORD       Web UI basic auth password (signs in as steward)
  MEM_ALLOWED_ORIGINS   Extra browser origins allowed to write (reverse proxies that rewrite Host)
  MEM_ALLOW_UNAUTHENTICATED=true  Allow running without any credential (local development only)

Examples:
  mem server                    # Start the HTTP server
  mem backup --file backup.json              # Export to JSON
  mem backup --file backup.json.gz --gzip      # Export to gzipped JSON
  mem restore --file backup.json             # Import from JSON
  mem backup | ssh remote mem restore --file -   # Remote backup
`)
		return
	}

	if len(extra) > 0 {
		// ignore unexpected positional args
	}
	if cmd == "server" {
		// Validate the listen address and credentials before touching the database.
		listenAddr, authCfg := loadServerConfig()
		initDB()
		defer pool.Close()

		mux := http.NewServeMux()

		// Health
		mux.HandleFunc("/health", handleHealth)

		// API aliases (memoh-style: /api/nodes, /api/edges)
		mux.HandleFunc("/api/nodes", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				handleNodesList(w, r)
			} else {
				handleNodes(w, r)
			}
		})
		mux.HandleFunc("/api/edges", func(w http.ResponseWriter, r *http.Request) {
			handleEdges(w, r)
		})

		// v1 API
		mux.HandleFunc("/v1/memory/nodes", handleNodes)
		mux.HandleFunc("/v1/memory/nodes/", handleNode)
		mux.HandleFunc("/v1/memory/edges", handleEdges)
		mux.HandleFunc("/v1/memory/search", handleSearch)
		mux.HandleFunc("/v1/memory/stats", handleStats)
		mux.HandleFunc("/v1/memory/context", handleContext)
		mux.HandleFunc("/v1/memory/extract", handleExtract)
		mux.HandleFunc("/v1/memory/rebuild-edges", handleRebuildEdges)
		mux.HandleFunc("/v1/memory/backup", handleBackup)
		mux.HandleFunc("/v1/memory/restore", handleRestore)
		mux.HandleFunc("/v1/memory/brief", handleBrief)
		mux.HandleFunc("/v1/memory/inbox", handleInbox)
		mux.HandleFunc("/api/brief", handleBrief)
		mux.HandleFunc("/api/inbox", handleInbox)
		mux.HandleFunc("/api/nodes/", handleNode)

		// UI
		mux.HandleFunc("/graph", handleUI)
		mux.HandleFunc("/", handleUI)

		server := &http.Server{
			Addr: listenAddr, Handler: apiAuthMiddleware(authCfg, mux),
			ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
			WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second,
		}

		// Live-system facts are re-read and swept in the background.
		stopRefresher := make(chan struct{})
		go startFactRefresher(stopRefresher)

		// Shut down cleanly so the refresher stops instead of writing mid-exit.
		ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stopSignals()
		go func() {
			<-ctx.Done()
			close(stopRefresher)
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				log.Printf("shutdown: %v", err)
			}
		}()

		log.Printf("mem service starting on %s", listenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
		log.Printf("mem service stopped")
		return
	}

	// All other commands need database connection
	initDB()
	defer pool.Close()

	switch cmd {
	case "backup":
		fn := fileFlag
		if fn == "" {
			fn = backupFile
		}
		runCLIBackup(fn, useGzip)
	case "restore":
		fn := fileFlag
		if fn == "" {
			fn = restoreFile
		}
		runCLIRestore(fn)
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		flag.Usage()
	}
}

func handleNodesList(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	query := `SELECT id, body, layer, topic, subject, confidence, fact_type, metadata, captured_at, created_at, updated_at, source, status, COALESCE(fact_key, '') FROM memory_nodes`
	args := []interface{}{}
	switch status {
	case "", statusActive:
		query += " WHERE status = $1"
		args = append(args, statusActive)
	case "all":
	default:
		query += " WHERE status = $1"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC"

	rows, err := pool.Query(context.Background(), query, args...)
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	type nodeResp struct {
		ID         string  `json:"id"`
		Body       string  `json:"body"`
		Layer      string  `json:"layer"`
		Topic      string  `json:"topic"`
		Subject    string  `json:"subject"`
		Confidence float64 `json:"confidence"`
		Source     string  `json:"source"`
		Status     string  `json:"status"`
		FactKey    string  `json:"fact_key,omitempty"`
		CreatedAt  string  `json:"created_at"`
		UpdatedAt  string  `json:"updated_at"`
	}
	nodes := []nodeResp{}
	for rows.Next() {
		var n nodeResp
		var captured, created, updated interface{}
		var factType, metadata string
		if err := rows.Scan(&n.ID, &n.Body, &n.Layer, &n.Topic, &n.Subject, &n.Confidence, &factType, &metadata, &captured, &created, &updated, &n.Source, &n.Status, &n.FactKey); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		n.CreatedAt = fmt.Sprint(created)
		n.UpdatedAt = fmt.Sprint(updated)
		nodes = append(nodes, n)
	}
	jsonResp(w, 200, nodes)
}

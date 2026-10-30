package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var pool *pgxpool.Pool

const port = "3200"

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
		// Support filters: limit, layer, topic, subject
		q := r.URL.Query()
		limit := 100
		if l := q.Get("limit"); l != "" {
			fmt.Sscanf(l, "%d", &limit)
		}
		if limit > 5000 {
			limit = 5000
		}
		layer := q.Get("layer")
		topic := q.Get("topic")
		subject := q.Get("subject")

		query := `SELECT id, body, layer, topic, subject, confidence, fact_type, metadata, captured_at, created_at, updated_at FROM memory_nodes WHERE 1=1`
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
			if err := rows.Scan(&n.ID, &n.Body, &n.Layer, &n.Topic, &n.Subject, &n.Confidence, &n.FactType, &n.Metadata, &captured, &created, &updated); err != nil {
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
		body := fmt.Sprint(data["body"])
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
		metadata := "{}"
		if v, ok := data["metadata"]; ok {
			b, _ := json.Marshal(v)
			metadata = string(b)
		}

		id := uuid.New().String()
		nowStr := now()
		_, err := pool.Exec(context.Background(),
			`INSERT INTO memory_nodes (id, body, hash, layer, fact_type, subject, confidence, metadata, topic, captured_at, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			id, body, hashBody(body), layer, factType, subject, confidence, metadata, topic, nowStr, nowStr, nowStr)
		if err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}

		node := Node{
			ID: id, Body: body, Layer: layer, Topic: topic, Subject: subject,
			Confidence: confidence, FactType: factType, Metadata: metadata,
			CapturedAt: nowStr, CreatedAt: nowStr, UpdatedAt: nowStr,
		}
		jsonResp(w, 201, node)

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
	if id == "" {
		jsonResp(w, 400, map[string]string{"error": "missing id"})
		return
	}

	switch r.Method {
	case "GET":
		var n Node
		var captured, created, updated interface{}
		err := pool.QueryRow(context.Background(),
			`SELECT id, body, layer, topic, subject, confidence, fact_type, metadata, captured_at, created_at, updated_at
			 FROM memory_nodes WHERE id = $1`, id).
			Scan(&n.ID, &n.Body, &n.Layer, &n.Topic, &n.Subject, &n.Confidence, &n.FactType, &n.Metadata, &captured, &created, &updated)
		if err != nil {
			jsonResp(w, 404, map[string]string{"error": "not found"})
			return
		}
		n.CapturedAt = fmt.Sprint(captured)
		n.CreatedAt = fmt.Sprint(created)
		n.UpdatedAt = fmt.Sprint(updated)
		jsonResp(w, 200, n)

	case "DELETE":
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
	if q == "" && r.Method == "POST" {
		var data map[string]interface{}
		json.NewDecoder(r.Body).Decode(&data)
		q = fmt.Sprint(data["query"])
	}
	limit := 10
	rows, err := pool.Query(context.Background(),
		`SELECT id, body, layer, topic, subject, confidence, fact_type, metadata, created_at, updated_at
		 FROM memory_nodes WHERE body ILIKE $1 ORDER BY created_at DESC LIMIT $2`,
		"%"+q+"%", limit)
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	nodes := []Node{}
	for rows.Next() {
		var n Node
		var created, updated interface{}
		if err := rows.Scan(&n.ID, &n.Body, &n.Layer, &n.Topic, &n.Subject, &n.Confidence, &n.FactType, &n.Metadata, &created, &updated); err != nil {
			continue
		}
		n.CreatedAt = fmt.Sprint(created)
		n.UpdatedAt = fmt.Sprint(updated)
		nodes = append(nodes, n)
	}
	jsonResp(w, 200, map[string]interface{}{"results": nodes, "count": len(nodes)})
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	var nodes, edges int
	pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM memory_nodes").Scan(&nodes)
	pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM memory_edges").Scan(&edges)

	rows, _ := pool.Query(context.Background(), "SELECT layer, COUNT(*) FROM memory_nodes GROUP BY layer")
	defer rows.Close()
	layers := map[string]int{}
	for rows.Next() {
		var l string
		var c int
		rows.Scan(&l, &c)
		layers[l] = c
	}

	jsonResp(w, 200, map[string]interface{}{
		"nodes":  nodes,
		"edges":  edges,
		"layers": layers,
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
	if r.Method == "GET" {
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
		`SELECT body FROM memory_nodes WHERE body ILIKE $1 ORDER BY created_at DESC LIMIT $2`,
		"%"+query+"%", limit)
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
	text := fmt.Sprint(data["text"])
	if text == "" || text == "<nil>" {
		jsonResp(w, 400, map[string]string{"error": "text required"})
		return
	}
	id := "mem_" + strings.ReplaceAll(uuid.New().String()[:12], "-", "")
	layer := "semantic"
	if l, ok := data["layer"].(string); ok && l != "" {
		layer = l
	}
	nowStr := now()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO memory_nodes (id, body, hash, layer, fact_type, confidence, metadata, captured_at, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 'fact', 1.0, '{}', $5, $5, $5)`,
		id, text, hashBody(text), layer, nowStr)
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResp(w, 201, map[string]interface{}{"extracted": 1, "nodes": []map[string]string{{"id": id, "body": text}}})
}

func handleRebuildEdges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
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
func handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/graph" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(graphHTML))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML))
}


func bulkDeleteAllowed(r *http.Request) bool {
	return os.Getenv("MEM_ENABLE_BULK_DELETE") == "true" && r.Header.Get("X-Confirm-Delete") == "all"
}
func isLoopbackListenAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil { return false }
	if strings.EqualFold(host, "localhost") { return true }
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func apiAuthMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isAPI := strings.HasPrefix(r.URL.Path, "/v1/memory/") ||
			strings.HasPrefix(r.URL.Path, "/api/nodes") || strings.HasPrefix(r.URL.Path, "/api/edges")
		if isAPI && token != "" {
			provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if provided == "" || len(provided) != len(token) || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				jsonResp(w, http.StatusUnauthorized, map[string]string{"error": "valid bearer token required"})
				return
			}
		}
		if isAPI && r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func main() {
	initDB()
	defer pool.Close()

	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("/health", handleHealth)

	// API aliases (memoh-style: /api/nodes, /api/edges)
	mux.HandleFunc("/api/nodes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
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

	// UI
	mux.HandleFunc("/graph", handleUI)
	mux.HandleFunc("/", handleUI)

	listenAddr := strings.TrimSpace(os.Getenv("MEM_LISTEN_ADDR"))
	if listenAddr == "" { listenAddr = "127.0.0.1:" + port }
	apiToken := strings.TrimSpace(os.Getenv("MEM_API_TOKEN"))
	if apiToken == "" && !isLoopbackListenAddress(listenAddr) {
		log.Fatal("MEM_API_TOKEN is required when listening on a non-loopback address")
	}
	if apiToken == "" { log.Printf("WARNING: MEM_API_TOKEN is unset; API is unauthenticated but bound to loopback only") }
	server := &http.Server{
		Addr: listenAddr, Handler: apiAuthMiddleware(apiToken, mux),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}
	log.Printf("mem service starting on %s", listenAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed { log.Fatal(err) }
}

func handleNodesList(w http.ResponseWriter, r *http.Request) {
	rows, err := pool.Query(context.Background(),
		`SELECT id, body, layer, topic, subject, confidence, fact_type, metadata, captured_at, created_at, updated_at
		 FROM memory_nodes ORDER BY created_at DESC`)
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
		CreatedAt  string  `json:"created_at"`
		UpdatedAt  string  `json:"updated_at"`
	}
	nodes := []nodeResp{}
	for rows.Next() {
		var n nodeResp
		var captured, created, updated interface{}
		var factType, metadata string
		if err := rows.Scan(&n.ID, &n.Body, &n.Layer, &n.Topic, &n.Subject, &n.Confidence, &factType, &metadata, &captured, &created, &updated); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		n.CreatedAt = fmt.Sprint(created)
		n.UpdatedAt = fmt.Sprint(updated)
		nodes = append(nodes, n)
	}
	jsonResp(w, 200, nodes)
}



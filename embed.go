package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Optional semantic layer.
//
// mem works without any model: search falls back to ILIKE and every write is
// stored as-is. When MEM_EMBED_URL and MEM_EMBED_MODEL are configured, the
// service also maintains a pgvector column and ranks search results by cosine
// distance. Embeddings are computed by a background indexer so request latency
// and availability never depend on the model provider.

const defaultEmbedDim = 1024

type embedConfig struct {
	Enabled  bool
	BaseURL  string
	Model    string
	APIKey   string
	Dim      int
	Interval time.Duration
	Batch    int
}

var (
	embedCfg    embedConfig
	vectorReady bool
	embedClient = &http.Client{Timeout: 60 * time.Second}
)

// loadEmbedConfig reads the optional semantic-search settings from the
// environment. Semantic search stays off unless both a base URL and a model are
// present, so an unconfigured deployment behaves exactly as before.
func loadEmbedConfig() embedConfig {
	c := embedConfig{
		BaseURL:  strings.TrimRight(strings.TrimSpace(os.Getenv("MEM_EMBED_URL")), "/"),
		Model:    strings.TrimSpace(os.Getenv("MEM_EMBED_MODEL")),
		APIKey:   strings.TrimSpace(os.Getenv("MEM_EMBED_API_KEY")),
		Dim:      defaultEmbedDim,
		Interval: 60 * time.Second,
		Batch:    16,
	}
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("MEM_EMBED_DIM"))); err == nil && v > 0 && v <= 4096 {
		c.Dim = v
	}
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("MEM_EMBED_INTERVAL_SECONDS"))); err == nil && v >= 5 {
		c.Interval = time.Duration(v) * time.Second
	}
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("MEM_EMBED_BATCH"))); err == nil && v > 0 && v <= 128 {
		c.Batch = v
	}
	c.Enabled = c.BaseURL != "" && c.Model != ""
	return c
}

// initEmbeddings prepares the vector column and index. Any failure degrades to
// keyword search instead of blocking startup.
func initEmbeddings() {
	embedCfg = loadEmbedConfig()
	if !embedCfg.Enabled {
		log.Printf("semantic search disabled (set MEM_EMBED_URL and MEM_EMBED_MODEL to enable)")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		log.Printf("WARNING: pgvector unavailable (%v); semantic search disabled, keyword search stays active", err)
		embedCfg.Enabled = false
		return
	}
	ddl := fmt.Sprintf("ALTER TABLE memory_nodes ADD COLUMN IF NOT EXISTS embedding vector(%d)", embedCfg.Dim)
	if _, err := pool.Exec(ctx, ddl); err != nil {
		log.Printf("WARNING: could not prepare embedding column (%v); semantic search disabled", err)
		embedCfg.Enabled = false
		return
	}
	// The HNSW index is an optimisation; a large table may take a while to
	// build, so a failure here only costs speed.
	if _, err := pool.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_nodes_embedding ON memory_nodes USING hnsw (embedding vector_cosine_ops)"); err != nil {
		log.Printf("WARNING: could not create embedding index (%v); vector search will be slower", err)
	}
	vectorReady = true
	log.Printf("semantic search enabled (model=%s dim=%d)", embedCfg.Model, embedCfg.Dim)
}

// embedTexts sends a batch to an OpenAI-compatible /embeddings endpoint.
func embedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	if !embedCfg.Enabled {
		return nil, fmt.Errorf("semantic search is not configured")
	}
	payload, err := json.Marshal(map[string]interface{}{"model": embedCfg.Model, "input": texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, embedCfg.BaseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if embedCfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+embedCfg.APIKey)
	}
	resp, err := embedClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding endpoint returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body))[:min(len(body), 200)])
	}
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("embedding endpoint returned %d vectors for %d inputs", len(parsed.Data), len(texts))
	}
	out := make([][]float32, len(parsed.Data))
	for i, d := range parsed.Data {
		if len(d.Embedding) != embedCfg.Dim {
			return nil, fmt.Errorf("embedding dimension %d does not match MEM_EMBED_DIM=%d", len(d.Embedding), embedCfg.Dim)
		}
		out[i] = d.Embedding
	}
	return out, nil
}

// vectorLiteral renders a vector for a text-cast parameter, so the service
// needs no pgvector client dependency.
func vectorLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// startEmbeddingIndexer fills in embeddings for nodes that do not have one yet.
// It never runs in the request path.
func startEmbeddingIndexer(stop <-chan struct{}) {
	if !embedCfg.Enabled {
		return
	}
	// Drain the backlog in one go: a full batch means there is more waiting, so
	// keep going instead of waiting a whole interval per batch.
	drain := func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			n, err := indexPendingEmbeddings(context.Background())
			if err != nil {
				log.Printf("embedding indexer: %v", err)
				return
			}
			if n == 0 {
				return
			}
		}
	}
	drain()
	ticker := time.NewTicker(embedCfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			drain()
		}
	}
}

func indexPendingEmbeddings(ctx context.Context) (int, error) {
	if !vectorReady {
		return 0, nil
	}
	rows, err := pool.Query(ctx,
		`SELECT id, body FROM memory_nodes WHERE embedding IS NULL AND source <> 'system' ORDER BY created_at LIMIT $1`, embedCfg.Batch)
	if err != nil {
		return 0, err
	}
	var ids, bodies []string
	for rows.Next() {
		var id, body string
		if err := rows.Scan(&id, &body); err != nil {
			continue
		}
		ids = append(ids, id)
		bodies = append(bodies, body)
	}
	rows.Close()
	if len(ids) == 0 {
		return 0, nil
	}
	vectors, err := embedTexts(ctx, bodies)
	if err != nil {
		return 0, err
	}
	stored := 0
	for i, id := range ids {
		if _, err := pool.Exec(ctx,
			`UPDATE memory_nodes SET embedding = $1::vector WHERE id = $2`, vectorLiteral(vectors[i]), id); err != nil {
			return stored, err
		}
		stored++
	}
	return stored, nil
}

// semanticIDs ranks active nodes by cosine distance. A nil result means the
// semantic layer is unavailable and the caller should use keyword search only.
func semanticIDs(ctx context.Context, query string, limit int) []string {
	if !vectorReady || strings.TrimSpace(query) == "" {
		return nil
	}
	vectors, err := embedTexts(ctx, []string{query})
	if err != nil {
		log.Printf("semantic search falling back to keyword search: %v", err)
		return nil
	}
	rows, err := pool.Query(ctx,
		`SELECT id FROM memory_nodes WHERE status = $2 AND embedding IS NOT NULL AND source <> 'system'
		 ORDER BY embedding <=> $1::vector LIMIT $3`,
		vectorLiteral(vectors[0]), statusActive, limit)
	if err != nil {
		log.Printf("semantic search query failed, using keyword search: %v", err)
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

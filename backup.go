package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Backup and restore. The HTTP endpoints and the CLI share these functions so the
// two paths cannot drift apart again: both export every column (including source,
// status and fact_key) and both restore inside a single transaction.

const (
	backupVersion = "1.0"
	// maxRestoreBytes caps the decompressed size of a backup, so a small gzip
	// payload cannot expand into an unbounded amount of memory.
	maxRestoreBytes = 512 << 20
)

// restoredNodeColumns is the COPY column order that nodeRowsFromBackup builds.
var restoredNodeColumns = []string{
	"id", "body", "hash", "layer", "fact_type", "subject", "confidence",
	"metadata", "topic", "captured_at", "created_at", "updated_at",
	"source", "status", "fact_key",
}

// nodeRowsFromBackup flattens backup nodes for COPY. Backups written before the
// steward columns existed have no source/status: default them so a restore never
// resurrects rows into an unknown state.
func nodeRowsFromBackup(nodes []Node) [][]any {
	rows := make([][]any, 0, len(nodes))
	for _, n := range nodes {
		source := n.Source
		if source == "" {
			source = "agent"
		}
		status := n.Status
		if status == "" {
			status = statusActive
		}
		var factKey any
		if n.FactKey != "" {
			factKey = n.FactKey
		}
		rows = append(rows, []any{
			n.ID, n.Body, hashBody(n.Body), n.Layer, n.FactType, n.Subject, n.Confidence,
			normalizeMetadataText(n.Metadata), n.Topic, n.CapturedAt, n.CreatedAt, n.UpdatedAt,
			source, status, factKey,
		})
	}
	return rows
}

// normalizeMetadataText guarantees the stored metadata is a JSON object. The
// steward SQL does metadata::jsonb and jsonb_set(...), which fail on anything
// else, so one malformed row would otherwise stall the whole refresher.
func normalizeMetadataText(s string) string {
	if strings.TrimSpace(s) == "" {
		return "{}"
	}
	var obj map[string]any
	if json.Unmarshal([]byte(s), &obj) == nil && obj != nil {
		return s
	}
	b, _ := json.Marshal(map[string]any{"legacy_metadata": s})
	return string(b)
}

// metadataObject converts a client-supplied metadata value into a JSON object.
func metadataObject(v any) string {
	switch m := v.(type) {
	case nil:
		return "{}"
	case map[string]any:
		b, _ := json.Marshal(m)
		return string(b)
	default:
		b, _ := json.Marshal(map[string]any{"value": m})
		return string(b)
	}
}

// exportBackup reads every node and edge from one consistent snapshot. Any scan or
// iteration error aborts the export: a backup must never be silently truncated.
func exportBackup(ctx context.Context) (Backup, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Backup{}, fmt.Errorf("begin snapshot: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read-only snapshot, nothing to undo

	nodes := []Node{}
	rows, err := tx.Query(ctx, `SELECT id, body, layer, COALESCE(topic, ''), COALESCE(subject, ''), confidence, fact_type,
		COALESCE(metadata, '{}'), captured_at, created_at, updated_at, source, status, COALESCE(fact_key, '')
		FROM memory_nodes ORDER BY created_at DESC, id`)
	if err != nil {
		return Backup{}, fmt.Errorf("query nodes: %w", err)
	}
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.Body, &n.Layer, &n.Topic, &n.Subject, &n.Confidence, &n.FactType,
			&n.Metadata, &n.CapturedAt, &n.CreatedAt, &n.UpdatedAt, &n.Source, &n.Status, &n.FactKey); err != nil {
			rows.Close()
			return Backup{}, fmt.Errorf("scan node: %w", err)
		}
		nodes = append(nodes, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Backup{}, fmt.Errorf("read nodes: %w", err)
	}

	edges := []Edge{}
	erows, err := tx.Query(ctx, `SELECT id, src, dst, rel, weight, created_at FROM memory_edges ORDER BY id`)
	if err != nil {
		return Backup{}, fmt.Errorf("query edges: %w", err)
	}
	for erows.Next() {
		var e Edge
		if err := erows.Scan(&e.ID, &e.Src, &e.Dst, &e.Rel, &e.Weight, &e.CreatedAt); err != nil {
			erows.Close()
			return Backup{}, fmt.Errorf("scan edge: %w", err)
		}
		edges = append(edges, e)
	}
	erows.Close()
	if err := erows.Err(); err != nil {
		return Backup{}, fmt.Errorf("read edges: %w", err)
	}

	return Backup{
		Version:   backupVersion,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Nodes:     nodes,
		Edges:     edges,
	}, nil
}

// encodeBackup writes the backup as JSON, optionally gzip-compressed.
func encodeBackup(w io.Writer, b Backup, useGzip bool) error {
	if !useGzip {
		return json.NewEncoder(w).Encode(b)
	}
	gw := gzip.NewWriter(w)
	if err := json.NewEncoder(gw).Encode(b); err != nil {
		gw.Close()
		return err
	}
	return gw.Close()
}

// capReader fails once more than left bytes have been read.
type capReader struct {
	r    io.Reader
	left int64
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, errors.New("backup exceeds the maximum restore size")
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}

// decodeBackup parses a backup, inflating gzip with a hard cap on the output size.
func decodeBackup(r io.Reader, gzipped bool) (Backup, error) {
	if gzipped {
		gz, err := gzip.NewReader(r)
		if err != nil {
			return Backup{}, errors.New("invalid gzip data")
		}
		defer gz.Close()
		r = gz
	}
	var b Backup
	if err := json.NewDecoder(&capReader{r: r, left: maxRestoreBytes}).Decode(&b); err != nil {
		return Backup{}, fmt.Errorf("invalid backup JSON data: %w", err)
	}
	return b, nil
}

// validateBackup rejects payloads that would wipe the database for nothing or that
// reference rows that do not exist.
func validateBackup(b Backup) error {
	if len(b.Nodes) == 0 && len(b.Edges) == 0 {
		return errors.New("backup is empty; refusing to replace the database with nothing")
	}
	ids := make(map[string]struct{}, len(b.Nodes))
	for _, n := range b.Nodes {
		if n.ID == "" || strings.TrimSpace(n.Body) == "" {
			return errors.New("backup contains a node without an id or body")
		}
		if _, dup := ids[n.ID]; dup {
			return fmt.Errorf("backup contains duplicate node id %q", n.ID)
		}
		ids[n.ID] = struct{}{}
		switch n.Status {
		case "", statusActive, statusProposed, statusRetired:
		default:
			return fmt.Errorf("node %q has unknown status %q", n.ID, n.Status)
		}
	}
	for _, e := range b.Edges {
		if _, ok := ids[e.Src]; !ok {
			return fmt.Errorf("edge %d references a missing source node", e.ID)
		}
		if _, ok := ids[e.Dst]; !ok {
			return fmt.Errorf("edge %d references a missing target node", e.ID)
		}
	}
	return nil
}

// restoreBackup replaces all data in a single transaction: either the whole backup
// is restored or nothing changes.
func restoreBackup(ctx context.Context, b Backup) (nodes, edges int64, err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("begin restore: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful commit

	if _, err := tx.Exec(ctx, "DELETE FROM memory_edges"); err != nil {
		return 0, 0, fmt.Errorf("clear edges: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM memory_nodes"); err != nil {
		return 0, 0, fmt.Errorf("clear nodes: %w", err)
	}

	nodeRows := nodeRowsFromBackup(b.Nodes)
	nodes, err = tx.CopyFrom(ctx, pgx.Identifier{"memory_nodes"}, restoredNodeColumns,
		pgx.CopyFromSlice(len(nodeRows), func(i int) ([]any, error) { return nodeRows[i], nil }))
	if err != nil {
		return 0, 0, fmt.Errorf("restore nodes: %w", err)
	}

	edgeRows := make([][]any, 0, len(b.Edges))
	for _, e := range b.Edges {
		edgeRows = append(edgeRows, []any{e.ID, e.Src, e.Dst, e.Rel, e.Weight, e.CreatedAt})
	}
	edges, err = tx.CopyFrom(ctx, pgx.Identifier{"memory_edges"},
		[]string{"id", "src", "dst", "rel", "weight", "created_at"},
		pgx.CopyFromSlice(len(edgeRows), func(i int) ([]any, error) { return edgeRows[i], nil }))
	if err != nil {
		return 0, 0, fmt.Errorf("restore edges: %w", err)
	}

	// COPY wrote explicit ids, so move the sequence past them; otherwise the next
	// inserted edge would collide with a restored one.
	if _, err := tx.Exec(ctx, `SELECT setval(pg_get_serial_sequence('memory_edges', 'id'),
		COALESCE((SELECT MAX(id) + 1 FROM memory_edges), 1), false)`); err != nil {
		return 0, 0, fmt.Errorf("reset edge sequence: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, fmt.Errorf("commit restore: %w", err)
	}
	return nodes, edges, nil
}

func handleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Export fully before writing any header, so a failure becomes a clean error
	// response instead of a truncated download.
	backup, err := exportBackup(ctx)
	if err != nil {
		log.Printf("backup failed: %v", err)
		jsonResp(w, http.StatusServiceUnavailable, map[string]string{"error": "unable to read data for backup"})
		return
	}

	useGzip := r.URL.Query().Get("gzip") == "true"
	if useGzip {
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", "attachment; filename=mem-backup.json.gz")
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=mem-backup.json")
	}
	if err := encodeBackup(w, backup, useGzip); err != nil {
		log.Printf("backup write failed: %v", err)
	}
}

func handleRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !requireSteward(w, r) {
		return
	}
	// A custom header cannot be sent cross-site without a CORS preflight, which
	// this service never grants. It also makes a replace-everything call explicit.
	if r.Header.Get("X-Confirm-Restore") != "replace-all" {
		jsonResp(w, http.StatusForbidden, map[string]string{"error": "restore replaces all data; send X-Confirm-Restore: replace-all"})
		return
	}

	gzipped := r.Header.Get("Content-Encoding") == "gzip" ||
		strings.HasPrefix(r.Header.Get("Content-Type"), "application/gzip")
	backup, err := decodeBackup(r.Body, gzipped)
	if err != nil {
		jsonResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateBackup(backup); err != nil {
		jsonResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	nodes, edges, err := restoreBackup(ctx, backup)
	if err != nil {
		log.Printf("restore failed: %v", err)
		jsonResp(w, http.StatusInternalServerError, map[string]string{"error": "restore failed; no changes were made"})
		return
	}
	jsonResp(w, http.StatusOK, map[string]interface{}{
		"status":    "restored",
		"nodes":     nodes,
		"edges":     edges,
		"timestamp": backup.Timestamp,
	})
}

func runCLIBackup(filename string, useGzip bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	backup, err := exportBackup(ctx)
	if err != nil {
		log.Fatalf("backup failed: %v", err)
	}

	var out io.Writer = os.Stdout
	var file *os.File
	if filename != "" && filename != "-" {
		// Backups hold every memory: keep them private to the current user.
		file, err = os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			log.Fatalf("cannot create file %s: %v", filename, err)
		}
		out = file
	}
	if err := encodeBackup(out, backup, useGzip); err != nil {
		log.Fatalf("failed to write backup: %v", err)
	}
	if file != nil {
		if err := file.Close(); err != nil {
			log.Fatalf("failed to finish writing %s: %v", filename, err)
		}
	}
	fmt.Fprintf(os.Stderr, "Backup complete: %d nodes, %d edges\n", len(backup.Nodes), len(backup.Edges))
}

func runCLIRestore(filename string) {
	if filename == "" {
		log.Fatal("restore requires --file argument")
	}

	var in io.Reader = os.Stdin
	if filename != "-" {
		f, err := os.Open(filename)
		if err != nil {
			log.Fatalf("cannot open file %s: %v", filename, err)
		}
		defer f.Close()
		in = f
	}
	// Detect gzip from its magic bytes, so stdin and renamed files work too.
	br := bufio.NewReader(in)
	magic, _ := br.Peek(2)
	gzipped := len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b

	backup, err := decodeBackup(br, gzipped)
	if err != nil {
		log.Fatalf("%v", err)
	}
	if err := validateBackup(backup); err != nil {
		log.Fatalf("refusing to restore: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	nodes, edges, err := restoreBackup(ctx, backup)
	if err != nil {
		log.Fatalf("restore failed, no changes were made: %v", err)
	}
	fmt.Fprintf(os.Stderr, "Restore complete: %d nodes, %d edges restored\n", nodes, edges)
}

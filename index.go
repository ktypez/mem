package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// The search index. SQLite FTS5 with the trigram tokenizer so Thai and other
// languages written without spaces can be matched by substring, with a plain
// LIKE scan as the fallback for very short queries. The whole index is derived
// state: `mem reindex` rebuilds it from the markdown files at any time.

var indexDB *sql.DB

func indexPath() string {
	return filepath.Join(wikiDir(), ".mem-index.db")
}

func openIndex() error {
	if err := os.MkdirAll(wikiDir(), 0o700); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", indexPath())
	if err != nil {
		return err
	}
	// A single writer at a time; WAL keeps readers unblocked.
	for _, stmt := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		`CREATE TABLE IF NOT EXISTS pages (
			slug TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			tags TEXT NOT NULL DEFAULT '',
			body TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			mtime INTEGER NOT NULL DEFAULT 0,
			size INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS pages_fts USING fts5(
			slug UNINDEXED, title, body, tags, tokenize='trigram'
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return fmt.Errorf("index schema: %w", err)
		}
	}
	indexDB = db
	return nil
}

func closeIndex() {
	if indexDB != nil {
		indexDB.Close()
		indexDB = nil
	}
}

// indexUpsertPage stores the page metadata and refreshes its FTS row.
func indexUpsertPage(p Page, mtime int64, size int64) error {
	if indexDB == nil {
		return fmt.Errorf("index not open")
	}
	tags := strings.Join(p.Tags, ", ")
	if _, err := indexDB.Exec(`INSERT INTO pages
			(slug, title, tags, body, created_at, updated_at, updated_by, mtime, size)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(slug) DO UPDATE SET title=excluded.title, tags=excluded.tags,
			body=excluded.body, created_at=excluded.created_at, updated_at=excluded.updated_at,
			updated_by=excluded.updated_by, mtime=excluded.mtime, size=excluded.size`,
		p.Slug, p.Title, tags, p.Body, p.CreatedAt, p.UpdatedAt, p.UpdatedBy, mtime, size); err != nil {
		return err
	}
	if _, err := indexDB.Exec(`DELETE FROM pages_fts WHERE slug = ?`, p.Slug); err != nil {
		return err
	}
	_, err := indexDB.Exec(`INSERT INTO pages_fts (slug, title, body, tags) VALUES (?, ?, ?, ?)`,
		p.Slug, p.Title, p.Body, tags)
	return err
}

// indexDeletePage drops a page from both tables.
func indexDeletePage(slug string) error {
	if indexDB == nil {
		return nil
	}
	if _, err := indexDB.Exec(`DELETE FROM pages_fts WHERE slug = ?`, slug); err != nil {
		return err
	}
	_, err := indexDB.Exec(`DELETE FROM pages WHERE slug = ?`, slug)
	return err
}

// indexPageCurrent reports whether the index already reflects the file state,
// so a rescan can skip unchanged files cheaply.
func indexPageCurrent(slug string, mtime int64, size int64) bool {
	if indexDB == nil {
		return false
	}
	var m, s int64
	err := indexDB.QueryRow(`SELECT mtime, size FROM pages WHERE slug = ?`, slug).Scan(&m, &s)
	return err == nil && m == mtime && s == size
}

// indexRemoveMissing drops index rows whose files no longer exist and returns
// how many were removed.
func indexRemoveMissing(seen map[string]bool) int {
	if indexDB == nil {
		return 0
	}
	rows, err := indexDB.Query(`SELECT slug FROM pages`)
	if err != nil {
		return 0
	}
	defer rows.Close()
	var missing []string
	for rows.Next() {
		var slug string
		if rows.Scan(&slug) == nil && !seen[slug] {
			missing = append(missing, slug)
		}
	}
	rows.Close()
	for _, slug := range missing {
		indexDeletePage(slug)
	}
	return len(missing)
}

// matchQuery builds an FTS5 MATCH expression. Every whitespace-separated term is
// double-quoted so punctuation in user input can never change the query syntax.
func matchQuery(q string) string {
	terms := strings.Fields(q)
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		t = strings.ReplaceAll(t, `"`, `""`)
		if t != "" {
			quoted = append(quoted, `"`+t+`"`)
		}
	}
	return strings.Join(quoted, " ")
}

// searchHit is one search result: the page it came from plus a short excerpt.
type searchHit struct {
	Slug      string   `json:"slug"`
	Title     string   `json:"title"`
	Tags      []string `json:"tags"`
	Body      string   `json:"body"`
	Excerpt   string   `json:"excerpt"`
	UpdatedAt string   `json:"updated_at"`
}

// searchWiki runs FTS first, then falls back to a substring scan. Queries
// shorter than three characters skip straight to the fallback (trigram needs
// at least three characters to match).
func searchWiki(query string, limit int) []searchHit {
	if indexDB == nil || strings.TrimSpace(query) == "" {
		return []searchHit{}
	}
	if limit < 1 {
		limit = 10
	}
	q := strings.TrimSpace(query)
	var slugs []string

	if len([]rune(q)) >= 3 {
		if rows, err := indexDB.Query(
			`SELECT slug FROM pages_fts WHERE pages_fts MATCH ? ORDER BY rank LIMIT ?`,
			matchQuery(q), limit); err == nil {
			for rows.Next() {
				var s string
				if rows.Scan(&s) == nil {
					slugs = append(slugs, s)
				}
			}
			rows.Close()
		} else {
			log.Printf("fts query failed, falling back to substring scan: %v", err)
		}
	}
	if len(slugs) < limit {
		seen := map[string]bool{}
		for _, s := range slugs {
			seen[s] = true
		}
		pattern := "%" + escapeLike(q) + "%"
		rows, err := indexDB.Query(
			`SELECT slug FROM pages WHERE body LIKE ? OR title LIKE ? ORDER BY updated_at DESC LIMIT ?`,
			pattern, pattern, limit*2)
		if err == nil {
			for rows.Next() {
				var s string
				if rows.Scan(&s) == nil && !seen[s] && len(slugs) < limit {
					seen[s] = true
					slugs = append(slugs, s)
				}
			}
			rows.Close()
		}
	}

	hits := []searchHit{}
	for _, slug := range slugs {
		var title, body, tags, updatedAt string
		if err := indexDB.QueryRow(
			`SELECT title, body, tags, updated_at FROM pages WHERE slug = ?`, slug).Scan(&title, &body, &tags, &updatedAt); err != nil {
			continue
		}
		hits = append(hits, searchHit{
			Slug:      slug,
			Title:     title,
			Tags:      splitTags(tags),
			Body:      body,
			Excerpt:   excerpt(body, q, 240),
			UpdatedAt: updatedAt,
		})
		if len(hits) >= limit {
			break
		}
	}
	return hits
}

func splitTags(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// excerpt returns a window of body centred on the first case-insensitive match
// of the query (or the head of the body when nothing matches).
func excerpt(body, q string, width int) string {
	clean := strings.TrimSpace(body)
	if clean == "" {
		return ""
	}
	runes := []rune(clean)
	lq := strings.ToLower(q)
	lb := strings.ToLower(clean)
	idx := strings.Index(lb, lq)
	if idx < 0 {
		if len(runes) <= width {
			return clean
		}
		return string(runes[:width]) + " …"
	}
	runeIdx := len([]rune(lb[:idx]))
	start := runeIdx - width/3
	if start < 0 {
		start = 0
	}
	end := start + width
	if end > len(runes) {
		end = len(runes)
		start = end - width
		if start < 0 {
			start = 0
		}
	}
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += " …"
	}
	return out
}

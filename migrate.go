package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

// importLegacy converts an old mem backup (nodes + edges knowledge graph) into
// wiki pages. Nodes are grouped by topic (falling back to subject, then a
// shared Inbox page) and each node body becomes one fact line. The legacy
// database itself is never touched: it stays readable through its own backup.

type legacyNode struct {
	Body      string `json:"body"`
	Layer     string `json:"layer"`
	Topic     string `json:"topic"`
	Subject   string `json:"subject"`
	Source    string `json:"source"`
	Status    string `json:"status"`
	FactType  string `json:"fact_type"`
	CreatedAt string `json:"created_at"`
}

type legacyBackup struct {
	Version string       `json:"version"`
	Nodes   []legacyNode `json:"nodes"`
}

// runImportLegacy reads a legacy backup file (JSON, optionally gzipped) and
// writes the facts into wiki pages.
func runImportLegacy(path string) error {
	var src io.Reader
	if path == "" || path == "-" {
		src = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		src = f
	}

	var raw io.Reader = src
	if gz, err := gzip.NewReader(src); err == nil {
		defer gz.Close()
		raw = gz
	}

	var data legacyBackup
	dec := json.NewDecoder(io.LimitReader(raw, 512<<20))
	if err := dec.Decode(&data); err != nil {
		return fmt.Errorf("not a legacy backup: %w", err)
	}

	if err := openIndex(); err != nil {
		return err
	}
	defer closeIndex()
	if _, err := rescanWiki(); err != nil {
		log.Printf("index resync before import: %v", err)
	}

	imported, skippedSystem, skippedEmpty, skippedDup := 0, 0, 0, 0
	pages := map[string]bool{}
	for _, n := range data.Nodes {
		body := strings.TrimSpace(n.Body)
		if body == "" {
			skippedEmpty++
			continue
		}
		if n.Source == "system" || n.Status == "retired" {
			skippedSystem++
			continue
		}
		title := strings.TrimSpace(n.Topic)
		if title == "" {
			title = strings.TrimSpace(n.Subject)
		}
		if title == "" {
			title = "Inbox"
		}
		p, created, err := appendFact(title, "", body, n.Source+" (legacy)")
		if err != nil {
			return err
		}
		if pages[p.Slug] {
			created = false
		}
		pages[p.Slug] = true
		if created {
			// Tag migrated pages so they are easy to find and prune later.
			if p, err := loadPage(p.Slug); err == nil {
				found := false
				for _, t := range p.Tags {
					if t == "migrated" {
						found = true
					}
				}
				if !found {
					p.Tags = append(p.Tags, "migrated")
				}
				if err := savePage(p); err != nil {
					return err
				}
			}
		}
		imported++
		_ = skippedDup
	}

	fmt.Printf("Import complete: %d facts -> %d pages\n", imported, len(pages))
	fmt.Printf("  skipped system/retired facts: %d\n", skippedSystem)
	fmt.Printf("  skipped empty bodies: %d\n", skippedEmpty)
	return nil
}

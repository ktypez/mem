package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The nightly organizer: safe, boring housekeeping for the wiki.
//
//   - resync the index with the files
//   - drop exact duplicate fact lines inside a page
//   - report facts that appear on more than one page (review log, no auto-move:
//     which page a fact belongs to is a context decision, not a script's)
//   - hard-delete trash older than MEM_TRASH_DAYS (default 30)
//   - optimize the SQLite index
//   - log the run summary into the "mem-organizer" page

// factLinePrefix / factSep describe the canonical fact line shape written by
// appendFact: `- <body> — <source>, <date>`.
const factSep = " — "

// normalizeFactLine strips the bullet and the trailing provenance so the same
// fact written on two pages (or with different timestamps) is recognised.
func normalizeFactLine(line string) string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "- ")
	s = strings.TrimPrefix(s, "* ")
	if i := strings.LastIndex(s, factSep); i > 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// isFactLine reports whether a body line is a fact bullet.
func isFactLine(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ")
}

type organizeResult struct {
	PagesScanned  int
	DedupeLines   int
	CrossPageDups [][2]string // fact body, pages it appears on (joined)
	TrashRemoved  int
	IndexSynced   int
}

// runOrganizer performs one pass and returns a summary.
func runOrganizer() (organizeResult, error) {
	var res organizeResult

	if err := openIndex(); err != nil {
		return res, err
	}
	synced, err := rescanWiki()
	if err != nil {
		return res, err
	}
	res.IndexSynced = synced

	pages, err := listPages()
	if err != nil {
		return res, err
	}
	res.PagesScanned = len(pages)

	// Within-page dedupe + cross-page fact index.
	factPages := map[string]map[string]bool{} // normalized body -> slugs
	for _, p := range pages {
		full, err := loadPage(p.Slug)
		if err != nil {
			continue
		}
		lines := strings.Split(full.Body, "\n")
		seen := map[string]bool{}
		changed := false
		var out []string
		for _, line := range lines {
			if isFactLine(line) {
				key := normalizeFactLine(line)
				if key != "" && seen[key] {
					changed = true
					res.DedupeLines++
					continue
				}
				if key != "" {
					seen[key] = true
					if factPages[key] == nil {
						factPages[key] = map[string]bool{}
					}
					factPages[key][p.Slug] = true
				}
			}
			out = append(out, line)
		}
		if changed {
			full.Body = strings.Join(out, "\n")
			if err := savePage(full); err != nil {
				log.Printf("organizer: save %s: %v", p.Slug, err)
				continue
			}
		}
	}
	for body, slugs := range factPages {
		if len(slugs) > 1 {
			names := make([]string, 0, len(slugs))
			for s := range slugs {
				names = append(names, s)
			}
			res.CrossPageDups = append(res.CrossPageDups, [2]string{body, strings.Join(names, ", ")})
		}
	}

	// Trash rotation.
	res.TrashRemoved, err = rotateTrash()
	if err != nil {
		log.Printf("organizer: trash rotation: %v", err)
	}

	// Index maintenance.
	if indexDB != nil {
		if _, err := indexDB.Exec("PRAGMA optimize"); err != nil {
			log.Printf("organizer: index optimize: %v", err)
		}
	}

	return res, nil
}

// rotateTrash hard-deletes trashed pages older than the retention window.
func rotateTrash() (int, error) {
	days := 30
	if v := strings.TrimSpace(os.Getenv("MEM_TRASH_DAYS")); v != "" {
		if n := atoiSafe(v); n >= 0 {
			days = n
		}
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	trash := filepath.Join(wikiDir(), "trash")
	entries, err := os.ReadDir(trash)
	if err != nil {
		return 0, nil // no trash yet
	}
	n := 0
	for _, e := range entries {
		path := filepath.Join(trash, e.Name())
		st, err := os.Stat(path)
		if err != nil || !st.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(path); err == nil {
			n++
		}
	}
	return n, nil
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// organizeLog writes the run summary into the mem-organizer page.
func organizeLog(res organizeResult) {
	summary := fmt.Sprintf("organize run: %d pages, %d duplicate line(s) removed, %d cross-page fact(s), %d trashed file(s), %d index sync", res.PagesScanned, res.DedupeLines, len(res.CrossPageDups), res.TrashRemoved, res.IndexSynced)
	if _, _, err := appendFact("mem-organizer", "", summary, "mem-organizer"); err != nil {
		log.Printf("organizer: log: %v", err)
	}
	if len(res.CrossPageDups) > 0 {
		for _, dup := range res.CrossPageDups {
			line := fmt.Sprintf("fact on multiple pages: %q -> %s", dup[0], dup[1])
			if _, _, err := appendFact("mem-organizer", "", line, "mem-organizer"); err != nil {
				log.Printf("organizer: log: %v", err)
			}
		}
	}
}

func formatOrganizeResult(res organizeResult) string {
	return fmt.Sprintf("pages=%d deduped=%d crossPage=%d trashRemoved=%d indexSync=%d",
		res.PagesScanned, res.DedupeLines, len(res.CrossPageDups), res.TrashRemoved, res.IndexSynced)
}

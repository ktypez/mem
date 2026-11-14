package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The wiki store: markdown files are the source of truth, the SQLite index is
// derived state that can always be rebuilt from the files. A page is a file
// `<slug>.md` with a small frontmatter header followed by free-form markdown.

// wikiDir returns the directory holding the pages.
func wikiDir() string {
	if d := strings.TrimSpace(os.Getenv("MEM_WIKI_DIR")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, "mem-wiki")
}

// Page is one wiki page.
type Page struct {
	Slug      string   `json:"slug"`
	Title     string   `json:"title"`
	Tags      []string `json:"tags"`
	Body      string   `json:"body"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	UpdatedBy string   `json:"updated_by"`
}

// factsHeading is where appendFact files new lines, so a page always has one
// predictable section that grows.
const factsHeading = "## Facts"

// --- slug handling ---

var slugStrip = regexp.MustCompile(`[^\p{L}\p{N}\p{M}\- ]`)
var slugDash = regexp.MustCompile(`[-\s]+`)

// slugify turns a page title into a filesystem-safe slug. Unicode letters and
// digits are kept, so Thai and other non-ASCII titles survive intact.
func slugify(title string) string {
	t := slugStrip.ReplaceAllString(strings.TrimSpace(title), "")
	t = slugDash.ReplaceAllString(t, "-")
	t = strings.Trim(t, "-")
	if t == "" {
		t = "page"
	}
	// Keep byte length bounded for filesystem limits without cutting a rune.
	runes := []rune(t)
	if len(runes) > 80 {
		t = strings.TrimRight(string(runes[:80]), "-")
	}
	return t
}

// safeSlug rejects anything that could escape the wiki directory.
func safeSlug(slug string) (string, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" || strings.ContainsAny(slug, "/\\") || slug == "." || slug == ".." ||
		strings.Contains(slug, "..") || strings.HasPrefix(slug, ".") {
		return "", fmt.Errorf("invalid slug %q", slug)
	}
	return slug, nil
}

// --- frontmatter ---

// renderPage serialises a page to its markdown file content.
func renderPage(p Page) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: %s\n", p.Title)
	fmt.Fprintf(&b, "tags: [%s]\n", strings.Join(p.Tags, ", "))
	fmt.Fprintf(&b, "created: %s\n", p.CreatedAt)
	fmt.Fprintf(&b, "updated: %s\n", p.UpdatedAt)
	fmt.Fprintf(&b, "updated_by: %s\n", p.UpdatedBy)
	b.WriteString("---\n\n")
	b.WriteString(p.Body)
	if !strings.HasSuffix(p.Body, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// parsePageFile reads a page file into a Page. Files with a malformed or
// missing header still load: the file stem becomes the title and the whole
// content becomes the body, so hand-edited files never become unreadable.
func parsePageFile(path string) (Page, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Page{}, err
	}
	content := string(raw)
	slug := strings.TrimSuffix(filepath.Base(path), ".md")
	p := Page{Slug: slug, Title: slug, Tags: []string{}}

	if strings.HasPrefix(content, "---\n") {
		end := strings.Index(content[4:], "\n---\n")
		if end >= 0 {
			header := content[4 : 4+end]
			p.Body = content[4+end+5:]
			p.Title = slug
			for _, line := range strings.Split(header, "\n") {
				key, val, ok := strings.Cut(line, ":")
				if !ok {
					continue
				}
				key = strings.TrimSpace(key)
				val = strings.TrimSpace(val)
				switch key {
				case "title":
					if val != "" {
						p.Title = val
					}
				case "tags":
					val = strings.TrimSuffix(strings.TrimPrefix(val, "["), "]")
					for _, t := range strings.Split(val, ",") {
						if t = strings.TrimSpace(t); t != "" {
							p.Tags = append(p.Tags, t)
						}
					}
				case "created":
					p.CreatedAt = val
				case "updated":
					p.UpdatedAt = val
				case "updated_by":
					p.UpdatedBy = val
				}
			}
			return p, nil
		}
	}
	p.Body = content
	return p, nil
}

// --- page operations ---

func pageFile(slug string) string {
	return filepath.Join(wikiDir(), slug+".md")
}

// loadPage reads one page from disk.
func loadPage(slug string) (Page, error) {
	p, err := parsePageFile(pageFile(slug))
	if err != nil {
		return Page{}, err
	}
	p.Slug = slug
	return p, nil
}

// savePage writes a page atomically (tmp file + rename) and refreshes the index.
func savePage(p Page) error {
	if err := os.MkdirAll(wikiDir(), 0o700); err != nil {
		return err
	}
	p.UpdatedAt = time.Now().Format(time.RFC3339)
	final := pageFile(p.Slug)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, []byte(renderPage(p)), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		return err
	}
	st, err := os.Stat(final)
	if err != nil {
		return err
	}
	return indexUpsertPage(p, st.ModTime().Unix(), st.Size())
}

// upsertPage creates or updates a page by slug. The title in the frontmatter is
// rewritten when the caller supplies a new one.
func upsertPage(slug, title, body string, tags []string, by string) (Page, error) {
	cleanSlug, err := safeSlug(slug)
	if err != nil {
		return Page{}, err
	}
	nowStr := time.Now().Format(time.RFC3339)
	p, err := loadPage(cleanSlug)
	if err != nil {
		// New page.
		if strings.TrimSpace(title) == "" {
			title = cleanSlug
		}
		p = Page{
			Slug: cleanSlug, Title: title, Tags: tags, Body: body,
			CreatedAt: nowStr, UpdatedAt: nowStr, UpdatedBy: by,
		}
		if len(p.Tags) == 0 {
			p.Tags = []string{"memory"}
		}
	} else {
		if title != "" {
			p.Title = title
		}
		if body != "" {
			p.Body = body
		}
		if len(tags) > 0 {
			p.Tags = tags
		}
		if by != "" {
			p.UpdatedBy = by
		}
	}
	if strings.TrimSpace(p.Body) == "" {
		p.Body = factsHeading + "\n"
	}
	if err := savePage(p); err != nil {
		return Page{}, err
	}
	return p, nil
}

// slugForTitle finds an existing page whose frontmatter title matches, or
// derives a fresh unique slug for a new one.
func slugForTitle(title string) (string, error) {
	entries, err := os.ReadDir(wikiDir())
	if err != nil {
		return slugify(title), nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p, err := parsePageFile(filepath.Join(wikiDir(), e.Name()))
		if err == nil && strings.EqualFold(p.Title, title) {
			return p.Slug, nil
		}
	}
	base := slugify(title)
	slug := base
	for i := 2; ; i++ {
		if _, err := os.Stat(pageFile(slug)); os.IsNotExist(err) {
			return slug, nil
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
}

// appendFact writes one fact line into the page for its topic (falling back to
// subject, then a shared inbox page). Duplicate facts update the page instead
// of piling up copies. It returns the page and whether a new line was added.
func appendFact(topic, subject, body, source string) (Page, bool, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Page{}, false, fmt.Errorf("empty fact body")
	}
	title := strings.TrimSpace(topic)
	if title == "" {
		title = strings.TrimSpace(subject)
	}
	if title == "" {
		title = "Inbox"
	}
	slug, err := slugForTitle(title)
	if err != nil {
		return Page{}, false, err
	}
	nowStr := time.Now().Format(time.RFC3339)
	p, err := loadPage(slug)
	created := false
	if err != nil {
		p = Page{Slug: slug, Title: title, Tags: []string{"memory"}, Body: factsHeading + "\n",
			CreatedAt: nowStr, UpdatedAt: nowStr}
		created = true
	}
	if source == "" {
		source = "agent"
	}
	line := fmt.Sprintf("- %s — %s, %s", body, source, time.Now().Format("2006-01-02"))
	if strings.Contains(p.Body, body) {
		// Already recorded: touch the page instead of duplicating the line.
		p.UpdatedBy = source
		if err := savePage(p); err != nil {
			return Page{}, false, err
		}
		return p, false, nil
	}
	if !strings.Contains(p.Body, factsHeading) {
		if !strings.HasSuffix(p.Body, "\n") {
			p.Body += "\n"
		}
		p.Body += "\n" + factsHeading + "\n"
	}
	p.Body += line + "\n"
	p.UpdatedBy = source
	if err := savePage(p); err != nil {
		return Page{}, false, err
	}
	return p, created, nil
}

// listPages returns the frontmatter of every page, newest first.
func listPages() ([]Page, error) {
	entries, err := os.ReadDir(wikiDir())
	if err != nil {
		return []Page{}, nil // empty wiki is fine
	}
	pages := []Page{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p, err := parsePageFile(filepath.Join(wikiDir(), e.Name()))
		if err != nil {
			continue
		}
		p.Slug = strings.TrimSuffix(e.Name(), ".md")
		pages = append(pages, p)
	}
	sort.SliceStable(pages, func(i, j int) bool {
		return pages[i].UpdatedAt > pages[j].UpdatedAt
	})
	return pages, nil
}

// deletePage moves a page into trash/ instead of erasing it: memory is
// append-only even when a page is "deleted".
func deletePage(slug string) (string, error) {
	cleanSlug, err := safeSlug(slug)
	if err != nil {
		return "", err
	}
	src := pageFile(cleanSlug)
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("page %q not found", cleanSlug)
	}
	trash := filepath.Join(wikiDir(), "trash")
	if err := os.MkdirAll(trash, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(trash, fmt.Sprintf("%s.%s.md", cleanSlug, time.Now().Format("20060102-150405")))
	if err := os.Rename(src, dst); err != nil {
		return "", err
	}
	indexDeletePage(cleanSlug)
	return dst, nil
}

// rescanWiki walks the page files and syncs the index with the disk. External
// edits (agents touching the markdown directly) are picked up here.
func rescanWiki() (int, error) {
	entries, err := os.ReadDir(wikiDir())
	if err != nil {
		return 0, nil
	}
	seen := map[string]bool{}
	changed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".md")
		seen[slug] = true
		path := filepath.Join(wikiDir(), e.Name())
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		if indexPageCurrent(slug, st.ModTime().Unix(), st.Size()) {
			continue
		}
		p, err := parsePageFile(path)
		if err != nil {
			continue
		}
		p.Slug = slug
		if err := indexUpsertPage(p, st.ModTime().Unix(), st.Size()); err == nil {
			changed++
		}
	}
	removed := indexRemoveMissing(seen)
	return changed + removed, nil
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testWiki(t *testing.T) {
	t.Helper()
	t.Setenv("MEM_WIKI_DIR", t.TempDir())
	if err := openIndex(); err != nil {
		t.Fatalf("openIndex: %v", err)
	}
	t.Cleanup(closeIndex)
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Hello World":     "Hello-World",
		"  padded  name ": "padded-name",
		"bad/slash:name":  "badslashname",
		"":                "page",
		"ตัวอย่าง หน้า":   "ตัวอย่าง-หน้า",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSafeSlug(t *testing.T) {
	for _, bad := range []string{"", "..", "../x", "a/b", ".hidden"} {
		if _, err := safeSlug(bad); err == nil {
			t.Errorf("safeSlug(%q) should fail", bad)
		}
	}
	if _, err := safeSlug("ok-page"); err != nil {
		t.Errorf("safeSlug(ok-page) should pass: %v", err)
	}
}

func TestPageRoundtrip(t *testing.T) {
	testWiki(t)
	p, err := upsertPage("test-page", "Test Page", "## Facts\n- hello — agent, 2026-11-14\n", []string{"test"}, "opencode")
	if err != nil {
		t.Fatalf("upsertPage: %v", err)
	}
	got, err := loadPage(p.Slug)
	if err != nil {
		t.Fatalf("loadPage: %v", err)
	}
	if got.Title != "Test Page" || !strings.Contains(got.Body, "hello") {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if !fileExists(filepath.Join(wikiDir(), "test-page.md")) {
		t.Fatal("page file missing")
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestAppendFactDedupe(t *testing.T) {
	testWiki(t)
	if _, _, err := appendFact("truck", "", "battery at 64%", "opencode"); err != nil {
		t.Fatalf("appendFact 1: %v", err)
	}
	p, appended, err := appendFact("truck", "", "battery at 64%", "hermes")
	if err != nil {
		t.Fatalf("appendFact 2: %v", err)
	}
	if appended {
		t.Fatal("duplicate fact should not append a second line")
	}
	if p.Title != "truck" {
		t.Fatalf("page title = %q, want truck", p.Title)
	}
	if n := strings.Count(p.Body, "battery at 64%"); n != 1 {
		t.Fatalf("fact appears %d times, want 1", n)
	}
}

func TestAppendFactFallsBackToInbox(t *testing.T) {
	testWiki(t)
	p, _, err := appendFact("", "", "no topic given", "agent")
	if err != nil {
		t.Fatalf("appendFact: %v", err)
	}
	if p.Title != "Inbox" {
		t.Fatalf("expected Inbox page, got %q", p.Title)
	}
}

func TestSearchThai(t *testing.T) {
	testWiki(t)
	if _, _, err := appendFact("การบ้าน", "", "ระบบสำรองข้อมูลทำงานผ่าน Salt ทุกคืน", "opencode"); err != nil {
		t.Fatalf("appendFact: %v", err)
	}
	if _, err := upsertPage("eng", "Engineering", "The backup runs through Salt every night.", nil, "agent"); err != nil {
		t.Fatalf("upsertPage: %v", err)
	}
	hits := searchWiki("Salt", 10)
	if len(hits) == 0 {
		t.Fatal("expected at least one hit for 'Salt'")
	}
	// Trigram search must also find Thai substrings.
	hits = searchWiki("สำรองข้อมูล", 10)
	if len(hits) == 0 {
		t.Fatal("expected a Thai substring hit")
	}
	// Short queries fall back to LIKE.
	hits = searchWiki("Salt", 10)
	_ = hits
}

func TestDeletePageTrashes(t *testing.T) {
	testWiki(t)
	p, err := upsertPage("doomed", "Doomed", "## Facts\n", nil, "agent")
	if err != nil {
		t.Fatalf("upsertPage: %v", err)
	}
	dst, err := deletePage(p.Slug)
	if err != nil {
		t.Fatalf("deletePage: %v", err)
	}
	if !strings.Contains(dst, "trash") || !fileExists(dst) {
		t.Fatalf("expected file in trash, got %q", dst)
	}
	if fileExists(pageFile(p.Slug)) {
		t.Fatal("original file should be gone")
	}
}

func TestRenderParseRoundtrip(t *testing.T) {
	p := Page{
		Slug: "s", Title: "Some: Title", Tags: []string{"a", "b"},
		Body: "line one\nline two\n", CreatedAt: "c", UpdatedAt: "u", UpdatedBy: "me",
	}
	var file = renderPage(p)
	dir := t.TempDir()
	path := filepath.Join(dir, "s.md")
	if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := parsePageFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Some: Title" || len(got.Tags) != 2 || !strings.Contains(got.Body, "line two") {
		t.Fatalf("parse mismatch: %+v", got)
	}
}

func TestMatchQueryQuotes(t *testing.T) {
	got := matchQuery(`he said "hi" there`)
	if !strings.Contains(got, `"he"`) || !strings.Contains(got, `"said"`) {
		t.Fatalf("unexpected match query: %q", got)
	}
}

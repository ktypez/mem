package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"golang.org/x/term"
)

// Pretty output for humans. Every command checks stdoutTTY(): terminals get
// the styled rendering, pipes/agents get the plain output unchanged, and
// -json always wins with machine-readable data.

// stdoutTTY reports whether stdout is a terminal.
func stdoutTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func termWidth(fallback int) int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
		return w
	}
	return fallback
}

// --- palette ---

var (
	cliAccent = lipgloss.NewStyle().Foreground(lipgloss.Color("81")) // soft blue
	cliPink   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	cliOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("114")) // green
	cliWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("215")) // orange
	cliMuted  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	cliHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")).Background(lipgloss.Color("235"))
)

// relTime renders an RFC3339 timestamp as a short human delta.
func relTime(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

// --- list table ---

func renderPagesTTY(pages []Page) string {
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(cliMuted).
		Headers("TITLE", "SLUG", "UPDATED", "TAGS").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return cliHeader.Padding(0, 1)
			}
			st := lipgloss.NewStyle().Padding(0, 1)
			if row%2 == 1 {
				st = st.Foreground(lipgloss.Color("250"))
			}
			return st
		})
	for _, p := range pages {
		t.Row(p.Title, p.Slug, relTime(p.UpdatedAt), strings.Join(p.Tags, "·"))
	}
	return t.Render() + "\n" + cliMuted.Render(fmt.Sprintf("%d page(s) · /v1/wiki/pages", len(pages)))
}

// --- search results ---

func renderSearchTTY(hits []searchHit, query string) string {
	if len(hits) == 0 {
		return cliWarn.Render("no matches for ") + cliAccent.Render(query)
	}
	var b strings.Builder
	for _, h := range hits {
		b.WriteString(cliPink.Render("▸ " + h.Title))
		b.WriteString(cliMuted.Render("  (" + h.Slug + " · " + relTime(h.UpdatedAt) + ")"))
		b.WriteString("\n")
		for _, line := range wrapText(h.Excerpt, termWidth(80)-4) {
			b.WriteString("  " + line + "\n")
		}
		b.WriteString("\n")
	}
	return b.String() + cliMuted.Render(fmt.Sprintf("%d result(s)", len(hits)))
}

func wrapText(s string, width int) []string {
	if width < 20 {
		width = 20
	}
	runes := []rune(strings.TrimSpace(s))
	if len(runes) == 0 {
		return nil
	}
	var out []string
	for len(runes) > width {
		cut := width
		for cut > width/2 && cut > 0 && runes[cut-1] != ' ' {
			cut--
		}
		out = append(out, strings.TrimRight(string(runes[:cut]), " "))
		runes = runes[cut:]
	}
	out = append(out, string(runes))
	return out
}

// --- markdown ---

var (
	glamourOnce     sync.Once
	glamourRenderer *glamour.TermRenderer
	glamourErr      error
)

func renderMarkdownTTY(body string) (string, error) {
	glamourOnce.Do(func() {
		glamourRenderer, glamourErr = glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(termWidth(80)),
		)
	})
	if glamourErr != nil {
		return "", glamourErr
	}
	return glamourRenderer.Render(body)
}

// --- status lines ---

func okLine(msg string) string {
	if !stdoutTTY() {
		return msg
	}
	return cliOK.Render("✓ ") + msg
}

// --- json output ---

func printJSON(v interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

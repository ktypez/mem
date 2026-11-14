package main

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// Interactive TUI: `mem` with no arguments on a terminal. Left sidebar lists
// pages (bubbles/list with built-in filtering), the right pane previews the
// selection; `/` runs a full-text search; enter opens the page in a scrollable
// viewport; n creates a page; r reindexes; q quits.

var (
	tuiAccent  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	tuiMuted   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	tuiTitle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")).Background(lipgloss.Color("235")).Padding(0, 1)
	tuiPanel   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	tuiStatus  = lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Background(lipgloss.Color("236")).Padding(0, 1)
	tuiOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	tuiWarn    = cliWarn
	tuiErr     = lipgloss.NewStyle().Foreground(lipgloss.Color("210"))
	tuiSidebar = 32
)

// --- markdown renderer (shared, created once) ---

var (
	tuiMDSync sync.Once
	tuiMD     *glamour.TermRenderer
	tuiMDErr  error
)

func renderMD(body string) (string, error) {
	tuiMDSync.Do(func() {
		tuiMD, tuiMDErr = glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(100))
	})
	if tuiMDErr != nil {
		return "", tuiMDErr
	}
	return tuiMD.Render(body)
}

// --- list items ---

type pageItem struct{ p Page }

func (i pageItem) Title() string { return i.p.Title }
func (i pageItem) Description() string {
	return relTime(i.p.UpdatedAt) + " · " + strings.Join(i.p.Tags, " ")
}
func (i pageItem) FilterValue() string { return i.p.Title + " " + i.p.Slug }

// --- modes ---

const (
	modeBrowse = iota
	modeSearch
	modeView
	modeNew
)

// --- messages ---

type contentMsg struct {
	slug     string
	rendered string
	err      error
}

type reindexedMsg struct{ n int }

type statusMsg string

// --- model ---

type tui struct {
	mode    int
	list    list.Model
	search  textinput.Model
	newName textinput.Model
	vp      viewport.Model

	allPages []Page
	status   string
	width    int
	height   int
}

func runTUI() error {
	if !stdoutTTY() {
		return fmt.Errorf("the interactive UI needs a terminal (use mem list / mem show)")
	}
	_, err := tea.NewProgram(newTUIModel(), tea.WithAltScreen()).Run()
	closeIndex()
	return err
}

func newTUIModel() tui {
	if err := openIndex(); err != nil {
		// The TUI still works read-only from the index-less paths.
		fmt.Fprintln(os.Stderr, "index:", err)
	}
	_, _ = rescanWiki()

	pages, _ := listPages()
	m := tui{allPages: pages, status: "↑/↓ select · / search · enter open · n new · r reindex · q quit"}

	l := list.New(itemsFromPages(pages), list.NewDefaultDelegate(), tuiSidebar, 20)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.Title = "mem wiki"
	l.Styles.Title = tuiTitle
	l.KeyMap.Quit.SetEnabled(false)
	m.list = l

	m.search = textinput.New()
	m.search.Placeholder = "search the wiki…"
	m.search.Prompt = "/ "

	m.newName = textinput.New()
	m.newName.Placeholder = "new page title"
	m.newName.Prompt = "+ "

	m.vp = viewport.New(60, 20)
	return m
}

func itemsFromPages(pages []Page) []list.Item {
	items := make([]list.Item, 0, len(pages))
	for _, p := range pages {
		items = append(items, pageItem{p})
	}
	return items
}

func (m tui) Init() tea.Cmd {
	return m.loadPreview()
}

// --- helpers ---

func (m *tui) reload() tea.Cmd {
	pages, err := listPages()
	if err != nil {
		m.status = tuiErr.Render(err.Error())
		return nil
	}
	m.allPages = pages
	cmd := m.list.SetItems(itemsFromPages(pages))
	return tea.Batch(cmd, m.loadPreview())
}

func (m *tui) selected() (Page, bool) {
	if it, ok := m.list.SelectedItem().(pageItem); ok {
		return it.p, true
	}
	return Page{}, false
}

// loadPreview renders the currently selected page for the right pane.
func (m *tui) loadPreview() tea.Cmd {
	p, ok := m.selected()
	if !ok {
		return nil
	}
	slug := p.Slug
	return func() tea.Msg {
		page, err := loadPage(slug)
		if err != nil {
			return contentMsg{slug: slug, err: err}
		}
		rendered, err := renderMD(page.Body)
		if err != nil {
			return contentMsg{slug: slug, rendered: page.Body}
		}
		return contentMsg{slug: slug, rendered: rendered}
	}
}

// openView switches to view mode for the selected page.
func (m *tui) openView() tea.Cmd {
	p, ok := m.selected()
	if !ok {
		return nil
	}
	m.mode = modeView
	m.status = "viewing " + p.Slug + " · esc back · / search"
	slug := p.Slug
	return func() tea.Msg {
		page, err := loadPage(slug)
		if err != nil {
			return contentMsg{slug: slug, err: err}
		}
		rendered, err := renderMD(page.Body)
		if err != nil {
			return contentMsg{slug: slug, rendered: page.Body}
		}
		return contentMsg{slug: slug, rendered: rendered}
	}
}

func (m *tui) doReindex() tea.Cmd {
	return func() tea.Msg {
		n, err := rescanWiki()
		if err != nil {
			return statusMsg(tuiErr.Render("reindex: " + err.Error()))
		}
		return reindexedMsg{n}
	}
}

func (m *tui) doCreate() tea.Cmd {
	title := strings.TrimSpace(m.newName.Value())
	if title == "" {
		return nil
	}
	slug, err := slugForTitle(title)
	if err != nil {
		m.status = tuiErr.Render(err.Error())
		return nil
	}
	if _, err := upsertPage(slug, title, "", nil, "tui"); err != nil {
		m.status = tuiErr.Render(err.Error())
		return nil
	}
	m.mode = modeBrowse
	m.newName.Blur()
	m.newName.Reset()
	m.status = tuiOK.Render("created " + slug)
	return m.reload()
}

func (m *tui) backToBrowse() tea.Cmd {
	m.mode = modeBrowse
	m.search.Blur()
	m.search.Reset()
	cmd := m.list.SetItems(itemsFromPages(m.allPages))
	return tea.Batch(cmd, m.loadPreview())
}

// --- update ---

func (m tui) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		h := m.height - 3
		m.list.SetSize(tuiSidebar, h)
		m.vp.Width = m.width - tuiSidebar - 6
		m.vp.Height = h
		cmds = append(cmds, m.loadPreview())

	case contentMsg:
		if msg.err != nil {
			m.status = tuiErr.Render(msg.slug + ": " + msg.err.Error())
			break
		}
		m.vp.SetContent(msg.rendered)
		m.vp.GotoTop()

	case reindexedMsg:
		m.status = tuiOK.Render(fmt.Sprintf("reindex: %d change(s) synced", msg.n))
		cmds = append(cmds, m.reload())

	case tea.KeyMsg:
		if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c"))) {
			return m, tea.Quit
		}
		if msg.String() == "q" && m.mode != modeSearch && m.mode != modeNew {
			return m, tea.Quit
		}

		switch m.mode {
		case modeView:
			switch msg.String() {
			case "esc", "enter":
				m.mode = modeBrowse
				m.status = "↑/↓ select · / search · enter open · n new · r reindex · q quit"
				return m, m.loadPreview()
			}
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			cmds = append(cmds, cmd)

		case modeNew:
			switch msg.String() {
			case "esc":
				m.mode = modeBrowse
				m.newName.Blur()
				m.newName.Reset()
				return m, nil
			case "enter":
				return m, m.doCreate()
			}
			var cmd tea.Cmd
			m.newName, cmd = m.newName.Update(msg)
			cmds = append(cmds, cmd)

		case modeSearch:
			switch msg.String() {
			case "esc":
				return m, m.backToBrowse()
			case "enter":
				cmds = append(cmds, m.openView())
				return m, tea.Batch(cmds...)
			}
			// Everything else goes to the search input; live results follow.
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			cmds = append(cmds, cmd)
			if q := strings.TrimSpace(m.search.Value()); q != "" {
				hits := searchWiki(q, 50)
				items := make([]list.Item, 0, len(hits))
				for _, h := range hits {
					items = append(items, pageItem{p: Page{
						Slug: h.Slug, Title: h.Title, Tags: h.Tags, UpdatedAt: h.UpdatedAt,
					}})
				}
				if len(hits) == 0 {
					m.status = tuiWarn.Render("no matches · esc browse")
				} else {
					m.status = fmt.Sprintf("%d result(s) · enter open · esc browse", len(hits))
				}
				cmds = append(cmds, m.list.SetItems(items))
			}

		default: // browse
			switch msg.String() {
			case "/":
				m.mode = modeSearch
				m.search.Focus()
				m.status = "search · enter open top hit · esc browse"
				return m, textinput.Blink
			case "n":
				m.mode = modeNew
				m.newName.Focus()
				m.status = "new page · enter create · esc cancel"
				return m, textinput.Blink
			case "enter":
				return m, m.openView()
			case "r":
				m.status = "reindexing…"
				return m, m.doReindex()
			}
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			cmds = append(cmds, cmd, m.loadPreview())
		}
	}
	return m, tea.Batch(cmds...)
}

// --- view ---

func (m tui) View() string {
	if m.width == 0 {
		return "loading…"
	}
	h := m.height - 3
	if h < 5 {
		h = 5
	}
	m.list.SetHeight(h)
	m.list.SetWidth(tuiSidebar)

	var right string
	switch m.mode {
	case modeView:
		right = tuiPanel.Width(m.width - tuiSidebar - 5).Height(h).Render(m.vp.View())
	case modeSearch:
		right = tuiPanel.Width(m.width - tuiSidebar - 5).Height(3).
			Render(m.search.View() + "\n" + tuiMuted.Render("enter open top hit · esc browse"))
	case modeNew:
		right = tuiPanel.Width(m.width - tuiSidebar - 5).Height(3).
			Render(m.newName.View() + "\n" + tuiMuted.Render("enter create · esc cancel"))
	default:
		preview := "select a page to preview"
		if p, ok := m.selected(); ok {
			preview = "preview: " + p.Title + " — enter to open"
		}
		right = tuiPanel.Width(m.width - tuiSidebar - 5).Height(3).
			Render(tuiAccent.Render(preview) + "\n" + tuiMuted.Render("enter open · / search · n new"))
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top, m.list.View(), "  ", right)
	header := tuiTitle.Render(" mem wiki ") + tuiMuted.Render("  "+fmt.Sprintf("%d pages", len(m.allPages)))
	footer := tuiStatus.Render(m.status)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

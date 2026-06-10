package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Page identifies one app surface.
type Page int

const (
	PageHome Page = iota
	PageProjects
	PageSessions
	PageConfig
	PageSkills
	PageModels
	PageProviders
	PageMemory
)

// pageNames in rail order. Crons and Plugins join as they're built.
var pages = []struct {
	page Page
	name string
	key  string // quick-jump key (from home / with g prefix)
}{
	{PageHome, "home", "h"},
	{PageProjects, "projects", "p"},
	{PageSessions, "sessions", "s"},
	{PageConfig, "config", "c"},
	{PageSkills, "skills", "k"},
	{PageModels, "models", "m"},
	{PageProviders, "providers", "v"},
	{PageMemory, "memory", "y"},
}

// Action is what the app asks main to do after it exits.
type Action int

const (
	ActionQuit     Action = iota
	ActionOpenChat        // open a new chat (CWD = Result.Dir)
	ActionResume          // resume Result.SessionID
)

// Result returns the user's exit intent to main.
type Result struct {
	Action    Action
	Dir       string // ActionOpenChat: project directory ("" = current)
	SessionID string // ActionResume: session store id
}

// Model is the app shell: a side rail of pages + the active page's content.
type Model struct {
	width, height int
	active        Page
	result        Result
	quitting      bool

	// page state
	home      homeState
	projects  projectsState
	sessions  sessionsState
	config    configState
	skills    skillsState
	models    modelsState
	providers providersState
	memory    memoryState

	data        *Data // loaded app data (sessions, projects, config…)
	titledPolls int
	palette     palette
}

// New builds the app shell with loaded data.
func New(data *Data) *Model {
	m := &Model{data: data}
	m.home.init(data)
	m.projects.init(data)
	m.sessions.init(data)
	m.config.init(data)
	m.skills.init(data)
	m.models.init(data)
	m.providers.init(data)
	m.memory.init(data)
	return m
}

func (m *Model) Init() tea.Cmd {
	// Kick off background titling of untitled sessions with the small model,
	// and poll to refresh the view as titles land.
	if m.data.Store != nil && m.data.Titler != nil {
		m.data.Store.TitleUntitled(context.Background(), m.data.Titler, 60)
		return titleTick()
	}
	return nil
}

// titleRefreshMsg triggers a session-row reload (titles filled in the store).
type titleRefreshMsg struct{}

func titleTick() tea.Cmd {
	return tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg { return titleRefreshMsg{} })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case titleRefreshMsg:
		m.data.reloadSessions()
		m.titledPolls++
		// Poll for a while after launch (titles arrive over seconds), then stop.
		if m.titledPolls < 20 {
			return m, titleTick()
		}
		return m, nil
	case tea.KeyMsg:
		key := msg.String()
		// Command palette intercepts all keys while open.
		if m.palette.open {
			consumed, cmd := m.palette.update(m, key, msg.Runes)
			if consumed {
				return m, cmd
			}
		}
		switch key {
		case "ctrl+c":
			m.result = Result{Action: ActionQuit}
			m.quitting = true
			return m, tea.Quit
		case ":", "ctrl+k":
			m.palette.openPalette(m)
			return m, nil
		case "q":
			m.result = Result{Action: ActionQuit}
			m.quitting = true
			return m, tea.Quit
		case "tab", "shift+tab", "[", "]":
			d := 1
			if key == "shift+tab" || key == "[" {
				d = -1
			}
			m.cycle(d)
			return m, nil
		}
		// Page quick-jump: g then the page key (or the key alone from home).
		if p, ok := jumpKey(key, m.active); ok {
			m.active = p
			return m, nil
		}
		// Delegate to the active page.
		return m.updatePage(msg)
	}
	return m, nil
}

// cycle moves to the next/previous page in rail order.
func (m *Model) cycle(d int) {
	idx := 0
	for i, p := range pages {
		if p.page == m.active {
			idx = i
			break
		}
	}
	idx = (idx + d + len(pages)) % len(pages)
	m.active = pages[idx].page
}

// jumpKey maps a quick-jump key to a page. Plain letter keys jump only from
// pages that don't need text input (all of v1's pages are lists, so letters
// are safe; pages that later take input will consume keys before this).
func jumpKey(key string, _ Page) (Page, bool) {
	for _, p := range pages {
		if key == "g"+p.key || key == p.key {
			// 'g'-prefixed always jumps; bare letter jumps too in v1.
			if key == p.key && strings.ContainsAny(p.key, "jk") {
				return 0, false // j/k reserved for list movement
			}
			return p.page, true
		}
	}
	return 0, false
}

func (m *Model) updatePage(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.active {
	case PageHome:
		return m.home.update(m, msg)
	case PageProjects:
		return m.projects.update(m, msg)
	case PageSessions:
		return m.sessions.update(m, msg)
	case PageConfig:
		return m.config.update(m, msg)
	case PageSkills:
		return m.skills.update(m, msg)
	case PageModels:
		return m.models.update(m, msg)
	case PageProviders:
		return m.providers.update(m, msg)
	case PageMemory:
		return m.memory.update(m, msg)
	}
	return m, nil
}

// View renders rail + active page.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 {
		return "loading…"
	}
	rail := m.renderRail()
	railW := lipgloss.Width(rail)
	contentW := m.width - railW - 1
	if contentW < 20 {
		contentW = 20
	}
	content := m.renderPage(contentW, m.height-1)
	if m.palette.open {
		content = overlay(content, m.palette.view(contentW), contentW, m.height-1)
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, rail, " ", content)
	help := m.helpLine()
	return body + "\n" + help
}

// overlay places box over the top region of content (the palette focus), so the
// page stays visible beneath it for context.
func overlay(content, box string, w, h int) string {
	cl := strings.Split(content, "\n")
	bl := strings.Split(box, "\n")
	// Pad content to h lines so the box sits at a stable position.
	for len(cl) < h {
		cl = append(cl, "")
	}
	start := 1
	for i, line := range bl {
		row := start + i
		if row < len(cl) {
			cl[row] = line
		}
	}
	return strings.Join(cl, "\n")
}

// renderRail draws the left rail: pages, the active one highlighted.
func (m *Model) renderRail() string {
	var b strings.Builder
	b.WriteString(sTitle.Render(" eigen") + "\n")
	b.WriteString(sFaint.Render(" ────────") + "\n")
	for _, p := range pages {
		marker := "  "
		style := sRailIdle
		if p.page == m.active {
			marker = sAccent.Render("▎") + " "
			style = sRailActive
		}
		b.WriteString(marker + style.Render(p.name) + "\n")
	}
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	w := 0
	for _, l := range lines {
		if lw := lipgloss.Width(l); lw > w {
			w = lw
		}
	}
	if w < 12 {
		w = 12
	}
	col := lipgloss.NewStyle().Width(w).Height(m.height - 1)
	return col.Render(strings.Join(lines, "\n"))
}

func (m *Model) renderPage(w, h int) string {
	switch m.active {
	case PageHome:
		return m.home.view(m, w, h)
	case PageProjects:
		return m.projects.view(m, w, h)
	case PageSessions:
		return m.sessions.view(m, w, h)
	case PageConfig:
		return m.config.view(m, w, h)
	case PageSkills:
		return m.skills.view(m, w, h)
	case PageModels:
		return m.models.view(m, w, h)
	case PageProviders:
		return m.providers.view(m, w, h)
	case PageMemory:
		return m.memory.view(m, w, h)
	}
	return ""
}

func (m *Model) helpLine() string {
	base := " tab pages · j/k move · enter open · n new · : palette · q quit"
	return sFaint.Render(base)
}

// Run opens the app shell and returns the exit intent.
func Run(data *Data) (Result, error) {
	m := New(data)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return Result{Action: ActionQuit}, err
	}
	fm, ok := final.(*Model)
	if !ok {
		return Result{Action: ActionQuit}, fmt.Errorf("unexpected model type")
	}
	return fm.result, nil
}

package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/feed"
)

// Page identifies one app surface.
type Page int

const (
	PageHome Page = iota
	PageLive
	PageProjects
	PageSessions
	PageConfig
	PageSkills
	PageModels
	PageProviders
	PageMemory
	PageCrons
	PagePlugins
)

// pageNames in rail order.
var pages = []struct {
	page Page
	name string
	key  string // quick-jump key (from home / with g prefix)
}{
	{PageHome, "home", "h"},
	{PageLive, "live", "l"},
	{PageProjects, "projects", "p"},
	{PageSessions, "sessions", "s"},
	{PageConfig, "config", "c"},
	{PageSkills, "skills", "k"},
	{PageModels, "models", "m"},
	{PageProviders, "providers", "v"},
	{PageMemory, "memory", "y"},
	{PageCrons, "crons", "r"},
	{PagePlugins, "plugins", "x"},
}

// Action is what the app asks main to do after it exits.
type Action int

const (
	ActionQuit     Action = iota
	ActionOpenChat        // open a new chat (CWD = Result.Dir)
	ActionResume          // resume Result.SessionID
	ActionAttach          // attach a view to daemon session Result.SessionID
)

// Result returns the user's exit intent to main.
type Result struct {
	Action    Action
	Dir       string // ActionOpenChat: project directory ("" = current)
	SessionID string // ActionResume: session store id; ActionAttach: daemon id
	Task      string // ActionOpenChat: initial task (feed starters); "" = blank chat
}

// Model is the app shell: a side rail of pages + the active page's content.
type Model struct {
	width, height int
	active        Page
	result        Result
	quitting      bool

	// page state
	home      homeState
	live      liveState
	projects  projectsState
	sessions  sessionsState
	config    configState
	skills    skillsState
	models    modelsState
	providers providersState
	memory    memoryState
	crons     cronsState
	plugins   pluginsState

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
	m.crons.init(data)
	m.plugins.init(data)
	return m
}

func (m *Model) Init() tea.Cmd {
	// Kick off background titling of untitled sessions with the small model,
	// and poll to refresh the view as titles land.
	var cmds []tea.Cmd
	if m.data.Store != nil && m.data.Titler != nil {
		m.data.Store.TitleUntitled(context.Background(), m.data.Titler, 60)
		cmds = append(cmds, titleTick())
	}
	if m.data.Daemon != nil {
		cmds = append(cmds, livePoll())
	}
	// Refresh the proactive feed in the background when the cache is stale
	// (instant render from cache either way).
	if !m.data.FeedFresh {
		dirs := m.data.projectDirs()
		cmds = append(cmds, func() tea.Msg { return feedMsg{feed.Scan(dirs)} })
	}
	return tea.Batch(cmds...)
}

// feedMsg delivers a fresh feed scan.
type feedMsg struct{ f feed.Feed }

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
	case livePollMsg:
		m.data.refreshLive()
		return m, livePoll()
	case feedMsg:
		m.data.Feed, m.data.FeedFresh = msg.f, true
		m.home.syncFeed(m.data)
		return m, nil
	case consolidateDoneMsg:
		m.memory.consoling = false
		m.memory.loaded = false
		m.memory.load(m.data)
		if msg.err != nil {
			m.memory.status = "consolidate failed: " + msg.err.Error()
		} else {
			m.memory.status = "consolidated ✓ (backup kept)"
		}
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
		if key == "ctrl+c" {
			m.result = Result{Action: ActionQuit}
			m.quitting = true
			return m, tea.Quit
		}
		// A page in text-entry mode (config editor) gets every key except
		// ctrl+c — q/:/tab/letters must type, not quit/jump.
		if m.capturingInput() {
			return m.updatePage(msg)
		}
		switch key {
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

// capturingInput reports whether the active page is in a text-entry or
// confirmation mode (so bare letters must reach it rather than jump pages).
func (m *Model) capturingInput() bool {
	switch m.active {
	case PageConfig:
		return m.config.editing || m.config.picking
	case PageMemory:
		return m.memory.confirm
	}
	return false
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
	case PageLive:
		return m.live.update(m, msg)
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
	case PageCrons:
		return m.crons.update(m, msg)
	case PagePlugins:
		return m.plugins.update(m, msg)
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
	// Live daemon sessions: the rail shows each with a status glyph so
	// concurrent work is visible at a glance (attach from the live page).
	if len(m.data.Live) > 0 {
		b.WriteString(sFaint.Render(" ────────") + "\n")
		b.WriteString(sFaint.Render(" live") + "\n")
		for i, in := range m.data.Live {
			if i >= 6 {
				b.WriteString(sFaint.Render(fmt.Sprintf("  +%d more", len(m.data.Live)-i)) + "\n")
				break
			}
			b.WriteString("  " + liveGlyph(in.Status) + " " + sRailIdle.Render(liveLabel(in)) + "\n")
		}
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

// liveGlyph maps a daemon session status to a small colored indicator.
func liveGlyph(s daemon.Status) string {
	switch s {
	case daemon.StatusWorking:
		return sOk.Render("●") // green: actively working
	case daemon.StatusApproval:
		return sWarn.Render("◆") // amber: blocked on an approval
	case daemon.StatusError:
		return sErr.Render("✗")
	default:
		return sFaint.Render("○") // idle
	}
}

// liveLabel is the short rail label for a live session.
func liveLabel(in daemon.SessionInfo) string {
	name := in.Title
	if name == "" {
		name = filepath.Base(in.Dir)
	}
	if name == "" || name == "." || name == "/" {
		name = in.ID
	}
	return truncate(name, 14)
}

func (m *Model) renderPage(w, h int) string {
	switch m.active {
	case PageHome:
		return m.home.view(m, w, h)
	case PageLive:
		return m.live.view(m, w, h)
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
	case PageCrons:
		return m.crons.view(m, w, h)
	case PagePlugins:
		return m.plugins.view(m, w, h)
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

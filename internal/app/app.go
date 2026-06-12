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
	// (instant render from cache either way), and keep it fresh while the app
	// stays open (periodic tick → rescan).
	if !m.data.FeedFresh {
		cmds = append(cmds, m.scanFeed())
	}
	cmds = append(cmds, feedTick())
	return tea.Batch(cmds...)
}

// scanFeed rescans the feed in the background (with the small-model suggester
// when available) and delivers a feedMsg.
func (m *Model) scanFeed() tea.Cmd {
	dirs := m.data.projectDirs()
	suggest := m.data.suggester()
	return func() tea.Msg { return feedMsg{feed.Scan(dirs, suggest)} }
}

// feedRefreshEvery is how often an open app rescans the feed (matches the
// cache TTL: fresh enough to be useful, cheap enough to never matter).
const feedRefreshEvery = 10 * time.Minute

// feedTickMsg triggers a periodic feed rescan while the app is open.
type feedTickMsg struct{}

func feedTick() tea.Cmd {
	return tea.Tick(feedRefreshEvery, func(time.Time) tea.Msg { return feedTickMsg{} })
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
	case feedTickMsg:
		// Periodic refresh while the app is open: rescan in the background and
		// re-arm the tick.
		return m, tea.Batch(m.scanFeed(), feedTick())
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
		// Home's feed consumes x (remove) when the cursor is on a feed item —
		// it must not jump to the plugins page from there (g+x still jumps).
		if m.active == PageHome && key == "x" && m.home.list.cursor < m.home.feedN {
			return m.updatePage(msg)
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

// View renders the framed shell: title bar, bordered rail + content (+ optional
// right inspector), and a status bar — all positioned by computeLayout so
// rendering and (Wave 2) hit-testing share one geometry.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 {
		return "loading…"
	}
	l := m.computeLayout()

	title := m.renderTitleBar(l)
	rail := m.renderRailBox(l)
	content := m.renderContentBox(l)
	status := m.renderStatusBar(l)

	// Compose the body row: rail | gutter | content | gutter | inspector.
	gut := ""
	if l.bp != bpNarrow {
		gut = " "
	}
	cols := []string{rail}
	if gut != "" {
		cols = append(cols, gut)
	}
	cols = append(cols, content)
	if !l.inspector.empty() {
		if gut != "" {
			cols = append(cols, gut)
		}
		cols = append(cols, m.renderInspectorBox(l))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)

	out := title + "\n" + body
	if m.palette.open {
		out = m.overlayPalette(out, l)
	}
	return out + "\n" + status
}

// overlayPalette draws the command palette over the composed view (near the
// top of the content panel) so the shell stays visible beneath it.
func (m *Model) overlayPalette(view string, l appLayout) string {
	box := m.palette.view(l.inner.w)
	lines := strings.Split(view, "\n")
	bl := strings.Split(box, "\n")
	start := l.title.h + 1
	for i, bln := range bl {
		row := start + i
		if row >= 0 && row < len(lines) {
			// Overlay at the content's x so it sits inside the panel.
			lines[row] = padLeft(bln, l.content.x+1)
		}
	}
	return strings.Join(lines, "\n")
}

// padLeft left-pads s with n spaces (for placing an overlay at a column).
func padLeft(s string, n int) string {
	if n <= 0 {
		return s
	}
	return strings.Repeat(" ", n) + s
}

// renderTitleBar draws the top bar: product mark + the active page breadcrumb
// on the left, quick context on the right.
func (m *Model) renderTitleBar(l appLayout) string {
	left := sTitle.Render(" eigen ") + sFaint.Render("›") + " " + sText.Render(m.activeName())
	right := sFaint.Render(m.titleStats() + " ")
	gap := l.title.w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// activeName is the active page's display name.
func (m *Model) activeName() string {
	for _, p := range pages {
		if p.page == m.active {
			return p.name
		}
	}
	return ""
}

// titleStats is a compact right-aligned context line for the title bar.
func (m *Model) titleStats() string {
	d := m.data
	live := ""
	if n := len(d.Live); n > 0 {
		live = fmt.Sprintf("%d live · ", n)
	}
	return fmt.Sprintf("%s%d sessions", live, len(d.Sessions))
}

// renderRailBox renders the bordered rail into its outer rect.
func (m *Model) renderRailBox(l appLayout) string {
	inner := m.railContent(l.railInner)
	style := sRailBox.Width(l.railInner.w).Height(l.railInner.h)
	if l.bp == bpNarrow {
		// Compact: drop the border, keep the column.
		return lipgloss.NewStyle().Width(l.rail.w).Height(l.rail.h).Render(inner)
	}
	return style.Render(inner)
}

// renderContentBox renders the active page into the bordered content rect.
func (m *Model) renderContentBox(l appLayout) string {
	page := m.renderPage(l.inner.w, l.inner.h)
	style := sContentBox.Width(l.inner.w).Height(l.inner.h)
	if l.bp == bpNarrow {
		return lipgloss.NewStyle().Width(l.content.w).Height(l.content.h).Render(page)
	}
	return style.Render(page)
}

// renderInspectorBox renders the right inspector (wide breakpoint).
func (m *Model) renderInspectorBox(l appLayout) string {
	inner := m.inspectorContent(l.inspInner)
	return sContentBox.Width(l.inspInner.w).Height(l.inspInner.h).Render(inner)
}

// renderStatusBar draws the bottom help/status bar.
func (m *Model) renderStatusBar(l appLayout) string {
	help := m.helpLine()
	w := lipgloss.Width(help)
	if w < l.status.w {
		help += sFaint.Render(strings.Repeat(" ", l.status.w-w))
	}
	return help
}

// railContent builds the rail's inner text (page list + live sessions),
// trimmed to the inner rect. Rows align with railRowAt for hit-testing.
func (m *Model) railContent(r rect) string {
	var b strings.Builder
	for _, p := range pages {
		marker := "  "
		style := sRailIdle
		if p.page == m.active {
			marker = sAccent.Render("▎") + " "
			style = sRailActive
		}
		b.WriteString(marker + style.Render(truncate(p.name, r.w-2)) + "\n")
	}
	if len(m.data.Live) > 0 {
		b.WriteString(sFaint.Render("─── live") + "\n")
		for i, in := range m.data.Live {
			if i >= railLiveMax {
				b.WriteString(sFaint.Render(fmt.Sprintf("  +%d more", len(m.data.Live)-i)) + "\n")
				break
			}
			b.WriteString("  " + liveGlyph(in.Status) + " " + sRailIdle.Render(liveLabel(in)) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// railLiveMax caps how many live sessions the rail lists.
const railLiveMax = 6

// inspectorContent renders the right inspector (wide breakpoint): a contextual
// detail of the active page's selection. v1 is a calm placeholder; Wave 4 fills
// it with real per-selection detail.
func (m *Model) inspectorContent(r rect) string {
	return sFaint.Render("details") + "\n" + sFaint.Render(strings.Repeat("─", min(r.w, 20))) + "\n" +
		sDim.Render(wrapText("select an item to inspect it here", r.w))
}

// wrapText wraps s to width w (greedy, space-separated).
func wrapText(s string, w int) string {
	if w <= 0 {
		return s
	}
	var out, line strings.Builder
	col := 0
	for _, word := range strings.Fields(s) {
		wl := lipgloss.Width(word)
		if col > 0 && col+1+wl > w {
			out.WriteString(line.String() + "\n")
			line.Reset()
			col = 0
		}
		if col > 0 {
			line.WriteString(" ")
			col++
		}
		line.WriteString(word)
		col += wl
	}
	out.WriteString(line.String())
	return out.String()
}
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

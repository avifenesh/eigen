package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/feed"
	pluginpkg "github.com/avifenesh/eigen/internal/plugin"
	"github.com/avifenesh/eigen/internal/remote"
)

// Page identifies one app surface.
type Page int

const (
	PageHome Page = iota
	PageLive
	PageProjects
	PageMachines
	PageSessions
	PageConfig
	PageSkills
	PageModels
	PageProviders
	PageObserve
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
	{PageMachines, "machines", "e"},
	{PageSessions, "sessions", "s"},
	{PageConfig, "config", "c"},
	{PageSkills, "skills", "k"},
	{PageModels, "models", "m"},
	{PageProviders, "providers", "v"},
	{PageObserve, "observe", "o"},
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
	ActionRemote          // open a session on a REMOTE machine (Result.Host)
)

// Result returns the user's exit intent to main.
type Result struct {
	Action    Action
	Dir       string // ActionOpenChat: project directory ("" = current)
	SessionID string // ActionResume: store id; ActionAttach: daemon id; ActionRemote: remote session id ("" = newest/new)
	Task      string // ActionOpenChat: initial task (feed starters); "" = blank chat
	Host      string // ActionRemote: `eigen --remote` target (saved name or user@host)
}

// Model is the app shell: a side rail of pages + the active page's content.
type Model struct {
	width, height int
	active        Page
	contentScroll int // generic vertical scroll over the rendered content page
	result        Result
	quitting      bool

	// page state
	home      homeState
	live      liveState
	projects  projectsState
	machines  machinesState
	sessions  sessionsState
	config    configState
	skills    skillsState
	models    modelsState
	providers providersState
	observe   observeState
	memory    memoryState
	crons     cronsState
	plugins   pluginsState

	data        *Data // loaded app data (sessions, projects, config…)
	titledPolls int
	liveSpin    int // animation frame for working-session glyphs (advances on livePoll)
	palette     palette
}

// New builds the app shell with loaded data.
func New(data *Data) *Model { return NewAt(data, PageHome) }

// NewAt builds the app shell and selects an initial page. Unknown zero-value
// callers keep landing on Home, while chat slash commands can return directly
// to a product surface such as Plugins.
func NewAt(data *Data, initial Page) *Model {
	m := &Model{data: data}
	if isKnownPage(initial) {
		m.setActive(initial)
	}
	m.home.init(data)
	m.projects.init(data)
	m.machines.init(data)
	m.sessions.init(data)
	m.config.init(data)
	m.skills.init(data)
	m.models.init(data)
	m.providers.init(data)
	m.observe.init(data)
	m.memory.init(data)
	m.crons.init(data)
	m.plugins.init(data)
	return m
}

func isKnownPage(page Page) bool {
	for _, p := range pages {
		if p.page == page {
			return true
		}
	}
	return false
}

// PageByName resolves a stable app-page name/alias for integrations that do
// not import the Page enum directly (for example, the TUI result payload).
func PageByName(name string) (Page, bool) {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return PageHome, true
	}
	for _, p := range pages {
		if name == p.name || name == p.key {
			return p.page, true
		}
	}
	switch name {
	case "plugin", "plugins", "market", "marketplace", "extension", "extensions", "wiring", "hook", "hooks":
		return PagePlugins, true
	case "observe", "observability", "obs", "usage", "telemetry", "errors":
		return PageObserve, true
	}
	return PageHome, false
}

func newAtPageName(data *Data, pageName string) *Model {
	page, _ := PageByName(pageName)
	m := NewAt(data, page)
	m.applyInitialPageName(pageName)
	return m
}

func (m *Model) applyInitialPageName(pageName string) {
	name := strings.TrimSpace(strings.ToLower(pageName))
	if m.active != PagePlugins {
		return
	}
	switch name {
	case "market", "marketplace":
		m.plugins.setTab(pluginsTabMarketplace)
	case "extension", "extensions", "wiring":
		m.plugins.setTab(pluginsTabExtensions)
	case "hook", "hooks":
		m.plugins.setTab(pluginsTabHooks)
	}
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
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case livePollMsg:
		m.data.refreshLive()
		m.liveSpin++ // advance the working-session animation
		return m, livePoll()
	case feedMsg:
		m.data.Feed, m.data.FeedFresh = msg.f, true
		m.home.syncFeed(m.data)
		return m, nil
	case machineSessionsMsg:
		// Deliver a remote machine's session list to the Machines drill-in
		// (ignore if the user already navigated away from that machine).
		if m.machines.inside && m.machines.mach == msg.mach {
			m.machines.loading = false
			m.machines.sessions = msg.sessions
			m.machines.loadErr = msg.err
			m.machines.inner.count = len(msg.sessions)
		}
		return m, nil
	case machineInstallMsg:
		m.machines.installing = false
		if msg.err != "" {
			m.machines.installMsg = "install failed: " + msg.err
			if m.machines.inside {
				m.machines.loading = false
				m.machines.loadErr = "install failed: " + msg.err
			}
			return m, nil
		}
		v := msg.ver
		if v == "" {
			v = "eigen"
		}
		m.machines.installMsg = "installed " + v + " ✓ — enter to see its sessions"
		m.data.Machines = remote.Machines() // refresh row state
		// If we're inside the drill-in, re-fetch the now-installed machine's
		// sessions so they appear without leaving the view.
		if m.machines.inside && m.machines.mach == msg.mach && msg.mach < len(m.data.Machines) {
			m.machines.loading = true
			m.machines.loadErr = ""
			return m, fetchMachineSessions(msg.mach, m.data.Machines[msg.mach].SSH)
		}
		return m, nil
	case feedTickMsg:
		// Periodic refresh while the app is open: rescan in the background and
		// re-arm the tick.
		return m, tea.Batch(m.scanFeed(), feedTick())
	case installDoneMsg:
		switch msg.page {
		case PageSkills:
			m.skills.prompt.finish(msg.status)
			if m.data.Skills != nil {
				m.data.Skills.Rescan()
				m.skills.list.count = m.data.Skills.Len()
			}
		case PagePlugins:
			m.plugins.prompt.finish(msg.status)
			if msg.kind == "plugin" {
				m.plugins.catalogSelected = nil
			}
			if msg.tab != 0 || msg.kind == "plugin" {
				m.plugins.tab = msg.tab
			}
			m.plugins.loaded = false
			m.plugins.load()
		}
		return m, nil
	case marketplaceRefreshDoneMsg:
		m.plugins.prompt.finish(msg.status)
		m.plugins.catalogMarket = msg.marketName
		m.plugins.catalog = append([]pluginpkg.PluginEntry(nil), msg.catalog...)
		m.plugins.catalogSelected = nil
		m.plugins.catalogPreview = nil
		m.plugins.catalogPreviewKey = ""
		m.plugins.err = ""
		m.plugins.loaded = false
		m.plugins.load()
		if len(msg.catalog) > len(m.plugins.catalog) && strings.HasPrefix(msg.status, "refreshed ") {
			m.plugins.prompt.status = fmt.Sprintf("%s, %d available", msg.status, len(m.plugins.catalog))
		}
		m.plugins.catalogList.count = len(m.plugins.catalog)
		m.plugins.catalogList.cursor, m.plugins.catalogList.top = 0, 0
		m.plugins.catalogFocus = msg.focus && len(m.plugins.catalog) > 0
		return m, nil
	case pluginPreviewDoneMsg:
		m.plugins.prompt.finish("")
		if msg.err != "" {
			m.plugins.err = msg.err
			m.plugins.catalogPreview = nil
			m.plugins.catalogPreviewKey = ""
			return m, nil
		}
		m.plugins.catalogPreviewKey = msg.key
		m.plugins.catalogPreview = msg.preview
		m.plugins.err = ""
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
		if m.handleContentScrollKey(key) {
			return m, nil
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
			m.setActive(p)
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
		return m.memory.confirm || m.memory.open
	case PageSessions:
		// Type-to-search captures everything (typing "q" extends the query).
		return m.sessions.filter.searching || m.sessions.confirmDel
	case PagePlugins:
		return m.plugins.prompt.active || m.plugins.confirm.active || m.plugins.catalogFocus
	case PageSkills:
		return m.skills.prompt.active
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
	m.setActive(pages[idx].page)
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

func (m *Model) handleContentScrollKey(key string) bool {
	l := m.computeLayout()
	page := m.renderPage(l.inner.w, l.inner.h)
	pageLines := splitRenderableLines(page)
	maxScroll := max(0, len(pageLines)-l.inner.h)
	halfPage := max(1, l.inner.h/2)
	switch key {
	case "pgdown", "ctrl+d", "ctrl+f":
		m.contentScroll = min(maxScroll, m.contentScroll+halfPage)
	case "pgup", "ctrl+u", "ctrl+b":
		m.contentScroll = max(0, m.contentScroll-halfPage)
	case "home":
		m.contentScroll = 0
	case "end":
		m.contentScroll = maxScroll
	default:
		return false
	}
	return true
}

func (m *Model) scrollContent(delta int) bool {
	l := m.computeLayout()
	page := m.renderPage(l.inner.w, l.inner.h)
	pageLines := splitRenderableLines(page)
	maxScroll := max(0, len(pageLines)-l.inner.h)
	before := m.contentScroll
	m.contentScroll = min(max(0, m.contentScroll+delta), maxScroll)
	return m.contentScroll != before
}

// handleMouse routes a mouse event through the shell hit map. Wave 2 handles
// global chrome (rail pages, live entries); content/inspector clicks delegate
// to the active page (page-local row hits land in Wave 3). Wheel is routed by
// region. Only left-button press and wheel are acted on; motion is ignored.
func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// While the palette is open, ignore stray clicks (it's a modal overlay).
	if m.palette.open {
		return m, nil
	}
	h := m.hitTest(msg.X, msg.Y)
	// Wheel: scroll the page list when over the content, ignored elsewhere.
	if tea.MouseEvent(msg).IsWheel() {
		if h.region == hitContent {
			return m.contentWheel(msg)
		}
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress {
		return m, nil
	}
	switch h.region {
	case hitRail:
		m.setActive(h.page)
		return m, nil
	case hitRailLive:
		// Click a live session in the rail → attach a view to it.
		m.result = Result{Action: ActionAttach, SessionID: h.liveID}
		m.quitting = true
		return m, tea.Quit
	case hitContent:
		// Page-local row click: the active page maps content-local coords to
		// an item (select, or open if already selected). Account for the generic
		// content scroll because pages record click maps against their full view.
		if cmd, handled := m.contentClick(h.localX, h.localY+m.contentScroll); handled {
			return m, cmd
		}
		return m, nil
	}
	return m, nil
}

// contentClick dispatches a content-local click to the active page's click
// handler (pages own their row geometry via the clickMap recorded in view()).
// Returns (cmd, handled); handled=false when the page has no click target there.
func (m *Model) contentClick(localX, localY int) (tea.Cmd, bool) {
	switch m.active {
	case PageHome:
		return m.home.clickAt(m, localY)
	case PageSessions:
		return m.sessions.clickAt(m, localY)
	case PageProjects:
		return m.projects.clickAt(m, localY)
	case PageMachines:
		return m.machines.clickAt(m, localY)
	case PageLive:
		return m.live.clickAt(m, localY)
	case PageConfig:
		return m.config.clickAt(m, localY)
	case PageMemory:
		return m.memory.clickAt(m, localY)
	case PagePlugins:
		return m.plugins.clickAt(m, localX, localY)
	}
	return nil, false
}

// contentWheel scrolls the rendered content first. If the page fits (no generic
// scroll movement), fall back to the page's list movement so legacy list-only
// pages still respond to wheel gestures.
func (m *Model) contentWheel(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	delta := 3
	fallback := "j"
	if msg.Button == tea.MouseButtonWheelUp {
		delta = -3
		fallback = "k"
	}
	if m.scrollContent(delta) {
		return m, nil
	}
	return m.updatePage(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(fallback)})
}

func (m *Model) updatePage(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.active {
	case PageHome:
		return m.home.update(m, msg)
	case PageLive:
		return m.live.update(m, msg)
	case PageProjects:
		return m.projects.update(m, msg)
	case PageMachines:
		return m.machines.update(m, msg)
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
	case PageObserve:
		return m.observe.update(m, msg)
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

func (m *Model) setActive(p Page) {
	if m.active != p {
		m.active = p
		m.contentScroll = 0
	}
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
	style := sRailBox.Width(l.railInner.w + sContentPadH).Height(l.railInner.h)
	if l.bp == bpNarrow {
		// Compact: drop the border, keep the column.
		return lipgloss.NewStyle().Width(l.rail.w).Height(l.rail.h).Render(inner)
	}
	return style.Render(inner)
}

// renderContentBox renders the active page into the bordered content rect.
// lipgloss .Width() is padding-INCLUSIVE (border excluded), so the box width is
// the inner content width + the horizontal padding; the page itself is rendered
// at the true content width (l.inner.w) so its own width math (rules, rows) is
// exact and never wraps against the gutter.
func (m *Model) renderContentBox(l appLayout) string {
	page := clipTextWindow(m.renderPage(l.inner.w, l.inner.h), l.inner.h, m.contentScroll)
	style := sContentBox.Width(l.inner.w + sContentPadH).Height(l.inner.h)
	if l.bp == bpNarrow {
		return lipgloss.NewStyle().Width(l.content.w).Height(l.content.h).Render(page)
	}
	return style.Render(page)
}

// renderInspectorBox renders the right inspector (wide breakpoint).
func splitRenderableLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

func clipTextHeight(s string, h int) string { return clipTextWindow(s, h, 0) }

func clipTextWindow(s string, h, scroll int) string {
	if h <= 0 || s == "" {
		return ""
	}
	trail := strings.HasSuffix(s, "\n")
	lines := splitRenderableLines(s)
	if len(lines) <= h {
		return s
	}
	maxScroll := max(0, len(lines)-h)
	if scroll < 0 {
		scroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	end := min(len(lines), scroll+h)
	out := strings.Join(lines[scroll:end], "\n")
	if trail && end == len(lines) {
		out += "\n"
	}
	return out
}

func (m *Model) renderInspectorBox(l appLayout) string {
	inner := m.inspectorDetail(l.inspInner.w)
	return sContentBox.Width(l.inspInner.w + sContentPadH).Height(l.inspInner.h).Render(inner)
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
			// Active page: the clay Focus bar — the SAME "you are here" / active
			// treatment as everywhere else (selection ▎, active session). Blue
			// is reserved for brand + structure, not "active".
			marker = lipgloss.NewStyle().Foreground(cFocus).Render("▎") + " "
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
			b.WriteString("  " + liveGlyph(in.Status, m.liveSpin) + " " + sRailIdle.Render(liveLabel(in)) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// railLiveMax caps how many live sessions the rail lists.
const railLiveMax = 6

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

// liveGlyph renders a session's status in the app rail/live page. WORKING shows
// a breathing λ (eigen's mark) — a brightness pulse on the loud working color,
// advanced by frame — so an active session reads as alive and on-brand, not a
// static dot. The other states use the shared theme.Status* glyphs (width-1,
// matching the chat's status language exactly).
func liveGlyph(s daemon.Status, frame int) string {
	switch s {
	case daemon.StatusWorking:
		// Brightness pulse over the working ramp (the app polls ~1.2s, so a
		// slow breath): dim → working → bright → working → loop.
		ramp := theme.WorkingRamp
		return lipgloss.NewStyle().Foreground(ramp[frame%len(ramp)]).Bold(true).Render("λ")
	case daemon.StatusApproval:
		return sWarn.Render(theme.StatusApproval)
	case daemon.StatusError:
		return sErr.Render(theme.StatusError)
	default:
		return sFaint.Render(theme.StatusIdle)
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
	case PageMachines:
		return m.machines.view(m, w, h)
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
	case PageObserve:
		return m.observe.view(m, w, h)
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
	base := " tab pages · j/k move · pg scroll · enter open · n new · : palette · q quit"
	return sFaint.Render(base)
}

// Run opens the app shell and returns the exit intent.
func Run(data *Data) (Result, error) { return RunAt(data, PageHome) }

// RunPage opens the app shell at a named page. Unknown names gracefully land on
// Home so slash-command navigation never bricks the window.
func RunPage(data *Data, pageName string) (Result, error) {
	return runModel(newAtPageName(data, pageName))
}

// RunAt opens the app shell at an initial page and returns the exit intent.
func RunAt(data *Data, initial Page) (Result, error) {
	return runModel(NewAt(data, initial))
}

func runModel(m *Model) (Result, error) {
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
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

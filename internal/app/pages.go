package app

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/dream"
	"github.com/avifenesh/eigen/internal/llm"
)

// configState shows AND edits the persistent defaults. Fields with a CLOSED
// option set (perm, booleans, provider/model catalogs) get a dropdown picker
// (enter) and in-place cycling (space); free-text fields (commands, numbers)
// get the inline editor.
type configState struct {
	list  list
	err   string // last validation error
	saved string // last saved key (flash feedback)

	// inline editor (free-text fields)
	editing bool
	input   string

	// dropdown picker (closed-set fields)
	picking  bool
	choices  []string
	pickIdx  int
	multiSel map[string]bool // multi-select state (route_providers)
	clicks   clickMap
}

func (c *configState) init(*Data) { c.list.count = len(config.Keys()) }

// optionsFor resolves a field's option set (static or dynamic).
func optionsFor(f config.Field) []string {
	if len(f.Options) > 0 {
		return f.Options
	}
	switch f.Dynamic {
	case "providers":
		var out []string
		for _, p := range Providers() {
			out = append(out, p.Name)
		}
		return out
	case "models":
		var out []string
		for _, m := range llm.Models() {
			out = append(out, m.ID)
		}
		return out
	}
	return nil
}

// setAndSave validates + persists one key, updating page feedback.
func (c *configState) setAndSave(m *Model, key, value string) bool {
	cfg := m.data.Config
	if err := config.Set(&cfg, key, value); err != nil {
		c.err = err.Error()
		return false
	}
	if err := config.Save(cfg); err != nil {
		c.err = err.Error()
		return false
	}
	m.data.Config = cfg
	c.err = ""
	c.saved = key
	return true
}

func (c *configState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	fields := config.Fields()
	c.list.count = len(fields)
	key := msg.String()

	// Dropdown picker captures keys while open.
	if c.picking {
		fld := fields[c.list.cursor]
		switch key {
		case "esc", "q":
			c.picking = false
		case "up", "k", "ctrl+p":
			if c.pickIdx > 0 {
				c.pickIdx--
			}
		case "down", "j", "ctrl+n":
			if c.pickIdx < len(c.choices)-1 {
				c.pickIdx++
			}
		case " ", "space":
			if fld.Multi { // toggle membership, stay open
				v := c.choices[c.pickIdx]
				c.multiSel[v] = !c.multiSel[v]
			}
		case "enter":
			if fld.Multi {
				var sel []string
				for _, v := range c.choices { // option order, not map order
					if c.multiSel[v] {
						sel = append(sel, v)
					}
				}
				if c.setAndSave(m, fld.Key, strings.Join(sel, " ")) {
					c.picking = false
				}
			} else {
				if c.setAndSave(m, fld.Key, c.choices[c.pickIdx]) {
					c.picking = false
				}
			}
		}
		return m, nil
	}

	// Inline editor (free-text fields) captures keys while open.
	if c.editing {
		switch key {
		case "esc":
			c.editing, c.input, c.err = false, "", ""
		case "enter":
			if c.setAndSave(m, fields[c.list.cursor].Key, strings.TrimSpace(c.input)) {
				c.editing, c.input = false, ""
			}
		case "backspace":
			if len(c.input) > 0 {
				c.input = c.input[:len(c.input)-1]
			}
		default:
			if len(msg.Runes) > 0 {
				c.input += string(msg.Runes)
			} else if key == "space" || key == " " {
				c.input += " "
			}
		}
		return m, nil
	}

	if c.list.key(key, m.height-8) {
		c.saved, c.err = "", ""
		return m, nil
	}
	fld := fields[c.list.cursor]
	switch key {
	case "enter":
		opts := optionsFor(fld)
		if len(opts) == 0 { // free text → inline editor
			c.editing = true
			c.input = config.Get(m.data.Config, fld.Key)
			c.err, c.saved = "", ""
			return m, nil
		}
		// Closed set → dropdown, preselected on the current value.
		c.choices = opts
		c.pickIdx = 0
		cur := config.Get(m.data.Config, fld.Key)
		if fld.Multi {
			c.multiSel = map[string]bool{}
			for _, v := range strings.Fields(cur) {
				c.multiSel[v] = true
			}
		} else {
			for i, v := range opts {
				if v == cur {
					c.pickIdx = i
					break
				}
			}
		}
		c.picking = true
		c.err, c.saved = "", ""
	case " ", "space":
		// Cycle small static enums in place (gated→auto, true→false).
		if len(fld.Options) > 0 && !fld.Multi {
			cur := config.Get(m.data.Config, fld.Key)
			next := fld.Options[0]
			for i, v := range fld.Options {
				if v == cur {
					next = fld.Options[(i+1)%len(fld.Options)]
					break
				}
			}
			c.setAndSave(m, fld.Key, next)
		}
	}
	return m, nil
}

// clickAt handles a content-local click on the config page: select a field
// row, or open it (dropdown/editor — same as enter) if already selected.
// While editing/picking, clicks are ignored (those modes own the keyboard).
func (c *configState) clickAt(m *Model, localY int) (tea.Cmd, bool) {
	if c.editing || c.picking {
		return nil, false
	}
	idx, ok := c.clicks.at(localY)
	if !ok {
		return nil, false
	}
	if idx < 0 || idx >= len(config.Fields()) {
		return nil, false
	}
	if c.list.cursor == idx {
		// Second click on the selected field → activate it (reuse the enter path).
		_, cmd := c.update(m, tea.KeyMsg{Type: tea.KeyEnter})
		return cmd, true
	}
	c.list.cursor = idx
	c.err, c.saved = "", ""
	return nil, true
}

func (c *configState) view(m *Model, w, h int) string {
	out := pageTitle("config", config.Path(), w)
	fields := config.Fields()

	if c.picking {
		fld := fields[c.list.cursor]
		out += "  " + sText.Render(fld.Key) + "  " + sDim.Render(truncate(fld.Desc, w-len(fld.Key)-6)) + "\n\n"
		visible := h - 8
		if visible < 3 {
			visible = 3
		}
		start := 0
		if c.pickIdx >= visible {
			start = c.pickIdx - visible + 1
		}
		end := min(start+visible, len(c.choices))
		for i := start; i < end; i++ {
			v := c.choices[i]
			mark := "  "
			if fld.Multi {
				mark = sFaint.Render("○ ")
				if c.multiSel[v] {
					mark = sOk.Render("● ")
				}
			} else if v == config.Get(m.data.Config, fld.Key) {
				mark = sAccent.Render("· ")
			}
			out += row(i == c.pickIdx, mark+sText.Render(v)) + "\n"
		}
		if c.err != "" {
			out += "\n" + sErr.Render("  "+truncate(c.err, w-4)) + "\n"
		}
		if fld.Multi {
			out += "\n" + sFaint.Render("  space toggle · enter save selection · esc cancel")
		} else {
			out += "\n" + sFaint.Render("  enter choose · esc cancel")
		}
		return out
	}

	for i, f := range fields {
		v := config.Get(m.data.Config, f.Key)
		val := sText.Render(v)
		if v == "" {
			val = sFaint.Render("(unset)")
		}
		if c.editing && i == c.list.cursor {
			val = sAccent.Render(c.input + "▏")
		}
		line := sDim.Render(pad(f.Key, 16)) + val
		if i == c.list.cursor && c.saved == f.Key {
			line += sOk.Render("  ✓ saved")
		}
		if i == 0 {
			c.clicks.reset()
		}
		c.clicks.mark(lineCount(out), i)
		out += row(i == c.list.cursor, line) + "\n"
	}
	// Description pane for the selected field — what it means, what it takes.
	if c.list.cursor < len(fields) {
		f := fields[c.list.cursor]
		out += "\n" + sDim.Render("  "+wrapTo(f.Desc, w-4, "  ")) + "\n"
	}
	if c.err != "" {
		out += sErr.Render("  "+truncate(c.err, w-4)) + "\n"
	}
	switch {
	case c.editing:
		out += sFaint.Render("  enter save · esc cancel")
	default:
		f := fields[c.list.cursor]
		if len(optionsFor(f)) > 0 {
			if f.Multi {
				out += sFaint.Render("  enter pick providers · defaults for NEW sessions")
			} else if len(f.Options) > 0 {
				out += sFaint.Render("  space next value · enter dropdown · defaults for NEW sessions")
			} else {
				out += sFaint.Render("  enter dropdown · defaults for NEW sessions")
			}
		} else {
			out += sFaint.Render("  enter edit (free text) · defaults for NEW sessions")
		}
	}
	return out
}

// wrapTo wraps text to a width with a continuation indent (simple greedy wrap).
func wrapTo(s string, w int, indent string) string {
	if w < 20 {
		return truncate(s, w)
	}
	words := strings.Fields(s)
	var b strings.Builder
	line := 0
	for _, word := range words {
		if line > 0 && line+1+len(word) > w {
			b.WriteString("\n" + indent)
			line = 0
		} else if line > 0 {
			b.WriteString(" ")
			line++
		}
		b.WriteString(word)
		line += len(word)
	}
	return b.String()
}

// skillsState lists discovered skills with preview on enter, and installs new
// ones inline (i).
type skillsState struct {
	list    list
	preview string // body being previewed ("" = list view)
	name    string
	prompt  installPrompt
}

func (s *skillsState) init(d *Data) { s.list.count = d.Skills.Len() }

func (s *skillsState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if s.preview != "" {
		if key == "esc" || key == "backspace" || key == "enter" {
			s.preview, s.name = "", ""
		}
		return m, nil
	}
	// Inline install prompt active: capture text input. The install/scan can call a
	// model or fetch GitHub, so run it as a tea.Cmd and leave a visible busy line
	// instead of freezing the page.
	if s.prompt.active {
		if s.prompt.busy {
			return m, nil
		}
		if src, ok := s.prompt.key(key, msg.Runes); ok {
			s.prompt.startBusy("skill", "skill source", src, "installing skill "+src+" … (scanning + fetching)")
			data := m.data
			return m, func() tea.Msg {
				return installDoneMsg{page: PageSkills, kind: "skill", status: runSkillInstall(data, src)}
			}
		}
		return m, nil
	}
	if s.list.key(key, m.height-6) {
		return m, nil
	}
	switch key {
	case "i": // install a skill from a path or owner/repo[/sub][@ref]
		s.prompt.open("skill", "skill source (path or owner/repo[/sub][@ref]) [--force|--no-scan]")
	case "enter":
		skills := m.data.Skills.List()
		if s.list.cursor < len(skills) {
			sk := skills[s.list.cursor]
			if body, err := m.data.Skills.Body(sk.Name); err == nil {
				s.preview, s.name = body, sk.Name
			}
		}
	}
	return m, nil
}

func (s *skillsState) view(m *Model, w, h int) string {
	if s.preview != "" {
		out := pageTitle(s.name, "skill", w)
		lines := strings.Split(s.preview, "\n")
		limit := min(len(lines), h-5)
		for _, l := range lines[:limit] {
			out += "  " + sText.Render(truncate(l, w-4)) + "\n"
		}
		if limit < len(lines) {
			out += sFaint.Render(fmt.Sprintf("  ⋯ %d more lines", len(lines)-limit)) + "\n"
		}
		out += "\n" + sFaint.Render("  esc back")
		return out
	}
	skills := m.data.Skills.List()
	out := pageTitle("skills", fmt.Sprintf("%d discovered", len(skills)), w)
	if len(skills) == 0 {
		out += sFaint.Render("  none — add SKILL.md under ~/.eigen/skills/<name>/ or press i to install") + "\n"
		out += s.prompt.render()
		out += "\n" + sFaint.Render("  i install a skill")
		return out
	}
	visible := h - 5
	from, to := s.list.window(visible)
	for i := from; i < to; i++ {
		sk := skills[i]
		line := pad(sk.Name, 24) + sDim.Render(truncate(sk.Description, w-28))
		out += row(i == s.list.cursor, line) + "\n"
	}
	out += s.prompt.render()
	out += "\n" + sFaint.Render("  enter preview · i install (path or owner/repo[@ref], optional --force/--no-scan)")
	return out
}

// modelsState lists the catalog with availability + capability tags.
type modelsState struct {
	list list
	rows []ModelRow
}

func (s *modelsState) init(*Data) {
	s.rows = Models()
	s.list.count = len(s.rows)
}

func (s *modelsState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s.list.key(msg.String(), m.height-6)
	return m, nil
}

func (s *modelsState) view(m *Model, w, h int) string {
	out := pageTitle("models", fmt.Sprintf("%d in catalog", len(s.rows)), w)
	out += modelsSummaryLine(s.rows, w) + "\n\n"
	visible := h - 8
	from, to := s.list.window(visible)
	for i := from; i < to; i++ {
		r := s.rows[i]
		dot := sErr.Render("●")
		if r.Available {
			dot = sOk.Render("●")
		}
		win := fmt.Sprintf("%dk", r.Window/1000)
		line := fmt.Sprintf("%s %s %s %s %s",
			dot,
			pad(truncate(r.ID, 36), 36),
			sDim.Render(pad(r.Provider, 10)),
			sViolet.Render(pad(win, 6)),
			sFaint.Render(r.Tags))
		out += row(i == s.list.cursor, line) + "\n"
	}
	if s.list.cursor >= 0 && s.list.cursor < len(s.rows) {
		out += "\n" + modelSelectedDetail(s.rows[s.list.cursor], w) + "\n"
	}
	out += "\n" + sFaint.Render("  ● available (credentialed) · default set via /config model")
	return out
}

func modelsSummaryLine(rows []ModelRow, w int) string {
	var available, vision, search, reasoning int
	providers := map[string]bool{}
	for _, r := range rows {
		providers[r.Provider] = true
		if r.Available {
			available++
		}
		if strings.Contains(r.Tags, "vision") {
			vision++
		}
		if strings.Contains(r.Tags, "search") {
			search++
		}
		if strings.Contains(r.Tags, "reasoning") {
			reasoning++
		}
	}
	parts := []string{
		fmt.Sprintf("%d available", available),
		fmt.Sprintf("%d providers", len(providers)),
		fmt.Sprintf("%d reasoning", reasoning),
		fmt.Sprintf("%d vision", vision),
		fmt.Sprintf("%d search", search),
	}
	return sFaint.Render("  " + truncate(strings.Join(parts, "  ·  "), max(20, w-2)))
}

func modelSelectedDetail(r ModelRow, w int) string {
	line := fmt.Sprintf("selected: %s · %s · context %dk", r.Provider, r.ID, r.Window/1000)
	if r.Tags != "" {
		line += " · " + r.Tags
	}
	return sFaint.Render("  " + truncate(line, max(20, w-2)))
}

// providersState shows credential status per provider.
type providersState struct {
	list list
	rows []ProviderRow
}

func (s *providersState) init(*Data) {
	s.rows = Providers()
	s.list.count = len(s.rows)
}

func (s *providersState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s.list.key(msg.String(), m.height-6)
	return m, nil
}

func (s *providersState) view(m *Model, w, h int) string {
	out := pageTitle("providers", providerSubtitle(m.data), w)
	out += providersSummaryLine(s.rows, w) + "\n\n"
	from, to := s.list.window(h - 8)
	for i := from; i < to; i++ {
		r := s.rows[i]
		statusText := "missing"
		status := sErr.Render(statusText)
		if r.Available {
			statusText = "available"
			status = sOk.Render(statusText)
		}
		defaultModel := r.Default
		if defaultModel == "" {
			defaultModel = "(provider default)"
		}
		line := fmt.Sprintf("%s %s %s %s",
			pad(r.Name, 12),
			pad(statusText, 11),
			pad(fmt.Sprintf("%d models", r.Models), 10),
			"default: "+truncate(defaultModel, max(8, w-42)))
		line = strings.Replace(line, statusText, status, 1)
		out += row(i == s.list.cursor, line) + "\n"
	}
	if s.list.cursor >= 0 && s.list.cursor < len(s.rows) {
		out += "\n" + providerSelectedDetail(s.rows[s.list.cursor], w) + "\n"
	}
	out += "\n" + sFaint.Render("  credentials external · edit route_providers in config")
	return out
}

func providerSubtitle(d *Data) string {
	if d == nil || !d.Config.Route {
		return "route off"
	}
	if len(d.Config.RouteProviders) == 0 {
		return "route on · all credentialed providers"
	}
	return "route on · " + strings.Join(d.Config.RouteProviders, " ")
}

func providersSummaryLine(rows []ProviderRow, w int) string {
	var available, models int
	for _, r := range rows {
		models += r.Models
		if r.Available {
			available++
		}
	}
	parts := []string{fmt.Sprintf("%d/%d available", available, len(rows)), fmt.Sprintf("%d models", models)}
	return sFaint.Render("  " + truncate(strings.Join(parts, "  ·  "), max(20, w-2)))
}

func providerSelectedDetail(r ProviderRow, w int) string {
	status := "missing credentials"
	if r.Available {
		status = "available"
	}
	def := r.Default
	if def == "" {
		def = "provider default"
	}
	line := fmt.Sprintf("selected: %s · %s · %d model(s) · default %s", r.Name, status, r.Models, def)
	return sFaint.Render("  " + truncate(line, max(20, w-2)))
}

// memoryState shows global memory as selectable bullets: d deletes one
// (with confirm), c consolidates via the small model (async), j/k move.
// Project memory lives with each project.
type memoryState struct {
	list         list
	bullets      []string
	loaded       bool
	confirm      bool   // pending delete confirmation for the selected bullet
	status       string // transient feedback ("consolidating…", errors)
	consoling    bool   // consolidation running in the background
	open         bool   // full selected-note reader is open
	detailScroll int    // first visible wrapped detail line in the reader
	clicks       clickMap
}

func (s *memoryState) init(*Data) {}

func (s *memoryState) load(d *Data) {
	if s.loaded {
		return
	}
	s.bullets = nil
	if d.GlobalMem != nil {
		s.bullets = memoryBullets(d.GlobalMem.Read())
	}
	s.list.count = len(s.bullets)
	s.list.clamp()
	s.loaded = true
}

// memoryBullets splits a memory file into its top-level "- " bullets
// (continuation lines belong to the bullet above).
func memoryBullets(content string) []string {
	var bullets []string
	var cur strings.Builder
	flush := func() {
		if b := strings.TrimRight(cur.String(), "\n"); strings.TrimSpace(b) != "" {
			bullets = append(bullets, b)
		}
		cur.Reset()
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "- ") {
			flush()
		}
		if cur.Len() > 0 {
			cur.WriteByte('\n')
		}
		cur.WriteString(line)
	}
	flush()
	return bullets
}

// consolidateDoneMsg reports the background consolidation result.
type consolidateDoneMsg struct{ err error }

func (s *memoryState) selectedNote() string {
	if s.list.cursor < 0 || s.list.cursor >= len(s.bullets) {
		return ""
	}
	return s.bullets[s.list.cursor]
}

func memoryDetailLines(note string, w int) []string {
	lineW := w - 4
	if lineW < 12 {
		lineW = 12
	}
	var out []string
	for _, raw := range strings.Split(strings.TrimRight(note, "\n"), "\n") {
		if strings.TrimSpace(raw) == "" {
			out = append(out, "")
			continue
		}
		for _, ln := range strings.Split(wrapTo(raw, lineW, ""), "\n") {
			out = append(out, ln)
		}
	}
	if len(out) == 0 {
		out = []string{"(empty)"}
	}
	return out
}

func (s *memoryState) clampDetailScroll(lineN, visible int) {
	if visible < 1 {
		visible = 1
	}
	maxScroll := lineN - visible
	if maxScroll < 0 {
		maxScroll = 0
	}
	if s.detailScroll > maxScroll {
		s.detailScroll = maxScroll
	}
	if s.detailScroll < 0 {
		s.detailScroll = 0
	}
}

func (s *memoryState) scrollDetail(m *Model, delta int) {
	l := m.computeLayout()
	w, visible := l.inner.w, l.inner.h-5
	if w <= 0 {
		w = m.width
	}
	s.detailScroll += delta
	lines := memoryDetailLines(s.selectedNote(), w)
	s.clampDetailScroll(len(lines), visible)
}

func (s *memoryState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s.load(m.data)
	key := msg.String()

	if s.open {
		switch key {
		case "esc", "backspace", "enter", "q":
			s.open = false
			s.detailScroll = 0
		case "j", "down":
			s.scrollDetail(m, 1)
		case "k", "up":
			s.scrollDetail(m, -1)
		case "ctrl+d", "pgdown":
			s.scrollDetail(m, max(1, m.computeLayout().inner.h/2))
		case "ctrl+u", "pgup":
			s.scrollDetail(m, -max(1, m.computeLayout().inner.h/2))
		case "home":
			s.detailScroll = 0
		case "G", "end":
			s.detailScroll = 1 << 30
			s.scrollDetail(m, 0)
		}
		return m, nil
	}

	if s.confirm {
		switch key {
		case "y", "enter":
			s.confirm = false
			if err := s.deleteSelected(m.data); err != nil {
				s.status = "delete failed: " + err.Error()
			} else {
				s.status = "deleted (backup kept)"
				s.loaded = false
				s.load(m.data)
			}
		default:
			s.confirm = false
			s.status = ""
		}
		return m, nil
	}

	if s.list.key(key, m.height-7) {
		s.status = ""
		return m, nil
	}
	switch key {
	case "enter", " ", "space":
		if len(s.bullets) > 0 {
			s.open = true
			s.detailScroll = 0
			s.status = ""
		}
	case "d":
		if len(s.bullets) > 0 {
			s.confirm = true
		}
	case "C":
		// Consolidate via the small model (capital: c is the config page jump).
		if s.consoling {
			break
		}
		if m.data.Small == nil || m.data.GlobalMem == nil {
			s.status = "consolidation needs the small model (credentials missing?)"
			break
		}
		s.consoling = true
		s.status = "consolidating…"
		gm, small := m.data.GlobalMem, m.data.Small
		return m, func() tea.Msg {
			cur := gm.Read()
			out, err := dream.Consolidate(context.Background(), small, cur)
			if err != nil {
				return consolidateDoneMsg{err: err}
			}
			if _, err := gm.Snapshot(); err != nil {
				return consolidateDoneMsg{err: err}
			}
			return consolidateDoneMsg{err: gm.Rewrite(out)}
		}
	case "R":
		s.loaded = false
		s.load(m.data)
		s.status = ""
	}
	return m, nil
}

// deleteSelected removes the selected bullet, snapshots first, rewrites.
func (s *memoryState) deleteSelected(d *Data) error {
	if d.GlobalMem == nil || s.list.cursor >= len(s.bullets) {
		return fmt.Errorf("nothing selected")
	}
	if _, err := d.GlobalMem.Snapshot(); err != nil {
		return err
	}
	kept := make([]string, 0, len(s.bullets)-1)
	for i, b := range s.bullets {
		if i != s.list.cursor {
			kept = append(kept, b)
		}
	}
	return d.GlobalMem.Rewrite(strings.Join(kept, "\n") + "\n")
}

func (s *memoryState) view(m *Model, w, h int) string {
	s.load(m.data)
	out := pageTitle("memory", "global (cross-project)", w)
	if m.data.GlobalMem == nil {
		return out + sFaint.Render("  unavailable")
	}
	if len(s.bullets) == 0 {
		return out + sFaint.Render("  empty — global notes accumulate from sessions (scope=global)")
	}
	if s.open {
		return s.detailView(m, w, h)
	}
	s.clicks.reset()
	visible := h - 7
	from, to := s.list.window(visible)
	for i := from; i < to; i++ {
		// One line per bullet: first line, truncated.
		first := strings.TrimPrefix(strings.SplitN(s.bullets[i], "\n", 2)[0], "- ")
		s.clicks.mark(lineCount(out), i)
		out += row(i == s.list.cursor, sText.Render(truncate(first, w-6))) + "\n"
	}
	if s.list.cursor < len(s.bullets) {
		// Detail pane: the selected bullet, wrapped, a few lines.
		out += "\n"
		detail := s.bullets[s.list.cursor]
		lines := strings.Split(detail, "\n")
		for i, l := range lines {
			if i >= 3 {
				out += sFaint.Render(fmt.Sprintf("  ⋯ %d more lines", len(lines)-i)) + "\n"
				break
			}
			out += "  " + sDim.Render(truncate(l, w-4)) + "\n"
		}
	}
	switch {
	case s.confirm:
		out += "\n" + sErr.Render("  delete this note? y confirm · any other key cancels")
	case s.status != "":
		out += "\n" + sWarn.Render("  "+s.status)
	default:
		out += "\n" + sFaint.Render("  enter read note · d delete · C consolidate · R refresh")
	}
	return out
}

// clickAt selects a memory row; clicking the already-selected note opens the
// full reader, matching enter.
func (s *memoryState) clickAt(_ *Model, localY int) (tea.Cmd, bool) {
	if s.open || s.confirm {
		return nil, false
	}
	idx, ok := s.clicks.at(localY)
	if !ok || idx < 0 || idx >= len(s.bullets) {
		return nil, false
	}
	if s.list.cursor == idx {
		s.open = true
		s.detailScroll = 0
	} else {
		s.list.cursor = idx
		s.status = ""
	}
	return nil, true
}

func (s *memoryState) detailView(m *Model, w, h int) string {
	n := s.list.cursor + 1
	if n < 1 || n > len(s.bullets) {
		n = 0
	}
	sub := fmt.Sprintf("note %d/%d", n, len(s.bullets))
	if m.data.GlobalMem != nil {
		sub += " · " + m.data.GlobalMem.Path()
	}
	out := pageTitle("memory", sub, w)
	lines := memoryDetailLines(s.selectedNote(), w)
	visible := h - 5
	if visible < 1 {
		visible = 1
	}
	s.clampDetailScroll(len(lines), visible)
	from := s.detailScroll
	to := from + visible
	if to > len(lines) {
		to = len(lines)
	}
	if from > 0 {
		out += sFaint.Render(fmt.Sprintf("  ⋯ %d earlier lines", from)) + "\n"
	}
	for _, l := range lines[from:to] {
		out += "  " + sText.Render(truncate(l, w-4)) + "\n"
	}
	if to < len(lines) {
		out += sFaint.Render(fmt.Sprintf("  ⋯ %d more lines", len(lines)-to)) + "\n"
	}
	out += "\n" + sFaint.Render("  j/k or wheel scroll · pgup/pgdn page · esc/q back")
	return out
}

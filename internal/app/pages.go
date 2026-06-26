package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/dream"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/skill"
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

	keyW := 18
	for i, f := range fields {
		v := config.Get(m.data.Config, f.Key)
		val := sText.Render(v)
		if v == "" {
			val = sFaint.Render("(unset)")
		}
		if c.editing && i == c.list.cursor {
			val = sAccent.Render(c.input + "▏")
		}
		line := sDim.Render(pad(truncate(f.Key, keyW-1), keyW)) + val
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
	clicks  clickMap
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

// clickAt selects a skill row; a second click on the selected row previews it
// (same as enter). Clicks are ignored while previewing or installing — those
// modes own the keyboard.
func (s *skillsState) clickAt(m *Model, localY int) (tea.Cmd, bool) {
	if s.preview != "" || s.prompt.active {
		return nil, false
	}
	idx, ok := s.clicks.at(localY)
	if !ok || idx < 0 || idx >= m.data.Skills.Len() {
		return nil, false
	}
	if s.list.cursor == idx {
		_, cmd := s.update(m, tea.KeyMsg{Type: tea.KeyEnter}) // preview the selected skill
		return cmd, true
	}
	s.list.cursor = idx
	return nil, true
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
	out += skillsSummaryLine(m.data, skills, w) + "\n\n"
	if len(skills) == 0 {
		out += sFaint.Render("  none — add SKILL.md under ~/.eigen/skills/<name>/ or press i to install") + "\n"
		out += s.prompt.render()
		out += "\n" + sFaint.Render("  i install a skill")
		return out
	}
	visible := h - 8
	from, to := s.list.window(visible)
	nameW := 30
	s.clicks.reset()
	for i := from; i < to; i++ {
		sk := skills[i]
		usage := ""
		if st, ok := m.data.Observe.Skills[sk.Name]; ok && st.Calls > 0 {
			usage = sViolet.Render(fmt.Sprintf(" %dx", st.Calls))
		}
		line := pad(truncate(sk.Name, nameW-1), nameW) + sDim.Render(truncate(sk.Description, max(8, w-nameW-8))) + usage
		s.clicks.mark(lineCount(out), i)
		out += row(i == s.list.cursor, line) + "\n"
	}
	if s.list.cursor >= 0 && s.list.cursor < len(skills) {
		out += "\n" + skillSelectedDetail(m.data, skills[s.list.cursor], w) + "\n"
	}
	out += s.prompt.render()
	out += "\n" + sFaint.Render("  enter preview · i install (path or owner/repo[@ref], optional --force/--no-scan)")
	return out
}

func skillsSummaryLine(d *Data, skills []skill.Skill, w int) string {
	var invoked, errors int
	if d != nil {
		for _, st := range d.Observe.Skills {
			if st.Calls > 0 {
				invoked++
				errors += st.Errors
			}
		}
	}
	parts := []string{fmt.Sprintf("%d installed", len(skills))}
	if invoked > 0 {
		parts = append(parts, fmt.Sprintf("%d invoked", invoked))
	}
	if errors > 0 {
		parts = append(parts, fmt.Sprintf("%d errors", errors))
	}
	return sFaint.Render("  " + truncate(strings.Join(parts, "  ·  "), max(20, w-2)))
}

func skillSelectedDetail(d *Data, sk skill.Skill, w int) string {
	line := fmt.Sprintf("selected: %s", sk.Name)
	if d != nil {
		if st, ok := d.Observe.Skills[sk.Name]; ok && st.Calls > 0 {
			line += fmt.Sprintf(" · used %d time(s)", st.Calls)
			if st.Errors > 0 {
				line += fmt.Sprintf(" · %d error(s)", st.Errors)
			}
		}
	}
	if sk.Path != "" {
		line += " · " + sk.Path
	}
	return sFaint.Render("  " + truncate(line, max(20, w-2)))
}

// modelsState lists the catalog with availability + capability tags.
type modelsState struct {
	list   list
	rows   []ModelRow
	clicks clickMap
}

func (s *modelsState) init(*Data) {
	s.rows = Models()
	s.list.count = len(s.rows)
}

func (s *modelsState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s.list.key(msg.String(), m.height-6)
	return m, nil
}

// clickAt selects a model row (the catalog is read-only, so a click moves the
// selection and refreshes the detail line — there is no separate open action).
func (s *modelsState) clickAt(_ *Model, localY int) (tea.Cmd, bool) {
	idx, ok := s.clicks.at(localY)
	if !ok || idx < 0 || idx >= len(s.rows) {
		return nil, false
	}
	s.list.cursor = idx
	return nil, true
}

func (s *modelsState) view(m *Model, w, h int) string {
	out := pageTitle("models", fmt.Sprintf("%d in catalog", len(s.rows)), w)
	out += modelsSummaryLine(s.rows, w) + "\n\n"
	visible := h - 8
	from, to := s.list.window(visible)
	s.clicks.reset()
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
		s.clicks.mark(lineCount(out), i)
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

	adding    bool
	addCursor int
	editing   bool
	input     string
	draft     providerDraft
	err       string
	saved     string
	clicks    clickMap
}

type providerDraft struct {
	Name          string
	Type          string
	API           string
	BaseURL       string
	APIKeyEnv     string
	Version       string
	ModelName     string
	ModelID       string
	ContextWindow string
}

type providerAddField struct {
	Key     string
	Label   string
	Help    string
	Options []string
}

var providerAddFields = []providerAddField{
	{Key: "type", Label: "type", Help: "wire protocol: openai-compatible or anthropic", Options: []string{"openai", "anthropic"}},
	{Key: "api", Label: "openai api", Help: "OpenAI-compatible endpoint kind", Options: []string{"chat", "responses"}},
	{Key: "name", Label: "provider name", Help: "short unique provider tag, e.g. openrouter or lab"},
	{Key: "base_url", Label: "base url", Help: "OpenAI root like https://host/v1 or Anthropic root like https://host/v1"},
	{Key: "api_key_env", Label: "api key env", Help: "env var containing the key; blank allows no-auth local endpoints"},
	{Key: "version", Label: "anthropic version", Help: "optional; default 2023-06-01"},
	{Key: "model_name", Label: "model name", Help: "Eigen alias shown in model picker, e.g. local-qwen"},
	{Key: "model_id", Label: "wire model id", Help: "id sent to the endpoint; blank uses model name"},
	{Key: "context_window", Label: "context window", Help: "tokens; blank uses a safe default"},
}

func (s *providersState) init(*Data) {
	s.rows = Providers()
	s.list.count = len(s.rows)
}

func (s *providersState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if s.adding {
		return s.updateAdd(m, msg)
	}
	s.err, s.saved = "", ""
	if s.list.key(key, m.height-6) {
		return m, nil
	}
	if key == "a" {
		s.startAdd()
		return m, nil
	}
	return m, nil
}

// clickAt selects a provider row (credentials are managed externally, so a
// click moves the selection and refreshes the detail line). Clicks are ignored
// while the add-provider form owns the keyboard.
func (s *providersState) clickAt(_ *Model, localY int) (tea.Cmd, bool) {
	if s.adding {
		return nil, false
	}
	idx, ok := s.clicks.at(localY)
	if !ok || idx < 0 || idx >= len(s.rows) {
		return nil, false
	}
	s.list.cursor = idx
	s.err, s.saved = "", ""
	return nil, true
}

func (s *providersState) startAdd() {
	s.adding = true
	s.addCursor = 0
	s.editing = false
	s.input = ""
	s.err = ""
	s.saved = ""
	s.draft = providerDraft{Type: "openai", API: "chat", APIKeyEnv: "OPENAI_API_KEY", ContextWindow: "128000"}
}

func (s *providersState) updateAdd(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	fields := s.visibleAddFields()
	if len(fields) == 0 {
		return m, nil
	}
	if s.addCursor >= len(fields) {
		s.addCursor = len(fields) - 1
	}
	if s.editing {
		s.err, s.saved = "", ""
		switch key {
		case "esc":
			s.editing, s.input = false, ""
		case "enter":
			s.setDraft(fields[s.addCursor].Key, strings.TrimSpace(s.input))
			s.editing, s.input = false, ""
		case "backspace":
			if len(s.input) > 0 {
				s.input = s.input[:len(s.input)-1]
			}
		default:
			if len(msg.Runes) > 0 {
				s.input += string(msg.Runes)
			} else if key == "space" || key == " " {
				s.input += " "
			}
		}
		return m, nil
	}
	if key == "esc" || key == "q" {
		s.adding, s.err = false, ""
		return m, nil
	}
	if key == "s" {
		s.saveDraft(m)
		return m, nil
	}
	if key == "up" || key == "k" || key == "ctrl+p" {
		if s.addCursor > 0 {
			s.addCursor--
		}
		return m, nil
	}
	if key == "down" || key == "j" || key == "ctrl+n" {
		if s.addCursor < len(fields)-1 {
			s.addCursor++
		}
		return m, nil
	}
	field := fields[s.addCursor]
	if (key == " " || key == "space") && len(field.Options) > 0 {
		cur := s.draftValue(field.Key)
		idx := 0
		for i, opt := range field.Options {
			if opt == cur {
				idx = i
				break
			}
		}
		s.setDraft(field.Key, field.Options[(idx+1)%len(field.Options)])
		return m, nil
	}
	if key == "enter" {
		if len(field.Options) > 0 {
			// Closed fields cycle on enter too: less modal state for this small form.
			s.updateAdd(m, tea.KeyMsg{Type: tea.KeySpace})
			return m, nil
		}
		s.editing = true
		s.input = s.draftValue(field.Key)
		return m, nil
	}
	return m, nil
}

func (s *providersState) visibleAddFields() []providerAddField {
	out := make([]providerAddField, 0, len(providerAddFields))
	for _, f := range providerAddFields {
		if s.draft.Type != "anthropic" && f.Key == "version" {
			continue
		}
		if s.draft.Type == "anthropic" && f.Key == "api" {
			continue
		}
		out = append(out, f)
	}
	return out
}

func (s *providersState) draftValue(key string) string {
	switch key {
	case "type":
		return s.draft.Type
	case "api":
		return s.draft.API
	case "name":
		return s.draft.Name
	case "base_url":
		return s.draft.BaseURL
	case "api_key_env":
		return s.draft.APIKeyEnv
	case "version":
		return s.draft.Version
	case "model_name":
		return s.draft.ModelName
	case "model_id":
		return s.draft.ModelID
	case "context_window":
		return s.draft.ContextWindow
	}
	return ""
}

func (s *providersState) setDraft(key, value string) {
	s.err, s.saved = "", ""
	switch key {
	case "type":
		s.draft.Type = value
		if value == "anthropic" && s.draft.APIKeyEnv == "OPENAI_API_KEY" {
			s.draft.APIKeyEnv = "ANTHROPIC_API_KEY"
		}
		if value == "openai" && s.draft.APIKeyEnv == "ANTHROPIC_API_KEY" {
			s.draft.APIKeyEnv = "OPENAI_API_KEY"
		}
	case "api":
		s.draft.API = value
	case "name":
		s.draft.Name = value
	case "base_url":
		s.draft.BaseURL = value
	case "api_key_env":
		s.draft.APIKeyEnv = value
	case "version":
		s.draft.Version = value
	case "model_name":
		s.draft.ModelName = value
	case "model_id":
		s.draft.ModelID = value
	case "context_window":
		s.draft.ContextWindow = value
	}
}

func (s *providersState) saveDraft(m *Model) {
	win := 0
	if strings.TrimSpace(s.draft.ContextWindow) != "" {
		n, err := strconv.Atoi(strings.TrimSpace(s.draft.ContextWindow))
		if err != nil || n < 0 {
			s.err = "context_window must be a non-negative integer"
			return
		}
		win = n
	}
	apiKeyEnv := strings.TrimSpace(s.draft.APIKeyEnv)
	p := llm.CustomProvider{
		Name:      strings.TrimSpace(s.draft.Name),
		Type:      strings.TrimSpace(s.draft.Type),
		API:       strings.TrimSpace(s.draft.API),
		BaseURL:   strings.TrimSpace(s.draft.BaseURL),
		APIKeyEnv: apiKeyEnv,
		NoAuth:    apiKeyEnv == "",
		Version:   strings.TrimSpace(s.draft.Version),
		Models: []llm.CustomModel{{
			Name:          strings.TrimSpace(s.draft.ModelName),
			ID:            strings.TrimSpace(s.draft.ModelID),
			ContextWindow: win,
		}},
	}
	if err := llm.UpsertCustomProvider(p); err != nil {
		s.err = err.Error()
		return
	}
	if cps, err := llm.LoadCustomProviders(); err == nil {
		m.data.CustomProviders = cps
	}
	s.rows = Providers()
	s.list.count = len(s.rows)
	m.models.rows = Models()
	m.models.list.count = len(m.models.rows)
	s.adding = false
	s.saved = "provider " + p.Name + " added"
	s.err = ""
}

func (s *providersState) view(m *Model, w, h int) string {
	if s.adding {
		return s.viewAdd(w, h)
	}
	out := pageTitle("providers", providerSubtitle(m.data), w)
	out += providersSummaryLine(s.rows, w) + "\n\n"
	from, to := s.list.window(h - 8)
	s.clicks.reset()
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
		s.clicks.mark(lineCount(out), i)
		out += row(i == s.list.cursor, line) + "\n"
	}
	if s.list.cursor >= 0 && s.list.cursor < len(s.rows) {
		out += "\n" + providerSelectedDetail(s.rows[s.list.cursor], w) + "\n"
	}
	if s.saved != "" {
		out += "\n" + sOk.Render("  "+s.saved)
	}
	if s.err != "" {
		out += "\n" + sErr.Render("  "+truncate(s.err, max(20, w-2)))
	}
	out += "\n" + sFaint.Render("  a add custom provider · credentials external · edit route_providers in config")
	return out
}

func (s *providersState) viewAdd(w, h int) string {
	out := pageTitle("add provider", "custom OpenAI-compatible or Anthropic endpoint", w)
	fields := s.visibleAddFields()
	if len(fields) == 0 {
		return out
	}
	out += sFaint.Render("  enter edit/cycle · space cycle choices · s save · esc cancel") + "\n\n"
	visible := h - 8
	if visible < 1 {
		visible = len(fields)
	}
	from := 0
	if s.addCursor >= visible {
		from = s.addCursor - visible + 1
	}
	to := min(len(fields), from+visible)
	for i := from; i < to; i++ {
		f := fields[i]
		val := s.draftValue(f.Key)
		if s.editing && i == s.addCursor {
			val = s.input + "▌"
		}
		if val == "" {
			val = "(blank)"
		}
		choiceHint := ""
		if len(f.Options) > 0 {
			choiceHint = sDim.Render("  [" + strings.Join(f.Options, "/") + "]")
		}
		line := fmt.Sprintf("%s = %s%s", pad(f.Label, 18), truncate(val, max(8, w-28)), choiceHint)
		out += row(i == s.addCursor, line) + "\n"
	}
	if s.addCursor >= 0 && s.addCursor < len(fields) {
		out += "\n" + sFaint.Render("  "+wrapText(fields[s.addCursor].Help, max(20, w-4))) + "\n"
	}
	if s.err != "" {
		out += sErr.Render("  "+truncate(s.err, max(20, w-2))) + "\n"
	}
	return out
}

func providerSubtitle(d *Data) string {
	if d != nil && d.CustomErr != "" {
		return "custom provider catalog error: " + d.CustomErr
	}
	if d == nil || !d.Config.Route {
		return "subtask route off"
	}
	if len(d.Config.RouteProviders) == 0 {
		return "subtask route on · all credentialed providers"
	}
	return "subtask route on · " + strings.Join(d.Config.RouteProviders, " ")
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
	if r.Custom {
		line += " · custom " + r.Type
		if r.BaseURL != "" {
			line += " · " + r.BaseURL
		}
	}
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
	if m.data != nil && m.data.GlobalMem != nil {
		visible-- // path row in detailView
	}
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
	out += memorySummaryLine(s.bullets, w) + "\n\n"
	visible := h - 9
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

func memorySummaryLine(bullets []string, w int) string {
	var sections, dated, bans int
	for _, b := range bullets {
		first := strings.TrimSpace(strings.TrimPrefix(strings.SplitN(b, "\n", 2)[0], "- "))
		if strings.HasPrefix(first, "## ") {
			sections++
		}
		if strings.HasPrefix(first, "**20") {
			dated++
		}
		if strings.Contains(strings.ToLower(first), "ban") || strings.Contains(strings.ToLower(first), "never") {
			bans++
		}
	}
	parts := []string{fmt.Sprintf("%d notes", len(bullets))}
	if sections > 0 {
		parts = append(parts, fmt.Sprintf("%d sections", sections))
	}
	if dated > 0 {
		parts = append(parts, fmt.Sprintf("%d dated", dated))
	}
	if bans > 0 {
		parts = append(parts, fmt.Sprintf("%d hard rules", bans))
	}
	return sFaint.Render("  " + truncate(strings.Join(parts, "  ·  "), max(20, w-2)))
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
	path := ""
	if m.data.GlobalMem != nil {
		path = m.data.GlobalMem.Path()
	}
	out := pageTitle("memory", sub, w)
	if path != "" {
		out += sFaint.Render("  path: "+truncate(path, max(12, w-8))) + "\n"
	}
	lines := memoryDetailLines(s.selectedNote(), w)
	visible := h - 5
	if path != "" {
		visible--
	}
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

package tui

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// compKind is which autocomplete source is currently driving the popup.
type compKind int

const (
	compNone    compKind = iota
	compSlash            // "/command" at the start of the input
	compMention          // "@path" file reference anywhere in the input
)

// compItem is one row in the autocomplete popup.
type compItem struct {
	insert string // text that replaces the active token when applied
	label  string // primary display (command name / file path)
	desc   string // secondary display (command description; "" for files)
}

// completion is the autocomplete popup state for the input box.
type completion struct {
	kind  compKind
	items []compItem
	idx   int
	start int // byte offset in the input where the active token ("/" or "@") begins
}

func (c completion) active() bool { return c.kind != compNone && len(c.items) > 0 }

// maxCompRows caps how many popup rows are shown at once.
const maxCompRows = 8

func (c completion) rows() int {
	n := len(c.items)
	if n > maxCompRows {
		n = maxCompRows
	}
	return n
}

// slashCmd is one entry in the slash-command set.
type slashCmd struct {
	name string
	desc string
}

// slashCommands is the full, ordered set of slash commands offered by the menu.
var slashCommands = []slashCmd{
	{"/help", "show commands and keybindings"},
	{"/home", "return to the app shell (home page)"},
	{"/sessions", "switch this window to another session (alt+s)"},
	{"/rail", "toggle the left session rail (running sessions)"},
	{"/changes", "toggle the right panel (changes/git/term)"},
	{"/term", "open a real shell terminal in the right panel"},
	{"/resume", "resume a saved session (picker, or path/id)"},
	{"/save", "save this conversation to a file"},
	{"/clear", "start a fresh conversation"},
	{"/rename", "rename this session (/rename <name>; empty reverts to derived)"},
	{"/compact", "summarize older context to shrink the token count"},
	{"/model", "show or switch the model/provider"},
	{"/effort", "show or set reasoning effort (levels are per-model)"},
	{"/search", "show or set grok live search (off|auto|on)"},
	{"/perm", "show or set the permission posture"},
	{"/goal", "show, set, or clear a persistent goal (north star)"},
	{"/loop", "re-submit a prompt every interval while idle (/loop 10m <prompt>)"},
	{"/config", "open the live settings panel (or /config <key> <value> to set)"},
	{"/route", "toggle the auto-router (per-task model selection)"},
	{"/review", "cross-vendor review of recent work (GPT⇄Claude)"},
	{"/voice", "toggle conversation mode (speak input ctrl+t, hear replies)"},
	{"/skills", "list skills, or /skills <name> to preview one"},
	{"/tools", "list available tools"},
	{"/find", "search the transcript"},
	{"/copy", "copy the selected block (or last answer) to clipboard"},
	{"/export", "export this conversation to a markdown file"},
	{"/read", "toggle reading answers aloud (TTS)"},
	{"/rebuild", "rebuild eigen and live-reload"},
	{"/quit", "exit eigen"},
}

// refreshCompletion recomputes the popup from the current input. Slash commands
// trigger on a leading "/" (no space yet); @file mentions trigger on a trailing
// "@token" at a word boundary.
func (m *model) refreshCompletion() {
	v := m.ti.Value()

	// Slash command: the input is a single "/word" token.
	if strings.HasPrefix(v, "/") && !strings.ContainsAny(v, " \t") {
		var items []compItem
		for _, c := range slashCommands {
			if strings.HasPrefix(c.name, v) {
				items = append(items, compItem{insert: c.name + " ", label: c.name, desc: c.desc})
			}
		}
		m.comp = completion{kind: compSlash, items: items, start: 0}
		m.relayout()
		return
	}

	// @file mention: the last "@" that sits at a word boundary, with no
	// whitespace between it and the cursor (end of input).
	if at := strings.LastIndexByte(v, '@'); at >= 0 {
		token := v[at+1:]
		boundary := at == 0 || v[at-1] == ' ' || v[at-1] == '\t'
		if boundary && !strings.ContainsAny(token, " \t") {
			matches := m.fileMatches(token)
			items := make([]compItem, 0, len(matches))
			for _, p := range matches {
				items = append(items, compItem{insert: p + " ", label: p})
			}
			if len(items) > 0 {
				m.comp = completion{kind: compMention, items: items, start: at}
				m.relayout()
				return
			}
		}
	}

	m.comp = completion{kind: compNone}
	m.relayout()
}

// applyCompletion replaces the active token with the highlighted item and
// closes the popup. It does not submit.
func (m *model) applyCompletion() {
	if !m.comp.active() || m.comp.idx >= len(m.comp.items) {
		return
	}
	v := m.ti.Value()
	if m.comp.start > len(v) {
		m.comp.start = len(v)
	}
	m.ti.SetValue(v[:m.comp.start] + m.comp.items[m.comp.idx].insert)
	m.ti.CursorEnd()
	m.comp = completion{kind: compNone}
	m.relayout()
}

// compMenuView renders the popup (one line per item) shown above the input.
func (m *model) compMenuView() string {
	if !m.comp.active() {
		return ""
	}
	rows := m.comp.rows()
	start := 0
	if m.comp.idx >= rows {
		start = m.comp.idx - rows + 1
	}
	var b strings.Builder
	for i := start; i < start+rows && i < len(m.comp.items); i++ {
		it := m.comp.items[i]
		line := it.label
		if it.desc != "" {
			line = fmt.Sprintf("%-9s %s", it.label, it.desc)
		}
		if i == m.comp.idx {
			b.WriteString(styleAsk.Render("› "+line) + "\n")
		} else {
			b.WriteString("  " + dim(line) + "\n")
		}
	}
	return b.String()
}

// --- @file index ------------------------------------------------------------

const (
	maxIndexedFiles = 20000
	fileIdxTTL      = 10 * time.Second
)

// skipDir names directories never worth walking for @file completion.
var skipDir = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".eigen": true,
	"dist": true, "build": true, "target": true, ".next": true,
	".venv": true, "__pycache__": true,
}

// fileMatches returns project-relative file paths matching token (case
// insensitive), ranked by how directly the basename matches. The project file
// list is cached briefly so typing doesn't re-walk the tree on every keystroke.
func (m *model) fileMatches(token string) []string {
	if time.Since(m.fileIdxAt) > fileIdxTTL || m.fileIdx == nil {
		m.fileIdx = indexFiles(".")
		m.fileIdxAt = time.Now()
	}
	tok := strings.ToLower(token)
	type scored struct {
		path  string
		score int
	}
	var hits []scored
	for _, p := range m.fileIdx {
		base := strings.ToLower(filepath.Base(p))
		lp := strings.ToLower(p)
		switch {
		case tok == "":
			hits = append(hits, scored{p, 2})
		case strings.HasPrefix(base, tok):
			hits = append(hits, scored{p, 0})
		case strings.Contains(base, tok):
			hits = append(hits, scored{p, 1})
		case strings.Contains(lp, tok):
			hits = append(hits, scored{p, 2})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score < hits[j].score
		}
		return len(hits[i].path) < len(hits[j].path)
	})
	out := make([]string, 0, maxCompRows)
	for _, h := range hits {
		out = append(out, h.path)
		if len(out) >= maxCompRows {
			break
		}
	}
	return out
}

// indexFiles walks root, returning project-relative file paths (skipping VCS and
// build directories), bounded by maxIndexedFiles.
func indexFiles(root string) []string {
	var files []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (skipDir[name] || strings.HasPrefix(name, ".") && name != ".") {
				return fs.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		files = append(files, rel)
		if len(files) >= maxIndexedFiles {
			return fs.SkipAll
		}
		return nil
	})
	return files
}

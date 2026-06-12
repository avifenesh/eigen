package tui

// The right "changes" panel (Tier 9 Wave 4, v1 = a change INDEX): the files
// touched by the last edit-producing run, each with +adds/−dels and a click/key
// jump to the tool block that changed it. It reads the transcript's edit-family
// tool blocks (edit/multiedit/write/apply_patch) — the same source the inline
// diffs render from — so it never needs a separate data feed. Keyed off the
// last RUN that produced edits (turns split at user messages), not raw block
// order, so it survives streaming, resumes, and retries.

import (
	"encoding/json"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// rightPanelWidthCols is the changes panel's total width (content + gutter).
const rightPanelWidthCols = 34

// minTranscriptCols is the transcript's minimum width; chrome panels degrade
// (right panel first, then the rail) to preserve it.
const minTranscriptCols = 40

// fileChange is one file touched in a run: its path, +adds/−dels, and the
// transcript block index that changed it (for jump-to).
type fileChange struct {
	path     string
	adds     int
	dels     int
	blockIdx int
}

// changesVisible reports whether the right panel should render: enabled, real
// content for the active tab (changes needs edits; git can show no-repo/status),
// and there is room (degrade right-first — the panel hides before the rail when
// the terminal is too narrow to keep the transcript usable).
func (m *model) changesVisible() bool {
	if !m.changesOn {
		return false
	}
	if m.rightTab == rightTabChanges && len(m.lastRunChanges()) == 0 {
		return false
	}
	// Room check: width minus the rail minus this panel must leave the
	// transcript its minimum. The rail (already shown) keeps priority.
	if m.width-m.railWidth()-rightPanelWidthCols < minTranscriptCols {
		return false
	}
	return true
}

// rightPanelWidth is the changes panel's column width (0 when hidden).
func (m *model) rightPanelWidth() int {
	if !m.changesVisible() {
		return 0
	}
	return rightPanelWidthCols
}

// lastRunChanges returns the files touched by the most recent run that produced
// edits — turns split at user messages; the last segment with ≥1 edit wins.
// Aggregated by path (summed stats), in first-touched order. Cached by a cheap
// transcript signature so it isn't recomputed on every View() frame (only when
// the transcript actually changed).
func (m *model) lastRunChanges() []fileChange {
	sig := m.changesSig()
	if sig == m.changesCacheSig && m.changesCache != nil {
		return m.changesCache
	}
	out := m.computeLastRunChanges()
	m.changesCacheSig = sig
	if out == nil {
		out = []fileChange{} // cache the empty result too (non-nil sentinel)
	}
	m.changesCache = out
	return out
}

// changesSig is a cheap signature of the transcript state that affects the
// change index: block count + the last block's tool args/result lengths (which
// grow as a tool completes).
func (m *model) changesSig() string {
	n := len(m.blocks)
	if n == 0 {
		return "0"
	}
	last := m.blocks[n-1]
	return itoa(n) + ":" + itoa(len(last.toolArgs)) + ":" + itoa(len(last.result)) + ":" + itoa(int(last.state))
}

// computeLastRunChanges does the actual scan (see lastRunChanges).
func (m *model) computeLastRunChanges() []fileChange {
	// Find run segments: [start,end) ranges between user text blocks.
	type seg struct{ start, end int }
	var segs []seg
	start := 0
	for i, b := range m.blocks {
		if b.kind == blockText && b.role == "user" {
			if i > start {
				segs = append(segs, seg{start, i})
			}
			start = i
		}
	}
	segs = append(segs, seg{start, len(m.blocks)})

	// Walk segments from the last; take the first (latest) with edits.
	for s := len(segs) - 1; s >= 0; s-- {
		changes := m.collectChanges(segs[s].start, segs[s].end)
		if len(changes) > 0 {
			return changes
		}
	}
	return nil
}

// collectChanges aggregates edit-family tool blocks in [start,end) by file path.
func (m *model) collectChanges(start, end int) []fileChange {
	order := []string{}
	byPath := map[string]*fileChange{}
	for i := start; i < end && i < len(m.blocks); i++ {
		b := m.blocks[i]
		if b.kind != blockTool {
			continue
		}
		for _, fc := range editsInBlock(b, i) {
			if fc.path == "" {
				continue
			}
			cur, ok := byPath[fc.path]
			if !ok {
				cp := fc
				byPath[fc.path] = &cp
				order = append(order, fc.path)
			} else {
				cur.adds += fc.adds
				cur.dels += fc.dels
			}
		}
	}
	out := make([]fileChange, 0, len(order))
	for _, p := range order {
		out = append(out, *byPath[p])
	}
	return out
}

// editsInBlock extracts the file change(s) a tool block represents (empty for
// non-edit tools). blockIdx is recorded for jump-to.
func editsInBlock(b *block, blockIdx int) []fileChange {
	if b.kind != blockTool {
		return nil
	}
	switch b.toolName {
	case "edit", "multiedit", "write":
		var a struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(b.toolArgs, &a)
		if a.Path == "" {
			return nil
		}
		add, del := diffStats(b.toolDetail())
		return []fileChange{{path: a.Path, adds: add, dels: del, blockIdx: blockIdx}}
	case "apply_patch":
		var a struct {
			Patch string `json:"patch"`
		}
		if json.Unmarshal(b.toolArgs, &a) != nil || a.Patch == "" {
			return nil
		}
		return filesInPatch(a.Patch, blockIdx)
	}
	return nil
}

// filesInPatch splits a unified-diff patch into per-file changes with +/− stats.
func filesInPatch(patch string, blockIdx int) []fileChange {
	var out []fileChange
	var cur *fileChange
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, ln := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(ln, "+++ "):
			flush()
			p := strings.TrimSpace(strings.TrimPrefix(ln, "+++ "))
			p = strings.TrimPrefix(p, "b/")
			if p == "/dev/null" {
				p = ""
			}
			cur = &fileChange{path: p, blockIdx: blockIdx}
		case strings.HasPrefix(ln, "diff --git"):
			// New file section; the +++ header that follows sets the path.
			flush()
		case cur != nil && strings.HasPrefix(ln, "+") && !strings.HasPrefix(ln, "+++"):
			cur.adds++
		case cur != nil && strings.HasPrefix(ln, "-") && !strings.HasPrefix(ln, "---"):
			cur.dels++
		}
	}
	flush()
	return out
}

// changesLines renders the changes panel as exactly h lines, each padded to the
// panel width with a left gutter separator.
func (m *model) changesLines(h int) []string {
	if m.rightTab == rightTabGit {
		return m.gitLines(h)
	}
	changes := m.lastRunChanges()
	contentW := rightPanelWidthCols - 2 // leading "│ " gutter
	lines := make([]string, 0, h)
	lines = append(lines, changesPad(m.rightPanelTitleLine(rightPanelWidthCols-2), rightPanelWidthCols))
	for _, fc := range changes {
		if len(lines) >= h {
			break
		}
		name := filepath.Base(fc.path)
		stats := ""
		if fc.adds > 0 {
			stats += styleStatus.Render("+" + itoa(fc.adds))
		}
		if fc.dels > 0 {
			if stats != "" {
				stats += " "
			}
			stats += styleErr.Render("−" + itoa(fc.dels))
		}
		statsW := ansi.StringWidth(ansi.Strip(stats))
		nameW := contentW - statsW - 1
		label := ansiTrunc(name, nameW)
		gap := contentW - ansi.StringWidth(label) - statsW
		if gap < 1 {
			gap = 1
		}
		lines = append(lines, changesPad(label+strings.Repeat(" ", gap)+stats, rightPanelWidthCols))
	}
	for len(lines) < h {
		lines = append(lines, changesPad("", rightPanelWidthCols))
	}
	return lines
}

// changesPad renders a dim separator gutter then pads the (styled) label to
// the panel width in display columns.
func changesPad(label string, w int) string {
	plainW := ansi.StringWidth(ansi.Strip(label))
	inner := w - 2 // leading "│ "
	pad := inner - plainW
	if pad < 0 {
		pad = 0
	}
	return dim("│ ") + label + strings.Repeat(" ", pad)
}

// changesRowAt maps a changes-panel-local y to a file-change index, or -1.
// Row 0 is the panel header; files start at row 1.
func (m *model) changesRowAt(localY int) int {
	idx := localY - 1
	changes := m.lastRunChanges()
	if idx < 0 || idx >= len(changes) {
		return -1
	}
	return idx
}

// jumpToChange selects + scrolls to the tool block that changed file index i.
func (m *model) jumpToChange(i int) tea.Cmd {
	changes := m.lastRunChanges()
	if i < 0 || i >= len(changes) {
		return nil
	}
	bi := changes[i].blockIdx
	if bi < 0 || bi >= len(m.blocks) {
		return nil
	}
	m.sel = bi
	if m.blocks[bi].collapsible() {
		m.blocks[bi].collapsed = false
	}
	m.sync()
	m.scrollToSelected()
	return nil
}

// itoa is a tiny int→string helper (avoids importing strconv just for this).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

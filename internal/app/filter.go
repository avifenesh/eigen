package app

// Session-list search + filters (Tier 13): type-to-search over title +
// project dir + id, a recency cutoff with a "show all" tail, and a source
// filter. ONE row model: the page's cursor walks the FILTERED index slice;
// every action (open/delete/export) resolves through it, so filtered views
// never operate on the wrong row.

import (
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/fuzzy"
)

// sessionFilter is the per-surface filter state. It resets when the page is
// re-entered (no sticky invisible filters).
type sessionFilter struct {
	searching bool   // typing into the search box
	query     string // incremental fuzzy query
	source    string // "" = all; cycles daemon/store/imported…
	showAll   bool   // recency cutoff disabled ("show all N")
}

// recencyCutoff hides sessions older than this by default (the list is
// endless; "last 7 days" is the working set). A tail row offers "show all".
const recencyCutoff = 7 * 24 * time.Hour

// filtered returns the indices into rows that pass the filter, rank-ordered:
// fuzzy rank when searching, original (recency) order otherwise. hidden is
// how many rows the recency cutoff removed (for the tail line).
func (f *sessionFilter) filtered(rows []SessionRow) (idx []int, hidden int) {
	type hit struct{ i, score int }
	var hits []hit
	cut := time.Now().Add(-recencyCutoff).UnixNano()
	for i, r := range rows {
		if f.source != "" && r.Source != f.source {
			continue
		}
		if f.query != "" {
			s := fuzzy.Score(r.Title+" "+r.Dir+" "+r.ID, f.query)
			if s < 0 {
				continue
			}
			hits = append(hits, hit{i, s})
			continue
		}
		if !f.showAll && r.Updated > 0 && r.Updated < cut {
			hidden++
			continue
		}
		hits = append(hits, hit{i, 0})
	}
	if f.query != "" {
		// Stable by score then original (recency) order.
		for a := 1; a < len(hits); a++ {
			for b := a; b > 0 && hits[b].score < hits[b-1].score; b-- {
				hits[b], hits[b-1] = hits[b-1], hits[b]
			}
		}
	}
	// A cutoff that hides EVERYTHING is useless — show all instead (e.g. a
	// machine that was away for two weeks still gets a session list).
	if len(hits) == 0 && hidden > 0 {
		for i, r := range rows {
			if f.source != "" && r.Source != f.source {
				continue
			}
			hits = append(hits, hit{i, 0})
		}
		hidden = 0
	}
	idx = make([]int, len(hits))
	for i, h := range hits {
		idx[i] = h.i
	}
	return idx, hidden
}

// active reports whether any filter narrows the list.
func (f *sessionFilter) active() bool {
	return f.query != "" || f.source != ""
}

// statusLine renders the filter state for the page footer.
func (f *sessionFilter) statusLine() string {
	var parts []string
	if f.searching || f.query != "" {
		parts = append(parts, "search: "+f.query+"▌")
	}
	if f.source != "" {
		parts = append(parts, "source="+f.source)
	}
	return strings.Join(parts, " · ")
}

// key handles filter keystrokes. Returns handled=true when consumed.
// Search capture: "/" starts, typing extends, backspace deletes, esc clears,
// enter keeps the query and returns focus to the list.
func (f *sessionFilter) key(key string) (handled bool) {
	if f.searching {
		switch key {
		case "esc":
			f.searching, f.query = false, ""
		case "enter":
			f.searching = false
		case "backspace":
			if f.query != "" {
				f.query = f.query[:len(f.query)-1]
			} else {
				f.searching = false
			}
		default:
			if len(key) == 1 || key == "space" {
				if key == "space" {
					key = " "
				}
				f.query += key
			}
		}
		return true
	}
	switch key {
	case "/":
		f.searching = true
		return true
	case "esc":
		if f.active() {
			f.query, f.source = "", ""
			return true
		}
	case "s":
		// Cycle source filter: all → daemon → eigen → imported sources → all.
		order := []string{"", "daemon", "eigen", "claude", "codex", "opencode"}
		for i, s := range order {
			if f.source == s {
				f.source = order[(i+1)%len(order)]
				return true
			}
		}
		f.source = ""
		return true
	case "a":
		f.showAll = !f.showAll
		return true
	}
	return false
}

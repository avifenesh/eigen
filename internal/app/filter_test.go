package app

import (
	"testing"
	"time"
)

func rowsFixture() []SessionRow {
	now := time.Now()
	return []SessionRow{
		{ID: "s1", Title: "refactor the daemon", Dir: "/p/eigen", Source: "daemon", Msgs: 10, Updated: now.UnixNano()},
		{ID: "s2", Title: "fix voice bug", Dir: "/p/eigen", Source: "daemon", Msgs: 4, Updated: now.Add(-2 * time.Hour).UnixNano()},
		{ID: "s3", Title: "old planning chat", Dir: "/p/revuto", Source: "eigen", Msgs: 20, Updated: now.Add(-30 * 24 * time.Hour).UnixNano()},
		{ID: "s4", Title: "imported claude thread", Dir: "/p/other", Source: "claude", Msgs: 8, Updated: now.Add(-1 * time.Hour).UnixNano()},
	}
}

func TestFilterRecencyCutoff(t *testing.T) {
	f := sessionFilter{}
	idx, hidden := f.filtered(rowsFixture())
	if hidden != 1 {
		t.Fatalf("the 30-day-old session should be hidden, hidden=%d", hidden)
	}
	if len(idx) != 3 {
		t.Fatalf("3 recent sessions expected, got %d", len(idx))
	}
	f.showAll = true
	idx, hidden = f.filtered(rowsFixture())
	if hidden != 0 || len(idx) != 4 {
		t.Fatalf("showAll should reveal everything: idx=%d hidden=%d", len(idx), hidden)
	}
}

func TestFilterSearch(t *testing.T) {
	f := sessionFilter{query: "voice"}
	idx, _ := f.filtered(rowsFixture())
	if len(idx) != 1 || idx[0] != 1 {
		t.Fatalf("search 'voice' should match only s2, got %v", idx)
	}
	// Search ignores the recency cutoff (find old sessions too).
	f = sessionFilter{query: "planning"}
	idx, _ = f.filtered(rowsFixture())
	if len(idx) != 1 || idx[0] != 2 {
		t.Fatalf("search should reach past the cutoff, got %v", idx)
	}
}

func TestFilterSource(t *testing.T) {
	f := sessionFilter{source: "claude", showAll: true}
	idx, _ := f.filtered(rowsFixture())
	if len(idx) != 1 || idx[0] != 3 {
		t.Fatalf("source=claude should match only s4, got %v", idx)
	}
}

func TestFilterNeverHidesEverything(t *testing.T) {
	// All rows older than the cutoff → fall back to showing them.
	old := time.Now().Add(-100 * 24 * time.Hour).UnixNano()
	rows := []SessionRow{{ID: "s1", Title: "ancient", Updated: old}}
	f := sessionFilter{}
	idx, hidden := f.filtered(rows)
	if len(idx) != 1 || hidden != 0 {
		t.Fatalf("a cutoff that hides all should show all: idx=%d hidden=%d", len(idx), hidden)
	}
}

func TestFilterKeyCapture(t *testing.T) {
	f := &sessionFilter{}
	if !f.key("/") || !f.searching {
		t.Fatal("/ should start searching")
	}
	f.key("v")
	f.key("o")
	if f.query != "vo" {
		t.Fatalf("typing should extend the query, got %q", f.query)
	}
	f.key("backspace")
	if f.query != "v" {
		t.Fatalf("backspace should trim, got %q", f.query)
	}
	f.key("enter")
	if f.searching {
		t.Fatal("enter should leave search mode but keep the query")
	}
	if f.query != "v" {
		t.Fatal("enter should keep the query")
	}
	f.key("esc")
	if f.query != "" {
		t.Fatal("esc should clear the active query")
	}
	// Source cycle.
	f.key("s")
	if f.source != "daemon" {
		t.Fatalf("s should cycle to daemon, got %q", f.source)
	}
}

func TestFilterHidesEmptySessions(t *testing.T) {
	now := time.Now().UnixNano()
	rows := []SessionRow{
		{ID: "s1", Title: "real", Dir: "/p", Source: "daemon", Msgs: 12, Updated: now},
		{ID: "s2", Title: "", Dir: "/p", Source: "daemon", Msgs: 0, Updated: now}, // empty: hide
	}
	f := sessionFilter{}
	idx, hidden := f.filtered(rows)
	if len(idx) != 1 || idx[0] != 0 {
		t.Fatalf("empty session should be hidden, got idx=%v", idx)
	}
	if hidden != 1 {
		t.Fatalf("hidden count should be 1, got %d", hidden)
	}
	// showAll reveals it.
	f.showAll = true
	idx, _ = f.filtered(rows)
	if len(idx) != 2 {
		t.Fatalf("showAll should reveal the empty session, got %d", len(idx))
	}
}

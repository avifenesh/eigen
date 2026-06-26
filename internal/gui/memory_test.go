package gui

import (
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/memory"
)

// isolateMemoryHome points the memory stores (which resolve ~/.eigen/memory via
// os.UserHomeDir) at a throwaway temp dir so these tests never touch the real
// user memory.
func isolateMemoryHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

// TestAppendMemoryConsolidates verifies AppendMemory routes through Store.Append
// (which also enqueues consolidation/summary) rather than only writing an ad-hoc
// note. We assert the note landed as an ad-hoc note — the path Append shares — so
// a regression back to a no-op or wrong store surfaces here.
func TestAppendMemoryConsolidates(t *testing.T) {
	isolateMemoryHome(t)
	b := &Bridge{}

	if err := b.AppendMemory("project", "remember the deploy step"); err != nil {
		t.Fatalf("AppendMemory: %v", err)
	}

	s, err := memory.Open("")
	if err != nil {
		t.Fatalf("memory.Open: %v", err)
	}
	notes := s.AdHocNotes(50)
	found := false
	for _, n := range notes {
		if strings.Contains(n, "remember the deploy step") {
			found = true
		}
	}
	if !found {
		t.Fatalf("appended note not stored; ad-hoc notes = %v", notes)
	}
}

// TestMoveMemoryNote_PromoteToGlobal verifies the bridge promote path: a project
// note moved to global is recorded in the global store (and the source gets a
// supersede tombstone). Exercises the same bridge method the GUI move button
// calls (the synthetic webview click is unreliable in the test sandbox).
func TestMoveMemoryNote_PromoteToGlobal(t *testing.T) {
	isolateMemoryHome(t)
	b := &Bridge{}

	if err := b.MoveMemoryNote("project", "global", "use ripgrep not grep everywhere"); err != nil {
		t.Fatalf("MoveMemoryNote: %v", err)
	}
	glob, err := memory.OpenGlobal()
	if err != nil {
		t.Fatalf("OpenGlobal: %v", err)
	}
	found := false
	for _, n := range glob.AdHocNotes(50) {
		if strings.Contains(n, "use ripgrep not grep everywhere") {
			found = true
		}
	}
	if !found {
		t.Fatal("promoted note not recorded in global memory")
	}
	// Same scope → error (guard).
	if err := b.MoveMemoryNote("global", "global", "x"); err == nil {
		t.Fatal("expected same-scope move to error")
	}
}

// TestMergeMemoryScope_FoldsByKey verifies the bridge merge path folds one
// project scope into another and reports a human summary.
func TestMergeMemoryScope_FoldsByKey(t *testing.T) {
	isolateMemoryHome(t)
	b := &Bridge{}

	src, err := memory.OpenByKey("orphan-11111111")
	if err != nil {
		t.Fatal(err)
	}
	if err := src.Rewrite("# Orphan\n- a fact only here\n"); err != nil {
		t.Fatal(err)
	}
	dst, err := memory.OpenByKey("live-22222222")
	if err != nil {
		t.Fatal(err)
	}
	if err := dst.Rewrite("# Live\n- existing\n"); err != nil {
		t.Fatal(err)
	}

	msg, err := b.MergeMemoryScope("orphan-11111111", "live-22222222")
	if err != nil {
		t.Fatalf("MergeMemoryScope: %v", err)
	}
	if !strings.Contains(msg, "merged") {
		t.Fatalf("unexpected summary: %q", msg)
	}
	merged, _ := memory.OpenByKey("live-22222222")
	if !strings.Contains(merged.Read(), "a fact only here") {
		t.Fatalf("destination missing folded content:\n%s", merged.Read())
	}
}

// TestSplitNotesSectionSplitting verifies splitNotes honors the curated-memory
// contract: section-structured MEMORY.md ("## Heading\n\n- bullets") splits on
// top-level "## " heading boundaries — keeping each heading WITH its body so a
// heading is never orphaned from its bullets (the APP-053 regression) — while
// un-sectioned (un-consolidated) content falls back to blank-line cards, and a
// leading "# " file title is dropped in both paths.
func TestSplitNotesSectionSplitting(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			// The APP-053 regression: post-consolidation markdown. Each heading must
			// stay attached to its bullets — NOT split into orphan "## Preferences"
			// + bullet cards.
			name: "consolidated sections keep heading with bullets",
			in:   "# Memory\n\n## Preferences\n\n- likes tabs\n- dislikes hedging\n\n## Commands\n\n- build: make\n- test: go test",
			want: []string{
				"## Preferences\n\n- likes tabs\n- dislikes hedging",
				"## Commands\n\n- build: make\n- test: go test",
			},
		},
		{
			name: "preamble before first section is dropped",
			in:   "# Memory\n\nintro text\n\n## Preferences\n\n- likes tabs",
			want: []string{"## Preferences\n\n- likes tabs"},
		},
		{
			name: "single section, no top-level title",
			in:   "## Preferences\n\n- likes tabs\n- dislikes hedging",
			want: []string{"## Preferences\n\n- likes tabs\n- dislikes hedging"},
		},
		{
			// Fallback: no "## " sections — un-consolidated append-only notes split
			// on blank lines, leading "# " title dropped.
			name: "no sections falls back to blank-line cards",
			in:   "# Memory\n\nfirst note\n\nsecond note",
			want: []string{"first note", "second note"},
		},
		{
			name: "no sections, no title keeps all chunks",
			in:   "first note\n\nsecond note",
			want: []string{"first note", "second note"},
		},
		{
			name: "only a top-level heading yields no notes",
			in:   "# Memory",
			want: nil,
		},
		{
			name: "empty content yields no notes",
			in:   "   \n\n  ",
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitNotes(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("splitNotes(%q) = %d notes %+v, want %d %v", tc.in, len(got), got, len(tc.want), tc.want)
			}
			for i, n := range got {
				if n.Text != tc.want[i] {
					t.Errorf("note[%d].Text = %q, want %q", i, n.Text, tc.want[i])
				}
				if n.Index != i {
					t.Errorf("note[%d].Index = %d, want %d (indices must be contiguous)", i, n.Index, i)
				}
			}
		})
	}
}

// TestSplitNotesNoOrphanHeadings is the focused APP-053 guard: no resulting note
// is solely a "## " heading line with no body. Before the fix, blank-line
// splitting produced exactly such orphan cards.
func TestSplitNotesNoOrphanHeadings(t *testing.T) {
	const consolidated = "# Memory\n\n## Preferences\n\n- a\n- b\n\n## Style\n\n- c"
	for _, n := range splitNotes(consolidated) {
		lines := strings.Split(strings.TrimSpace(n.Text), "\n")
		if len(lines) == 1 && strings.HasPrefix(lines[0], "## ") {
			t.Errorf("orphan heading card: %q (heading severed from its body)", n.Text)
		}
	}
}

// TestBanRoundTrip exercises the GUI ban bridge end to end: add (new + replace),
// surface via the DTO's BanList, then remove.
func TestBanRoundTrip(t *testing.T) {
	isolateMemoryHome(t)
	b := &Bridge{}

	replaced, err := b.AddBan("project", "No hedging", "don't start replies with 'I think'")
	if err != nil {
		t.Fatalf("AddBan: %v", err)
	}
	if replaced {
		t.Fatalf("first AddBan reported replaced=true, want false")
	}

	// Re-adding the same title updates in place.
	replaced, err = b.AddBan("project", "No hedging", "never hedge")
	if err != nil {
		t.Fatalf("AddBan (update): %v", err)
	}
	if !replaced {
		t.Fatalf("second AddBan reported replaced=false, want true")
	}

	// The DTO exposes the structured ban list.
	dto, err := b.Memory()
	if err != nil {
		t.Fatalf("Memory: %v", err)
	}
	if dto.Project == nil || len(dto.Project.BanList) != 1 {
		t.Fatalf("BanList = %+v, want exactly 1 ban", dto.Project)
	}
	if got := dto.Project.BanList[0].Rule; got != "never hedge" {
		t.Fatalf("ban rule = %q, want updated %q", got, "never hedge")
	}

	// Remove it.
	removed, err := b.RemoveBan("project", "No hedging")
	if err != nil {
		t.Fatalf("RemoveBan: %v", err)
	}
	if !removed {
		t.Fatalf("RemoveBan reported removed=false, want true")
	}

	// Removing a missing ban reports false without error.
	removed, err = b.RemoveBan("project", "No hedging")
	if err != nil {
		t.Fatalf("RemoveBan (missing): %v", err)
	}
	if removed {
		t.Fatalf("RemoveBan of absent title reported removed=true, want false")
	}
}

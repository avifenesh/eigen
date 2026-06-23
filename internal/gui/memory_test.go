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

// TestSplitNotesDropsLeadingHeading verifies splitNotes honors its documented
// contract: a leading chunk that is solely a top-level ATX "# " heading (the
// file title) is dropped, while a "## " subheading, an inline-bodied "# " block,
// and content without a title are all preserved.
func TestSplitNotesDropsLeadingHeading(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "leading top-level heading dropped",
			in:   "# Memory\n\nfirst note\n\nsecond note",
			want: []string{"first note", "second note"},
		},
		{
			name: "leading heading with surrounding blank lines dropped",
			in:   "\n\n# Memory\n\nonly note\n",
			want: []string{"only note"},
		},
		{
			name: "no heading keeps all chunks",
			in:   "first note\n\nsecond note",
			want: []string{"first note", "second note"},
		},
		{
			name: "subheading is not a droppable title",
			in:   "## Section\n\nbody",
			want: []string{"## Section", "body"},
		},
		{
			name: "heading with body in same chunk is kept whole",
			in:   "# Title\nbody on next line\n\nnext",
			want: []string{"# Title\nbody on next line", "next"},
		},
		{
			name: "only a heading yields no notes",
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
					t.Errorf("note[%d].Index = %d, want %d (indices must be contiguous after drop)", i, n.Index, i)
				}
			}
		})
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

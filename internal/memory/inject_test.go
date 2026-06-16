package memory

import (
	"strings"
	"testing"
)

func TestClampMemoryTailKeepsNewest(t *testing.T) {
	// Small content under the cap passes through unchanged.
	small := "line a\nline b\nline c"
	if got := clampMemoryTail(small, 1024); got != small {
		t.Fatalf("small content should be unchanged, got %q", got)
	}

	// Build oversized append-only content: oldest first, newest last.
	var b strings.Builder
	for i := 0; i < 5000; i++ {
		b.WriteString("- note number ")
		b.WriteByte(byte('0' + i%10))
		b.WriteString(" some text on this line\n")
	}
	b.WriteString("- THE NEWEST NOTE MARKER\n")
	full := b.String()

	got := clampMemoryTail(full, 8*1024)
	if len(got) > 8*1024+64 { // marker adds a little
		t.Fatalf("clamped length %d exceeds budget", len(got))
	}
	// Must keep the newest note (tail) and drop the oldest.
	if !strings.Contains(got, "THE NEWEST NOTE MARKER") {
		t.Fatal("clamp must keep the newest (tail) note")
	}
	if !strings.HasPrefix(got, "[…older notes trimmed") {
		t.Fatalf("expected truncation marker, got prefix %q", got[:40])
	}
	// No partial leading line: first real line after the marker is a clean note.
	lines := strings.SplitN(got, "\n", 3)
	if !strings.HasPrefix(lines[1], "- note number") {
		t.Fatalf("first kept line should be a clean note boundary, got %q", lines[1])
	}
}

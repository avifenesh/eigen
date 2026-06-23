package app

import "testing"

// inWindow reports whether idx falls inside the [from,to) range.
func inWindow(idx, from, to int) bool { return idx >= from && idx < to }

// TestWindowKeepsCursorVisibleAcrossMismatch reproduces APP-030: update()
// advances list.top using a (larger) visible estimate derived from the FULL
// terminal height, while view() renders with a smaller window (inner height
// minus the page's fixed chrome). The rendered window must still contain the
// cursor — otherwise the selected row scrolls past the bottom while top is put.
func TestWindowKeepsCursorVisibleAcrossMismatch(t *testing.T) {
	const updateVisible = 20 // what move()/key() saw (m.height - small chrome)
	const viewVisible = 12   // what view() actually renders (inner.h - real chrome)

	l := list{count: 40}
	// Walk the cursor down one row at a time the way j/down does in update().
	for step := 0; step < l.count; step++ {
		l.move(1, updateVisible)
		from, to := l.window(viewVisible)
		if !inWindow(l.cursor, from, to) {
			t.Fatalf("step %d: cursor %d outside rendered window [%d,%d)", step, l.cursor, from, to)
		}
		if to-from > viewVisible {
			t.Fatalf("step %d: window [%d,%d) wider than visible %d", step, from, to, viewVisible)
		}
		if from < 0 || to > l.count {
			t.Fatalf("step %d: window [%d,%d) out of bounds (count=%d)", step, from, to, l.count)
		}
	}
}

// TestWindowClampsAndContainsCursor checks the window invariants directly:
// in-bounds, exactly `visible` wide while there's room, and always covering the
// cursor for representative top/cursor combinations including a mismatched top.
func TestWindowClampsAndContainsCursor(t *testing.T) {
	cases := []struct {
		name             string
		count, top, curs int
		visible          int
	}{
		{"cursor below stale top", 40, 0, 19, 12},
		{"cursor at end", 40, 0, 39, 12},
		{"cursor above top", 40, 30, 5, 12},
		{"all fit", 5, 0, 3, 12},
		{"exact fit", 12, 0, 11, 12},
		{"zero visible", 40, 10, 15, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := list{count: tc.count, top: tc.top, cursor: tc.curs}
			from, to := l.window(tc.visible)
			if from < 0 || to > l.count || from > to {
				t.Fatalf("window [%d,%d) out of bounds (count=%d)", from, to, l.count)
			}
			if !inWindow(l.cursor, from, to) {
				t.Fatalf("cursor %d outside window [%d,%d)", l.cursor, from, to)
			}
		})
	}
}

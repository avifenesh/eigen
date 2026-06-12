package tui

// Drag-to-resize for the side panels (Tier 11): the rail's separator column
// (its rightmost cell) and the right panel's gutter column (its leftmost cell)
// are grabbable edges. Press starts a drag, motion resizes live, release ends
// it. Widths are clamped (per-panel min/max + the transcript's minimum) and
// persist for the window's lifetime in model.railW / model.rightW.

// panelResizeStep is the column delta for keyboard-driven panel resizing
// (palette actions); dragging the edge gives per-column control.
const panelResizeStep = 4

// resizeEdgeAt reports whether (x,y) is on a grabbable panel edge, and which
// panel it belongs to. The edge is the 1-column separator the panels already
// render, spanning the transcript band rows — except each panel's header row
// (row 0), where the close [x] / tab bar keeps priority.
func (m *model) resizeEdgeAt(x, y int) (region, bool) {
	l := m.computeLayout()
	if !l.leftRail.empty() && y > l.leftRail.y && y < l.leftRail.y+l.leftRail.h &&
		x == l.leftRail.x+l.leftRail.w-1 {
		return regLeftRail, true
	}
	if !l.rightPanel.empty() && y > l.rightPanel.y && y < l.rightPanel.y+l.rightPanel.h &&
		x == l.rightPanel.x {
		return regRightPanel, true
	}
	return regNone, false
}

// applyResizeDrag resizes the panel being dragged so its edge lands on column
// x. The setters clamp to per-panel bounds and reflow the layout.
func (m *model) applyResizeDrag(x int) {
	switch m.resizing {
	case regLeftRail:
		// The rail spans [0, w): dragging the separator to column x makes the
		// width x+1 (the separator is the last column).
		m.setRailW(x + 1)
	case regRightPanel:
		// The right panel spans [width-w, width): dragging its left edge to
		// column x makes the width width-x.
		m.setRightW(m.width - x)
	}
}

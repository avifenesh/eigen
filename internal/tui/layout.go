package tui

// Screen geometry + hit-testing foundation (Tier 9 Wave 0). The chat window is
// growing chrome — a header, side rails, panels — around the transcript, all
// clickable. A scalar top/bottom offset can't survive rails that shift the
// transcript origin horizontally, so layout is modeled as named rectangles
// recomputed from model state, and every mouse event is resolved to a region
// (with explicit z-order) before any handler runs. This is the single source
// of truth shared by rendering offsets and mouse mapping.

// rect is an absolute screen rectangle, origin top-left, 0-based. A zero-width
// or zero-height rect is "absent" (a chrome element not currently shown).
type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool {
	return r.w > 0 && r.h > 0 && x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

func (r rect) empty() bool { return r.w <= 0 || r.h <= 0 }

// region names a screen area for hit-testing.
type region int

const (
	regNone region = iota
	regPlan
	regTranscript
	regSpinner
	regComp
	regInput
	regStatus
	regHeader     // Wave 2
	regLeftRail   // Wave 3
	regRightPanel // Wave 4
)

// layout holds the absolute rects of every screen area. Computed by
// computeLayout from the current model state + terminal size; read by the View
// (rendering offsets) and by hitTest (mouse mapping). Rects for chrome not yet
// shown (header/rails/panels) stay zero (empty) until their wave lands.
type layout struct {
	plan       rect
	transcript rect
	spinner    rect
	comp       rect
	input      rect
	status     rect
	header     rect
	leftRail   rect
	rightPanel rect
}

// computeLayout derives the screen rectangles from the current model state. It
// mirrors the same accounting as topHeight/inputTopRow/bottomHeight (which size
// the viewport in relayout) so the read-side geometry can never drift from the
// write-side sizing. vp.Height is the independent variable (set by relayout);
// everything else is positioned around it.
func (m *model) computeLayout() layout {
	var l layout
	w := m.width
	top := m.topHeight()
	// Plan panel occupies the top rows (height 0 when there are no todos).
	l.plan = rect{x: 0, y: 0, w: w, h: top}
	// Transcript viewport.
	l.transcript = rect{x: 0, y: top, w: w, h: m.vp.Height}
	y := top + m.vp.Height
	if m.pending != nil {
		// Approval prompt replaces the spinner+input with a single line.
		l.input = rect{x: 0, y: y, w: w, h: 1}
		y++
		l.status = rect{x: 0, y: y, w: w, h: m.statusBarHeight()}
		return l
	}
	if m.ov.active {
		y++ // overlay confirm/text line (rendered atop the bottom block)
	}
	if m.state == stRunning {
		l.spinner = rect{x: 0, y: y, w: w, h: 1}
		y++
	}
	if m.comp.active() {
		h := m.comp.rows()
		l.comp = rect{x: 0, y: y, w: w, h: h}
		y += h
	}
	ih := m.inputRows()
	l.input = rect{x: 0, y: y, w: w, h: ih}
	y += ih
	l.status = rect{x: 0, y: y, w: w, h: m.statusBarHeight()}
	return l
}

// hit is the resolved target of a mouse position: the region it fell in, the
// action it triggers (for clickable chrome — actNone otherwise), and the
// coordinates local to that region's rect (origin at the rect's top-left).
type hit struct {
	region         region
	action         actionID
	localX, localY int
}

// hitTest resolves an absolute screen (x,y) to a region + optional action, in
// explicit z-order: modal overlays are handled by their own key/mouse capture
// before this is called, so here the order is chrome (header/status) above
// rails/panels above input above transcript. Returns regNone outside any rect.
func (m *model) hitTest(x, y int) hit {
	l := m.computeLayout()
	local := func(r rect) (int, int) { return x - r.x, y - r.y }
	// Header + status chrome first (top z-order among non-modal surfaces).
	if l.header.contains(x, y) {
		lx, ly := local(l.header)
		return hit{region: regHeader, action: m.headerActionAt(lx, ly), localX: lx, localY: ly}
	}
	if l.status.contains(x, y) {
		lx, ly := local(l.status)
		return hit{region: regStatus, action: m.statusActionAt(x, y), localX: lx, localY: ly}
	}
	// Side rails / panels.
	if l.leftRail.contains(x, y) {
		lx, ly := local(l.leftRail)
		return hit{region: regLeftRail, localX: lx, localY: ly}
	}
	if l.rightPanel.contains(x, y) {
		lx, ly := local(l.rightPanel)
		return hit{region: regRightPanel, localX: lx, localY: ly}
	}
	// Input box.
	if l.input.contains(x, y) {
		lx, ly := local(l.input)
		return hit{region: regInput, localX: lx, localY: ly}
	}
	// Completion menu.
	if l.comp.contains(x, y) {
		lx, ly := local(l.comp)
		return hit{region: regComp, localX: lx, localY: ly}
	}
	// Transcript (largest area; lowest z-order).
	if l.transcript.contains(x, y) {
		lx, ly := local(l.transcript)
		return hit{region: regTranscript, localX: lx, localY: ly}
	}
	if l.plan.contains(x, y) {
		lx, ly := local(l.plan)
		return hit{region: regPlan, localX: lx, localY: ly}
	}
	return hit{region: regNone}
}

// headerActionAt resolves a click within the header rect to an action (Wave 2).
// Returns actNone until the header lands.
func (m *model) headerActionAt(localX, localY int) actionID { return actNone }

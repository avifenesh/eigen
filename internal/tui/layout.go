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
	regComposer // voice controls bar under the input (Tier 15)
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
	composer   rect // voice controls bar under the input
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
	hh := m.headerHeight()
	// Header occupies the first row(s); the plan panel follows it.
	l.header = rect{x: 0, y: 0, w: w, h: hh}
	l.plan = rect{x: 0, y: hh, w: w, h: top - hh}
	// Transcript viewport (shifted right by the rail column, narrowed on the
	// right by the changes panel, when shown).
	rw := m.railWidth()
	pw := m.rightPanelWidth()
	if rw > 0 {
		l.leftRail = rect{x: 0, y: top, w: rw, h: m.vp.Height}
	}
	l.transcript = rect{x: rw, y: top, w: m.width - rw - pw, h: m.vp.Height}
	if pw > 0 {
		l.rightPanel = rect{x: m.width - pw, y: top, w: pw, h: m.vp.Height}
	}
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
	if m.composerBarVisible() {
		l.composer = rect{x: 0, y: y, w: w, h: 1}
		y++
	}
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
		a := actNone
		if m.sidebarVisible() {
			// Sidebar rows resolve in the click handler via sidebarRowAt
			// (nav rows carry their own action; no [x] close affordance —
			// the sidebar IS the chrome).
			if r, ok := m.sidebarRowAt(ly, l.leftRail.h); ok {
				a = r.action
			}
		} else if panelCloseAt(lx, ly, l.leftRail.w-1) { // rail's last col is the separator
			a = actRailToggle
		}
		return hit{region: regLeftRail, action: a, localX: lx, localY: ly}
	}
	if l.rightPanel.contains(x, y) {
		lx, ly := local(l.rightPanel)
		a := actNone
		if panelCloseAt(lx-2, ly, l.rightPanel.w-2) { // panel starts with "│ " gutter
			a = actChangesToggle
		}
		return hit{region: regRightPanel, action: a, localX: lx, localY: ly}
	}
	// Composer bar (voice controls under the input).
	if l.composer.contains(x, y) {
		lx, ly := local(l.composer)
		return hit{region: regComposer, action: m.composerActionAt(lx), localX: lx, localY: ly}
	}
	// Input box. While a turn is running, its top row is the status/spinner
	// line — clicking it backgrounds the turn (a zellij-safe affordance, since
	// ctrl+z is captured). Other rows are the text input.
	if l.input.contains(x, y) {
		lx, ly := local(l.input)
		a := actNone
		if ly == 0 && m.state == stRunning && m.canBackgroundTurn() {
			a = actBackgroundTurn
		}
		return hit{region: regInput, action: a, localX: lx, localY: ly}
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

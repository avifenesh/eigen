package app

// Central layout + hit-testing geometry for the app shell (Tier 10). Like the
// chat TUI's layout.go, this is the single source of truth shared by rendering
// and (Wave 2+) mouse mapping, so they can never drift. The shell computes
// named rectangles from the terminal size + a breakpoint; each page is handed
// the INNER content rect (post border/padding) so its own list windows size
// correctly.

import "github.com/charmbracelet/lipgloss"

// rect is an absolute screen rectangle, origin top-left, 0-based. A zero-width
// or zero-height rect is "absent".
type rect struct{ x, y, w, h int }

func (r rect) empty() bool { return r.w <= 0 || r.h <= 0 }

func (r rect) contains(x, y int) bool {
	return r.w > 0 && r.h > 0 && x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// breakpoint classifies the terminal width so the shell can adapt density.
type breakpoint int

const (
	bpNarrow breakpoint = iota // no rail border / compact
	bpNormal                   // rail + content
	bpWide                     // rail + content + right inspector
)

// Layout constants. Widths are OUTER (including borders); inner width is
// derived via the frame size so border math is never hardcoded as "minus 2".
const (
	railWidthNormal = 18  // outer rail width at the normal breakpoint
	railWidthWide   = 22  // a touch wider when there's room
	inspectorWidth  = 34  // right inspector panel (wide breakpoint)
	bpWideMin       = 130 // ≥ this many cols → show the right inspector
	bpNarrowMax     = 72  // ≤ this many cols → compact (rail loses its border)
)

// appLayout holds the absolute rects of the shell's regions.
type appLayout struct {
	bp        breakpoint
	title     rect // top title/breadcrumb bar (1 row)
	rail      rect // left rail OUTER (with border)
	railInner rect // left rail content area (inside the border)
	content   rect // content panel OUTER (with border)
	inner     rect // content INNER area handed to pages
	inspector rect // right inspector OUTER (wide only; empty otherwise)
	inspInner rect // right inspector inner
	status    rect // bottom status/help bar (1 row)
}

// frame sizes for the bordered panels, derived from the styles (so a style
// change can't silently break the math).
func railFrame() (w, h int) {
	return sRailBox.GetHorizontalFrameSize(), sRailBox.GetVerticalFrameSize()
}
func contentFrame() (w, h int) {
	return sContentBox.GetHorizontalFrameSize(), sContentBox.GetVerticalFrameSize()
}

// computeLayout derives the shell rectangles for the given terminal size.
func (m *Model) computeLayout() appLayout {
	var l appLayout
	W, H := m.width, m.height
	if W <= 0 || H <= 0 {
		return l
	}
	l.bp = breakpointFor(W)

	// Title bar (row 0) and status bar (last row) bracket the body.
	l.title = rect{x: 0, y: 0, w: W, h: 1}
	l.status = rect{x: 0, y: H - 1, w: W, h: 1}
	bodyY := 1
	bodyH := H - 2 // minus title + status
	if bodyH < 1 {
		bodyH = 1
	}

	railOuter := railWidthNormal
	if l.bp == bpWide {
		railOuter = railWidthWide
	}
	if l.bp == bpNarrow {
		railOuter = railWidthNormal // still drawn, just without border padding
	}

	// Optional right inspector at the wide breakpoint.
	inspOuter := 0
	if l.bp == bpWide {
		inspOuter = inspectorWidth
	}

	rfw, rfh := railFrame()
	cfw, cfh := contentFrame()

	// A one-column gutter separates the panels — cleaner edges and a no-op
	// hit zone between regions. Narrow drops it to reclaim space.
	gutter := 1
	if l.bp == bpNarrow {
		gutter = 0
	}

	// Rail outer rect.
	l.rail = rect{x: 0, y: bodyY, w: railOuter, h: bodyH}
	l.railInner = rect{
		x: l.rail.x + rfw/2, y: l.rail.y + rfh/2,
		w: railOuter - rfw, h: bodyH - rfh,
	}

	// Content outer rect fills between rail and inspector (with gutters).
	contentX := railOuter + gutter
	contentOuter := W - contentX - inspOuter
	if inspOuter > 0 {
		contentOuter -= gutter // gap before the inspector too
	}
	if contentOuter < 20 {
		contentOuter = 20
	}
	l.content = rect{x: contentX, y: bodyY, w: contentOuter, h: bodyH}
	l.inner = rect{
		x: l.content.x + cfw/2, y: l.content.y + cfh/2,
		w: contentOuter - cfw, h: bodyH - cfh,
	}

	// Right inspector (wide only).
	if inspOuter > 0 {
		ix := W - inspOuter
		l.inspector = rect{x: ix, y: bodyY, w: inspOuter, h: bodyH}
		l.inspInner = rect{
			x: l.inspector.x + cfw/2, y: l.inspector.y + cfh/2,
			w: inspOuter - cfw, h: bodyH - cfh,
		}
	}
	return l
}

// breakpointFor classifies a terminal width.
func breakpointFor(w int) breakpoint {
	switch {
	case w <= bpNarrowMax:
		return bpNarrow
	case w >= bpWideMin:
		return bpWide
	default:
		return bpNormal
	}
}

// box styles for the framed panels — defined here so layout's frame math and
// the renderer use the SAME styles.
var (
	// The rail box lifts onto the Surface tint (elevation — matches the chat
	// rail, so the dashboard and chat read as one product). A faint brand-tinted
	// hairline border.
	sRailBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cFaint).
			Background(cSurface)
	// The content box stays on the Base canvas with a hairline frame.
	sContentBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cFaint)
)

package app

// Mouse hit-testing for the app shell (Tier 10 Wave 2). Clicks resolve to a
// region + a stable target (page / live-session id / content-local point)
// against the SAME rects computeLayout produced for rendering, so the two can
// never drift. Wave 2 handles the global chrome (rail pages, live entries,
// title/footer actions, content delegation); page-local row hits land in
// Wave 3.

// hitRegion names where a click fell.
type hitRegion int

const (
	hitNone hitRegion = iota
	hitTitle
	hitRail      // a rail page row
	hitRailLive  // a live-session entry in the rail
	hitContent   // inside the content panel (page-local, Wave 3)
	hitInspector // inside the right inspector
	hitStatus    // the status/help bar
)

// appHit is the resolved target of a click.
type appHit struct {
	region hitRegion
	page   Page   // valid when region == hitRail
	liveID string // valid when region == hitRailLive
	// localX/localY are coordinates relative to the content (or inspector)
	// inner rect, for page-local hit maps in Wave 3.
	localX, localY int
}

// hitTest resolves an absolute screen (x,y) to a region + target, in z-order:
// title/status chrome, then rail (pages + live), then content / inspector.
// Border cells between panels fall through to hitNone (no-op zones).
func (m *Model) hitTest(x, y int) appHit {
	l := m.computeLayout()
	switch {
	case l.title.contains(x, y):
		return appHit{region: hitTitle}
	case l.status.contains(x, y):
		return appHit{region: hitStatus}
	case l.railInner.contains(x, y):
		return m.railHitAt(l, x, y)
	case !l.inner.empty() && l.inner.contains(x, y):
		return appHit{region: hitContent, localX: x - l.inner.x, localY: y - l.inner.y}
	case !l.inspInner.empty() && l.inspInner.contains(x, y):
		return appHit{region: hitInspector, localX: x - l.inspInner.x, localY: y - l.inspInner.y}
	}
	return appHit{region: hitNone}
}

// railHitAt maps a click inside the rail's inner area to a page row or a live
// entry. The rail content renders, top-down: one row per page (in `pages`
// order), then a "live" divider, then up to railLiveMax live sessions. The row
// math here MUST match railContent's layout.
func (m *Model) railHitAt(l appLayout, x, y int) appHit {
	row := y - l.railInner.y
	if row < 0 {
		return appHit{region: hitNone}
	}
	// Page rows: 0 .. len(pages)-1.
	if row < len(pages) {
		return appHit{region: hitRail, page: pages[row].page}
	}
	// The "─── live" divider row, then the live entries.
	if len(m.data.Live) == 0 {
		return appHit{region: hitNone}
	}
	liveStart := len(pages) + 1 // +1 for the divider row
	idx := row - liveStart
	if idx < 0 || idx >= len(m.data.Live) || idx >= railLiveMax {
		return appHit{region: hitNone}
	}
	return appHit{region: hitRailLive, liveID: m.data.Live[idx].ID}
}

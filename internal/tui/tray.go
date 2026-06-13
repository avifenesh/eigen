package tui

import (
	"strings"

	"github.com/avifenesh/eigen/internal/chat"
)

// trayItem is one actionable row in the notifications/approvals tray: a sibling
// session that needs the user (blocked on an approval, or errored). Selecting
// it hops the window there. sessionID is empty for the current window's own
// pending approval (handled in place, not by hopping).
type trayItem struct {
	sessionID string // "" = this window's own pending approval
	title     string
	dir       string
	status    string // "approval" | "error"
	current   bool   // is this the session shown in this window?
}

// openTray opens the notifications/approvals tray: an at-a-glance list of
// sibling daemon sessions that need attention (approval-blocked or errored),
// plus a ring of recent notifications. Works for any chat; the sessions list
// is empty for a local (non-daemon) chat, leaving just this window's own
// pending approval + recent notes.
func (m *model) openTray() {
	m.trayItems = m.buildTrayItems()
	m.trayIdx = 0
	m.tray = true
	m.sync()
}

// buildTrayItems gathers the "needs you" rows: this window's pending approval
// first (if any), then sibling daemon sessions whose status is approval/error.
func (m *model) buildTrayItems() []trayItem {
	var items []trayItem
	var curID string
	if sl, ok := m.backend.(interface{ SessionID() string }); ok {
		curID = sl.SessionID()
	}
	// This window's own pending approval leads — it's the most immediate ask.
	if m.pending != nil {
		items = append(items, trayItem{
			title:   "approve " + m.pending.name + " " + compact(m.pending.args),
			status:  "approval",
			current: true,
		})
	}
	if sl, ok := m.backend.(chat.SessionLister); ok {
		for _, e := range sl.Sessions() {
			if e.Status != "approval" && e.Status != "error" {
				continue
			}
			if e.ID == curID && m.pending != nil {
				continue // already shown as the in-window pending approval
			}
			title := e.Title
			if title == "" {
				title = e.ID
			}
			items = append(items, trayItem{
				sessionID: e.ID,
				title:     title,
				dir:       e.Dir,
				status:    e.Status,
				current:   e.ID == curID,
			})
		}
	}
	return items
}

// trayActivate acts on the selected tray row: a sibling session hops the
// window there; the current window's own pending approval just closes the tray
// (the in-place y/n prompt is already visible). Returns quit=true when the
// window should hop to another session.
func (m *model) trayActivate() (handled, quit bool) {
	if m.trayIdx < 0 || m.trayIdx >= len(m.trayItems) {
		return false, false
	}
	it := m.trayItems[m.trayIdx]
	m.tray = false
	if it.sessionID == "" || it.current {
		// Own/current session — nothing to hop to; the approval prompt (if
		// any) is already in place.
		m.sync()
		return true, false
	}
	m.switchTo = it.sessionID
	return true, true
}

// trayView renders the tray overlay: a "needs you" section (actionable rows)
// then recent notifications.
func (m *model) trayView() string {
	var b strings.Builder
	b.WriteString(styleUser.Render("needs you") + "\n\n")
	if len(m.trayItems) == 0 {
		b.WriteString(dim("  nothing waiting — all sessions idle") + "\n")
	} else {
		for i, it := range m.trayItems {
			cursor := "  "
			if i == m.trayIdx {
				cursor = styleSel.Render("▎ ") // selected row — non-brand
			}
			label := it.title
			if it.current {
				label += dim(" · this window")
			} else if it.dir != "" {
				label += dim(" · " + projBase(it.dir))
			}
			b.WriteString(cursor + statusGlyph(it.status) + " " + ansiTrunc(label, m.width-8) + "\n")
		}
	}
	if len(m.notif) > 0 {
		b.WriteString("\n" + styleUser.Render("recent") + "\n\n")
		// Newest first, last few.
		n := len(m.notif)
		show := 8
		if n < show {
			show = n
		}
		for i := 0; i < show; i++ {
			b.WriteString(dim("  · "+ansiTrunc(m.notif[n-1-i], m.width-6)) + "\n")
		}
	}
	b.WriteString("\n" + dim("enter open · ↑/↓ move · esc close"))
	return b.String()
}

// projBase is the base name of a project dir ("" → "").
func projBase(dir string) string {
	if i := strings.LastIndexByte(dir, '/'); i >= 0 && i < len(dir)-1 {
		return dir[i+1:]
	}
	return dir
}

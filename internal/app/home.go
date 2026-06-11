package app

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avifenesh/eigen/internal/feed"
)

// homeState is the landing page: identity, quick stats, the proactive action
// feed (one-keystroke session starters scanned from git/memory/GitHub), and
// recent sessions. One cursor walks feed items then sessions.
type homeState struct {
	list     list
	feed     []feed.Item // filtered (dismissals applied), capped
	feedN    int         // feed items shown (cursor 0..feedN-1 = feed)
	sessionN int         // sessions shown   (cursor feedN.. = sessions)
}

// homeFeedLimit / homeRecentLimit bound the two home sections.
const (
	homeFeedLimit   = 6
	homeRecentLimit = 6
)

func (h *homeState) init(d *Data) {
	h.syncFeed(d)
}

// syncFeed recomputes the filtered feed + section sizes (called when the
// feed, dismissals, or sessions change).
func (h *homeState) syncFeed(d *Data) {
	// Top-ranked with per-kind diversity: a busy GitHub week can't crowd
	// your own uncommitted work off the page.
	h.feed = feed.Top(d.feedItems(), homeFeedLimit, 3)
	h.feedN = len(h.feed)
	h.sessionN = min(len(d.Sessions), homeRecentLimit)
	h.list.count = h.feedN + h.sessionN
	h.list.clamp()
}

func (h *homeState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	h.syncFeed(m.data) // sessions/feed may have refreshed behind us
	key := msg.String()
	if h.list.key(key, h.list.count) {
		return m, nil
	}
	switch key {
	case "enter":
		c := h.list.cursor
		switch {
		case c < h.feedN:
			// A feed item: open a chat rooted at its project with the task
			// pre-submitted — the one-keystroke session starter.
			it := h.feed[c]
			m.result = Result{Action: ActionOpenChat, Dir: it.Dir, Task: it.Task}
			m.quitting = true
			return m, tea.Quit
		case c-h.feedN < h.sessionN:
			r := m.data.Sessions[c-h.feedN]
			m.result = openAction(r)
			m.quitting = true
			return m, tea.Quit
		}
	case "n":
		m.result = Result{Action: ActionOpenChat}
		m.quitting = true
		return m, tea.Quit
	case "d":
		// Dismiss the selected feed item (stops appearing for 2 weeks, or
		// until its content changes — e.g. the dirty-file count moves).
		if c := h.list.cursor; c < h.feedN {
			feed.Dismiss(h.feed[c])
			h.syncFeed(m.data)
		}
	}
	return m, nil
}

func (h *homeState) view(m *Model, w, _ int) string {
	d := m.data
	h.syncFeed(d)
	s := pageTitle("eigen", "your agent, everywhere", w)

	// Quick stats line: informative at a glance.
	s += fmt.Sprintf("  %s   %s   %s\n\n",
		countLabel(len(d.Sessions), "session"),
		countLabel(len(d.Projects), "project"),
		countLabel(d.Skills.Len(), "skill"))

	// The proactive feed: offered actions, one keystroke to start.
	if h.feedN > 0 {
		s += sDim.Render("  act on") + "\n"
		for i := 0; i < h.feedN; i++ {
			it := h.feed[i]
			line := fmt.Sprintf("%s %s %s",
				kindGlyph(it.Kind),
				pad(truncate(it.Title, w-26), w-26),
				sDim.Render(truncate(it.Detail, 18)))
			s += row(i == h.list.cursor, line) + "\n"
		}
		s += "\n"
	} else if !d.FeedFresh {
		s += sFaint.Render("  scanning for things to act on…") + "\n\n"
	}

	s += sDim.Render("  recent") + "\n"
	if h.sessionN == 0 {
		s += sFaint.Render("  no sessions yet — press n to start one") + "\n"
		return s
	}
	for i := 0; i < h.sessionN; i++ {
		r := d.Sessions[i]
		line := fmt.Sprintf("%s %s %s",
			pad(truncate(r.Title, w-30), w-30),
			sViolet.Render(pad(fmt.Sprintf("%d msg", r.Msgs), 8)),
			sDim.Render(relTime(r.Updated)))
		s += row(h.feedN+i == h.list.cursor, line) + "\n"
	}
	s += "\n" + sFaint.Render("  enter act/open · d dismiss · n new session · s sessions · p projects")
	return s
}

// kindGlyph marks a feed item's source: git loose ends, GitHub asks, memory
// intents — colored to match their nature (warn = your mess, violet = the
// world, accent = your own promises).
func kindGlyph(kind string) string {
	switch kind {
	case "git":
		return sWarn.Render("±")
	case "github":
		return sViolet.Render("◉")
	case "memory":
		return sAccent.Render("↺")
	}
	return sDim.Render("·")
}

// relTime renders a compact relative timestamp ("3h", "2d", "now").
func relTime(unixNano int64) string {
	if unixNano == 0 {
		return ""
	}
	d := time.Since(time.Unix(0, unixNano))
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	}
}

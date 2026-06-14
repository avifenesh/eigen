package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avifenesh/eigen/internal/feed"
)

// homeState is the landing page: identity, quick stats, the proactive action
// feed (one-keystroke session starters scanned from git/memory/GitHub plus
// small-model suggestions), and recent sessions. One cursor walks feed items
// then sessions; space expands the selected feed item to show the full task.
type homeState struct {
	list     list
	feed     []feed.Item // filtered (dismissals applied), capped
	feedN    int         // feed items shown (cursor 0..feedN-1 = feed)
	sessionN int         // sessions shown   (cursor feedN.. = sessions)
	expanded int         // index of the expanded feed item (-1 = none)
	clicks   clickMap
}

// homeFeedLimit / homeRecentLimit bound the two home sections.
const (
	homeFeedLimit   = 6
	homeRecentLimit = 6
)

func (h *homeState) init(d *Data) {
	h.expanded = -1
	h.syncFeed(d)
}

// syncFeed recomputes the filtered feed + section sizes (called when the
// feed, dismissals, or sessions change).
func (h *homeState) syncFeed(d *Data) {
	// Top-ranked with per-kind diversity: a busy GitHub week can't crowd
	// your own uncommitted work off the page.
	h.feed = feed.Top(d.feedItems(), homeFeedLimit, 3)
	h.feedN = len(h.feed)
	if h.expanded >= h.feedN {
		h.expanded = -1
	}
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
	case "d", "x":
		// Remove the selected feed item (dismiss: stops appearing for 2 weeks,
		// or until its content changes — e.g. the dirty-file count moves).
		if c := h.list.cursor; c < h.feedN {
			feed.Dismiss(h.feed[c])
			if h.expanded == c {
				h.expanded = -1
			}
			h.syncFeed(m.data)
		}
	case " ", "space":
		// Expand/collapse the selected feed item: shows the FULL offered task
		// (what a new session would actually be told to do).
		if c := h.list.cursor; c < h.feedN {
			if h.expanded == c {
				h.expanded = -1
			} else {
				h.expanded = c
			}
		}
	}
	return m, nil
}

func (h *homeState) view(m *Model, w, _ int) string {
	d := m.data
	h.syncFeed(d)
	h.clicks.reset()

	// Brand banner: the λ mark + a time-of-day greeting + at-a-glance stats —
	// the first screen has a face and feels lived-in, not a bare title.
	s := sTitle.Bold(true).Render("λ eigen")
	s += "  " + sDim.Render(homeGreeting()) + "\n"
	s += "  " + fmt.Sprintf("%s   %s   %s",
		countLabel(len(d.Sessions), "session"),
		countLabel(len(d.Projects), "project"),
		countLabel(d.Skills.Len(), "skill")) + "\n"
	s += sFaint.Render(strings.Repeat("─", min(w, 60))) + "\n\n"

	// The proactive feed: offered actions, one keystroke to start.
	if h.feedN > 0 {
		s += sDim.Render("  act on") + "\n"
		for i := 0; i < h.feedN; i++ {
			it := h.feed[i]
			line := fmt.Sprintf("%s %s %s",
				kindGlyph(it.Kind),
				pad(truncate(it.Title, w-26), w-26),
				sDim.Render(truncate(it.Detail, 18)))
			h.clicks.mark(lineCount(s), i) // feed item i at this line
			s += row(i == h.list.cursor, line) + "\n"
			if h.expanded == i {
				s += renderTask(it, w) // the full offered task under the row
			}
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
		h.clicks.mark(lineCount(s), h.feedN+i) // recent session at unified index
		s += row(h.feedN+i == h.list.cursor, line) + "\n"
	}
	s += "\n" + sFaint.Render("  enter act · space details · x remove · n new · s sessions · p projects")
	return s
}

// clickAt handles a content-local click on the home page: select the row, or
// activate it (open the feed item / resume the session) if already selected.
func (h *homeState) clickAt(m *Model, localY int) (tea.Cmd, bool) {
	idx, ok := h.clicks.at(localY)
	if !ok {
		return nil, false
	}
	if idx < 0 || idx >= h.list.count {
		return nil, false
	}
	if h.list.cursor != idx {
		h.list.cursor = idx
		return nil, true
	}
	// Second click on the selected row → activate.
	switch {
	case idx < h.feedN:
		it := h.feed[idx]
		m.result = Result{Action: ActionOpenChat, Dir: it.Dir, Task: it.Task}
	case idx-h.feedN < h.sessionN:
		m.result = openAction(m.data.Sessions[idx-h.feedN])
	default:
		return nil, true
	}
	m.quitting = true
	return tea.Quit, true
}

// renderTask renders the full task text of an expanded feed item, wrapped and
// indented under its row, with the project dir for orientation.
func renderTask(it feed.Item, w int) string {
	out := ""
	if it.Dir != "" {
		out += sFaint.Render("    ↳ "+it.Dir) + "\n"
	}
	for _, para := range strings.Split(strings.TrimSpace(it.Task), "\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		out += sDim.Render("    "+wrapTo(para, w-6, "    ")) + "\n"
	}
	return out
}

// kindGlyph marks a feed item's source: git loose ends, GitHub asks, memory
// intents, model suggestions — colored to match their nature (warn = your
// mess, violet = the world, accent = your own promises, ok = a step forward).
func kindGlyph(kind string) string {
	switch kind {
	case "git":
		return sWarn.Render("±")
	case "github":
		return sViolet.Render("◉")
	case "memory":
		return sAccent.Render("↺")
	case "suggest":
		return sOk.Render("✧")
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

// homeGreeting is a warm, time-of-day line for the home banner — the dashboard
// feels lived-in rather than a cold index.
func homeGreeting() string {
	switch hh := time.Now().Hour(); {
	case hh < 5:
		return "burning the midnight oil — what's next?"
	case hh < 12:
		return "good morning — what are we building?"
	case hh < 17:
		return "good afternoon — what are we building?"
	case hh < 22:
		return "good evening — what are we building?"
	default:
		return "late night — what are we building?"
	}
}

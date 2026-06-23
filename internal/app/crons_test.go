package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHumanizeMicros(t *testing.T) {
	if got := humanizeMicros(0); got != "—" {
		t.Fatalf("zero: %q", got)
	}
	now := time.Now()
	if got := humanizeMicros(now.UnixMicro()); !strings.HasPrefix(got, "today ") {
		t.Fatalf("today: %q", got)
	}
	future := now.AddDate(0, 0, 3)
	if got := humanizeMicros(future.UnixMicro()); !strings.HasPrefix(got, future.Format("2006-01-02")) {
		t.Fatalf("future: %q", got)
	}
}

func TestLoadCrontabParsing(t *testing.T) {
	// Pure-parse test through the row builder: crontab may not exist in CI;
	// loadCrons must not panic either way.
	_ = loadCrons()
}

// TestCronsViewKeepsCursorOnScreenAfterEnd guards APP-078: update() scrolls
// l.top with a (larger) m.height-6 estimate while view() renders h-8 rows. After
// G (end) the selected row must still appear in the rendered output — the view
// must window() on the cursor, not read l.top raw (which would scroll the active
// timer off the bottom).
func TestCronsViewKeepsCursorOnScreenAfterEnd(t *testing.T) {
	var c cronsState
	for i := 0; i < 40; i++ {
		c.rows = append(c.rows, CronRow{
			Name:   "unit-" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Kind:   "timer",
			Next:   "today 07:00",
			Active: true,
		})
	}
	c.list.count = len(c.rows)
	c.loaded = true // skip the shell-out in load()

	// update() sizes its window from the FULL height (m.height-6 = larger).
	m := &Model{height: 30}
	if _, _ = c.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")}); c.list.cursor != len(c.rows)-1 {
		t.Fatalf("G did not move cursor to end: cursor=%d", c.list.cursor)
	}

	// view() renders a SMALLER window (inner content height < full height).
	out := c.view(m, 80, 18) // visible = 18-8 = 10 rows
	want := c.rows[c.list.cursor].Name
	if !strings.Contains(out, want) {
		t.Fatalf("active timer %q (cursor row) not in rendered view after G", want)
	}
}

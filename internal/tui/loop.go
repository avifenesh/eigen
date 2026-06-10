package tui

// Loop: a prompt that re-submits itself every interval while the session is
// idle, until the user clears it. The inverse of steer/queue: it fires only
// when NOT running (a running turn defers the next fire to the turn's end).
// Typical use: "/loop 10m read GOALS.md and do the next unchecked item" — the
// user edits the file between iterations and never has to re-prompt the model.

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// loopMsg fires on the loop schedule; gen guards stale timers (any turn or
// loop change bumps idleGen, invalidating pending fires).
type loopMsg struct{ gen int }

// defaultLoopInterval is used when /loop is given a prompt with no interval.
const defaultLoopInterval = 10 * time.Minute

// minLoopInterval bounds how fast a loop may fire: each fire is a full model
// turn, so a too-tight loop would burn tokens unattended.
const minLoopInterval = 30 * time.Second

// scheduleLoop arms the next loop fire if a loop is configured.
func (m *model) scheduleLoop() tea.Cmd {
	if m.loopPrompt == "" || m.loopEvery <= 0 {
		return nil
	}
	gen := m.idleGen
	return tea.Tick(m.loopEvery, func(time.Time) tea.Msg { return loopMsg{gen: gen} })
}

// handleLoop fires the loop prompt if the session is still idle, or re-arms
// for later when a turn is running (fire when NOT running, never during).
func (m *model) handleLoop(msg loopMsg) tea.Cmd {
	if msg.gen != m.idleGen || m.loopPrompt == "" {
		return nil // stale timer, or loop cleared
	}
	if m.state != stInput {
		// A turn is running: do not interrupt; try again in one interval.
		return m.scheduleLoop()
	}
	m.loopRuns++
	m.note(fmt.Sprintf("loop fire #%d (every %s — /loop clear to stop)", m.loopRuns, m.loopEvery))
	return m.submit(m.loopPrompt)
}

// parseLoopArgs splits "/loop [interval] <prompt>" arguments: a leading
// duration token (10m, 90s, 1h30m) is the interval; the rest is the prompt.
func parseLoopArgs(arg string) (every time.Duration, prompt string, err error) {
	arg = strings.TrimSpace(arg)
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		return 0, "", fmt.Errorf("empty")
	}
	if d, derr := time.ParseDuration(fields[0]); derr == nil {
		if d < minLoopInterval {
			return 0, "", fmt.Errorf("interval %s too short (min %s)", d, minLoopInterval)
		}
		prompt = strings.TrimSpace(strings.TrimPrefix(arg, fields[0]))
		if prompt == "" {
			return 0, "", fmt.Errorf("missing prompt after interval")
		}
		return d, prompt, nil
	}
	return defaultLoopInterval, arg, nil
}

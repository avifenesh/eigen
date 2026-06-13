package tui

// Ping: attention signals when eigen needs the user or finishes a long turn.
// A terminal bell (BEL) works everywhere — terminals surface it as a visual
// flash, an urgency hint, or a sound; multiplexers propagate it to the right
// pane/window. An optional notify command (config notify_cmd / EIGEN_NOTIFY_CMD,
// e.g. notify-send) gets the message appended as its last argument for desktop
// notifications.

import (
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// pingMinTurn is the minimum turn duration that triggers a finished-turn ping:
// short turns mean the user is likely still watching; long ones mean they have
// tabbed away and want to be called back.
const pingMinTurn = 30 * time.Second

// bell writes BEL to the terminal. Safe always: terminals render it as flash,
// urgency hint, or sound per the user's settings.
func bell() {
	os.Stderr.WriteString("\a")
}

// setTermTitle sets the terminal window/tab title via OSC 2. Ghostty (and tmux/
// zellij/most emulators) show it in the tab/header — so a glance at the tab
// says whether eigen is working or waiting, even when the window isn't focused.
// Written to stderr (the tty), like bell().
func setTermTitle(s string) {
	os.Stderr.WriteString("\x1b]2;" + s + "\x07")
}

// setTitleThrottled writes the terminal title only when it CHANGED, so the
// fast spinner tick can call it freely without spamming the tab (a frantic
// rewrite read like a bug). The dot animation already advances on wall-clock
// seconds, so this collapses ~12 ticks/sec down to ≤1 actual rewrite/sec.
func (m *model) setTitleThrottled(s string) {
	if s == m.lastTitle {
		return
	}
	m.lastTitle = s
	setTermTitle(s)
}

// titleWorking is the tab title while a turn runs: the λ mark + slow animated
// dots. The dot count is driven by WALL-CLOCK seconds (1 dot/sec, cycling 1→3),
// NOT the in-app loader's fast framerate — so the tab breathes calmly instead
// of flickering like a bug. setTermTitle is throttled to skip no-op rewrites.
func titleWorking(secs int) string {
	dots := strings.Repeat(".", 1+secs%3)
	return brandGlyph + " eigen working" + dots
}

// titleReady is the tab title when eigen is waiting for the user — a calm,
// classic "ready" banner. Paired with the bell on turn-done so an unfocused
// tab both shows and (per the user's term settings) chimes.
func titleReady() string {
	return brandGlyph + " eigen — ready"
}

// notifyCmd returns the configured external notifier, if any.
func (m *model) notifyCmdline() string {
	if m.notifyCmd != "" {
		return m.notifyCmd
	}
	return os.Getenv("EIGEN_NOTIFY_CMD")
}

// ping signals the user: terminal bell always, external notifier when
// configured. msg is a short human label ("turn finished", "approval needed").
func (m *model) ping(msg string) {
	bell()
	cmdline := m.notifyCmdline()
	if cmdline == "" {
		return
	}
	parts := strings.Fields(cmdline)
	if len(parts) == 0 {
		return
	}
	args := append(parts[1:], "eigen: "+msg)
	cmd := exec.Command(parts[0], args...)
	// Fire and forget: a notifier must never block or break the UI.
	_ = cmd.Start()
	go func() { _ = cmd.Wait() }()
}

// pingOnTurnDone decides whether a finished turn deserves a ping: only after
// long turns (the user has likely tabbed away), and never for canceled ones.
func (m *model) pingOnTurnDone(err error) tea.Cmd {
	if m.turnStarted.IsZero() {
		return nil
	}
	dur := time.Since(m.turnStarted)
	if dur < pingMinTurn {
		return nil
	}
	label := "done"
	if err != nil {
		label = "failed"
	}
	rounded := dur.Round(time.Second).String()
	m.ping("turn " + label + " after " + rounded)
	// Also flash an in-app banner: a long turn finishing should be noticeable
	// on screen, not only via the (easily-missed) terminal bell.
	if err != nil {
		return m.showFlashTone("turn failed · "+rounded, flashBad)
	}
	return m.showFlash("turn done · " + rounded)
}

// goalNagInterval is how often eigen pings while a goal is set and the session
// sits idle: the goal is the user's declared north star, so an idle session
// with an unachieved goal is a stall worth surfacing. Cleared goals stop it.
const goalNagInterval = 5 * time.Minute

// goalNagMsg fires on the goal-nag schedule; gen guards stale timers (any new
// turn or goal change bumps idleGen, invalidating pending nags).
type goalNagMsg struct{ gen int }

// scheduleGoalNag arms the next goal nag if a goal is set. Uses the same
// generation counter as idle dreaming so any activity cancels pending nags.
func (m *model) scheduleGoalNag() tea.Cmd {
	if m.backend == nil || m.backend.Goal() == "" {
		return nil
	}
	gen := m.idleGen
	return tea.Tick(goalNagInterval, func(time.Time) tea.Msg { return goalNagMsg{gen: gen} })
}

// handleGoalNag pings if the session is still idle with the same goal, and
// re-arms the timer so the nag repeats until the goal is cleared or work
// resumes.
func (m *model) handleGoalNag(msg goalNagMsg) tea.Cmd {
	if msg.gen != m.idleGen || m.state != stInput {
		return nil // stale, or a turn is running
	}
	goal := ""
	if m.backend != nil {
		goal = m.backend.Goal()
	}
	if goal == "" {
		return nil // goal achieved/cleared: stop nagging
	}
	m.ping("goal not yet achieved: " + goal)
	m.note("goal still open: " + goal + "   (/goal clear when done — idle pings repeat every " + goalNagInterval.String() + ")")
	return m.scheduleGoalNag()
}

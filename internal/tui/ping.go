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
func (m *model) pingOnTurnDone(err error) {
	if m.turnStarted.IsZero() {
		return
	}
	dur := time.Since(m.turnStarted)
	if dur < pingMinTurn {
		return
	}
	label := "done"
	if err != nil {
		label = "failed"
	}
	m.ping("turn " + label + " after " + dur.Round(time.Second).String())
}

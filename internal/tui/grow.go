package tui

// Terminal growing (Tier 11 polish): when a side panel is toggled ON but the
// terminal is too narrow to show it, eigen asks the surrounding multiplexer to
// stretch the pane instead of silently doing nothing. zellij and tmux both
// expose a CLI that resizes the CURRENT pane from inside it; plain terminals
// can't be resized from the app, so we say so honestly.

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
)

// growDoneMsg reports the outcome of a pane-stretch attempt.
type growDoneMsg struct {
	want, got   int
	ok          bool
	unsupported bool // nothing around us would resize — user must do it manually
	triedWindow bool // we asked the terminal WINDOW itself to grow (escape code)
}

// termWidth reads the real terminal width from the tty (the model's m.width
// lags until the WindowSizeMsg arrives, so the grow loop reads directly).
func termWidth() int {
	w, _, err := term.GetSize(os.Stdout.Fd())
	if err != nil || w <= 0 {
		return 0
	}
	return w
}

// growToWidth asynchronously asks the surrounding multiplexer(s) — and, as a
// last resort, the terminal WINDOW itself — to widen this pane to at least
// target columns. Multiplexers can NEST (zellij inside tmux — the workspace
// harness does exactly this), and env vars from the outer one leak through
// the inner, so we can't trust a single env check: try the inner-most likely
// owner first (zellij), verify the width actually grew, then fall back to
// tmux. tmux resizes exactly; zellij only has stepwise directional "resize
// increase". Outside any multiplexer the only lever is the XTWINOPS resize
// escape (CSI 8 ; rows ; cols t) — some terminals honor it, some (by policy)
// don't, so the outcome is verified and reported honestly either way.
func growToWidth(target int) tea.Cmd {
	return func() tea.Msg {
		cur := termWidth()
		if cur >= target {
			return growDoneMsg{want: target, got: cur, ok: true}
		}
		tried := false
		if os.Getenv("ZELLIJ") != "" {
			tried = true
			growZellij(target)
		}
		if w := termWidth(); w < target && os.Getenv("TMUX") != "" {
			tried = true
			// Target OUR pane explicitly: without -t tmux resizes the
			// window's ACTIVE pane, which may be a neighbor.
			args := []string{"resize-pane"}
			if p := os.Getenv("TMUX_PANE"); p != "" {
				args = append(args, "-t", p)
			}
			args = append(args, "-x", strconv.Itoa(target))
			_ = exec.Command("tmux", args...).Run()
			time.Sleep(150 * time.Millisecond)
		}
		triedWindow := false
		if w := termWidth(); w < target && !tried {
			// No multiplexer owns the pane: ask the terminal window itself.
			// (Inside tmux/zellij the escape wouldn't reach the real terminal,
			// so it's only attempted bare.)
			triedWindow = growWindow(target)
			tried = tried || triedWindow
		}
		if !tried {
			return growDoneMsg{want: target, got: cur, unsupported: true}
		}
		got := termWidth()
		return growDoneMsg{want: target, got: got, ok: got >= target, triedWindow: triedWindow}
	}
}

// growWindow asks the terminal emulator to resize its WINDOW via the XTWINOPS
// escape (CSI 8 ; rows ; cols t — the same mechanism `resize -s` uses). Some
// terminals honor it (xterm and family), others refuse by policy; the caller
// verifies the resulting width. Returns whether the request could be sent.
func growWindow(targetCols int) bool {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	defer tty.Close()
	_, rows, err := term.GetSize(os.Stdout.Fd())
	if err != nil || rows <= 0 {
		rows = 0 // 0 = keep current height per XTWINOPS
	}
	if _, err := fmt.Fprintf(tty, "\x1b[8;%d;%dt", rows, targetCols); err != nil {
		return false
	}
	// Wait for the resize to land (async via the window system → SIGWINCH).
	for i := 0; i < 12; i++ {
		time.Sleep(50 * time.Millisecond)
		if termWidth() >= targetCols {
			break
		}
	}
	return true
}

// growZellij widens the current zellij pane stepwise: push the right edge out
// first, then the left edge. A pane at its limit stops growing — bail instead
// of spinning. Resize propagation (SIGWINCH → tty size) lags the CLI call, so
// progress is polled per step.
func growZellij(target int) {
	noProgress := 0
	for i := 0; i < 40; i++ {
		w := termWidth()
		if w >= target {
			return
		}
		dir := "right"
		if noProgress >= 3 {
			dir = "left" // right edge exhausted: try the left edge
		}
		if exec.Command("zellij", "action", "resize", "increase", dir).Run() != nil {
			return
		}
		// Wait for the resize to land (tty size update lags the CLI).
		grew := false
		for j := 0; j < 10; j++ {
			time.Sleep(40 * time.Millisecond)
			if nw := termWidth(); nw > w {
				grew = true
				break
			}
		}
		if grew {
			noProgress = 0
		} else {
			noProgress++
			if noProgress >= 6 {
				return // pane can't grow further (window/layout limit)
			}
		}
	}
}

// railNeededWidth is the minimum terminal width for the session rail.
func (m *model) railNeededWidth() int { return railMinTerminalWidth }

// rightNeededWidth is the minimum terminal width for the right panel beside
// the (possibly shown) rail and a usable transcript.
func (m *model) rightNeededWidth() int {
	return m.railWidth() + rightMinW + minTranscriptCols
}

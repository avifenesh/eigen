package tui

// Safe non-PTY terminal command runner tab (Tier 11). This is deliberately NOT
// an interactive terminal: one command at a time, argv parsing by shellwords-ish
// rules (no shell expansion), bounded timeout, output cap, and stale-safe result
// handling. It runs in the session dir.

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	termTimeout  = 30 * time.Second
	termMaxBytes = 24 * 1024
)

type termState struct {
	input   string
	running bool
	seq     int
	cmd     string
	out     string
	err     string
	exit    string
	dur     time.Duration
	started time.Time
	cancel  context.CancelFunc
}

type termDoneMsg struct {
	seq  int
	cmd  string
	out  string
	err  string
	exit string
	dur  time.Duration
}

func (m *model) termLines(h int) []string {
	lines := make([]string, 0, h)
	lines = append(lines, changesPad(m.rightPanelTitleLine(rightPanelWidthCols-2), rightPanelWidthCols))
	contentW := rightPanelWidthCols - 4
	prompt := "$ " + m.term.input
	if m.rightTab == rightTabTerminal && !m.term.running {
		prompt += "▌"
	}
	lines = append(lines, changesPad(ansiTrunc(prompt, contentW), rightPanelWidthCols))
	if m.term.running {
		lines = append(lines, changesPad("running: "+ansiTrunc(m.term.cmd, contentW-9), rightPanelWidthCols))
		lines = append(lines, changesPad("esc cancel", rightPanelWidthCols))
	} else if m.term.exit != "" {
		lines = append(lines, changesPad(m.term.exit+"  "+m.term.dur.Round(time.Millisecond).String(), rightPanelWidthCols))
	}
	if m.term.err != "" {
		for _, ln := range wrapPanelLines("err: "+m.term.err, contentW) {
			if len(lines) < h {
				lines = append(lines, changesPad(ln, rightPanelWidthCols))
			}
		}
	}
	if strings.TrimSpace(m.term.out) != "" {
		for _, ln := range strings.Split(strings.TrimRight(m.term.out, "\n"), "\n") {
			for _, wln := range wrapPanelLines(ln, contentW) {
				if len(lines) < h {
					lines = append(lines, changesPad(wln, rightPanelWidthCols))
				}
			}
		}
	} else if !m.term.running && m.term.exit == "" {
		lines = append(lines, changesPad("type command · enter run", rightPanelWidthCols))
		lines = append(lines, changesPad("no pipes/shell in v1", rightPanelWidthCols))
	}
	for len(lines) < h {
		lines = append(lines, changesPad("", rightPanelWidthCols))
	}
	return lines
}

func wrapPanelLines(s string, w int) []string {
	if w <= 0 || len(s) <= w {
		return []string{s}
	}
	var out []string
	for len(s) > w {
		out = append(out, s[:w])
		s = s[w:]
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}

func (m *model) termKey(key string) (tea.Cmd, bool) {
	if m.rightTab != rightTabTerminal || !m.changesOn {
		return nil, false
	}
	if m.term.running {
		if key == "esc" || key == "ctrl+c" {
			m.cancelTerm()
			return nil, true
		}
		return nil, true
	}
	switch key {
	case "esc":
		return nil, false
	case "enter":
		line := strings.TrimSpace(m.term.input)
		if line == "" {
			return nil, true
		}
		cmd, err := m.runTermCommand(line)
		if err != nil {
			m.term.err = err.Error()
			m.term.exit = "parse error"
			return nil, true
		}
		m.term.input = ""
		m.relayout()
		return func() tea.Msg { return cmd().(termDoneMsg) }, true
	case "backspace":
		if r := []rune(m.term.input); len(r) > 0 {
			m.term.input = string(r[:len(r)-1])
		}
		return nil, true
	case "ctrl+u":
		m.term.input = ""
		return nil, true
	case "space", " ":
		m.term.input += " "
		return nil, true
	default:
		if key != "" && !strings.HasPrefix(key, "ctrl+") && !strings.HasPrefix(key, "alt+") {
			m.term.input += key
			return nil, true
		}
	}
	return nil, false
}

// parseArgv splits a command line into argv with simple quotes and backslashes.
// It intentionally does not implement pipes, redirects, variables, globbing, or
// shell evaluation.
func parseArgv(line string) ([]string, error) {
	var args []string
	var b strings.Builder
	inSingle, inDouble, esc := false, false, false
	had := false
	for _, r := range line {
		switch {
		case esc:
			b.WriteRune(r)
			esc = false
			had = true
		case r == '\\' && !inSingle:
			esc = true
			had = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			had = true
		case r == '"' && !inSingle:
			inDouble = !inDouble
			had = true
		case (r == ' ' || r == '\t' || r == '\n') && !inSingle && !inDouble:
			if had || b.Len() > 0 {
				args = append(args, b.String())
				b.Reset()
				had = false
			}
		default:
			b.WriteRune(r)
			had = true
		}
	}
	if esc {
		b.WriteRune('\\')
	}
	if inSingle || inDouble {
		return nil, errors.New("unterminated quote")
	}
	if had || b.Len() > 0 {
		args = append(args, b.String())
	}
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}
	for _, a := range args {
		if strings.ContainsAny(a, "|<>") {
			return nil, errors.New("pipes/redirection need shell mode (not in v1)")
		}
	}
	return args, nil
}

func capBytes(b []byte, max int) (string, bool) {
	if len(b) <= max {
		return sanitizeOutput(string(b)), false
	}
	return sanitizeOutput(string(b[len(b)-max:])) + "\n… output truncated …", true
}

func sanitizeOutput(s string) string {
	// Strip ESC bytes to avoid OSC/title/clipboard/control-sequence surprises.
	return strings.Map(func(r rune) rune {
		if r == 0x1b {
			return -1
		}
		return r
	}, s)
}

func (m *model) runTermCommand(line string) (func() any, error) {
	argv, err := parseArgv(line)
	if err != nil {
		return nil, err
	}
	dir := m.sessionDir()
	if dir == "" {
		return nil, errors.New("no session dir")
	}
	m.term.seq++
	seq := m.term.seq
	ctx, cancel := context.WithTimeout(context.Background(), termTimeout)
	m.term.cancel = cancel
	m.term.running = true
	m.term.cmd = line
	m.term.out, m.term.err, m.term.exit = "", "", ""
	m.term.started = time.Now()
	return func() any {
		start := time.Now()
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Dir = dir
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		err := cmd.Run()
		text, _ := capBytes(out.Bytes(), termMaxBytes)
		exit := "exit 0"
		errText := ""
		if err != nil {
			exit = "error"
			errText = err.Error()
			if ctx.Err() == context.DeadlineExceeded {
				exit = "timeout"
			}
			if ctx.Err() == context.Canceled {
				exit = "canceled"
			}
		}
		return termDoneMsg{seq: seq, cmd: line, out: text, err: errText, exit: exit, dur: time.Since(start)}
	}, nil
}

func (m *model) cancelTerm() {
	if m.term.cancel != nil {
		m.term.cancel()
	}
}

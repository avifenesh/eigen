// Package tui renders an eigen run with Bubble Tea: a scrolling transcript of
// streamed model output, tool calls, and results, with inline gated approvals.
// It is one consumer of the agent's event sink; the plain CLI is another.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	styleUser   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	styleTool   = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	styleErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleReason = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleAsk    = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
)

// agentEvent wraps an agent.Event as a tea.Msg.
type agentEvent struct{ e agent.Event }

// approvalMsg asks the UI to approve a mutating tool call.
type approvalMsg struct {
	name  string
	args  json.RawMessage
	reply chan bool
}

// doneMsg signals the run finished.
type doneMsg struct {
	out string
	err error
}

type model struct {
	vp       viewport.Model
	sp       spinner.Model
	content  strings.Builder
	status   string
	pending  *approvalMsg // non-nil while awaiting a y/n answer
	finished bool
	out      string
	err      error
	width    int
	height   int
	ready    bool
}

func (m *model) Init() tea.Cmd { return m.sp.Tick }

func (m *model) append(s string) {
	m.content.WriteString(s)
	if m.ready {
		m.vp.SetContent(m.content.String())
		m.vp.GotoBottom()
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if !m.ready {
			m.vp = viewport.New(msg.Width, msg.Height-1)
			m.ready = true
		} else {
			m.vp.Width = msg.Width
			m.vp.Height = msg.Height - 1
		}
		m.vp.SetContent(m.content.String())
		m.vp.GotoBottom()
		return m, nil

	case tea.KeyMsg:
		if m.pending != nil {
			switch strings.ToLower(msg.String()) {
			case "y":
				m.pending.reply <- true
				m.append(styleStatus.Render("  approved") + "\n")
				m.pending = nil
			case "n", "esc":
				m.pending.reply <- false
				m.append(styleErr.Render("  denied") + "\n")
				m.pending = nil
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd

	case agentEvent:
		m.renderEvent(msg.e)
		return m, nil

	case approvalMsg:
		m.pending = &msg
		m.append(styleAsk.Render(fmt.Sprintf("  approve %s %s ? [y/N]", msg.name, compact(string(msg.args)))) + "\n")
		return m, nil

	case doneMsg:
		m.finished = true
		m.out, m.err = msg.out, msg.err
		return m, tea.Quit

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) renderEvent(e agent.Event) {
	switch e.Kind {
	case agent.EventTextDelta:
		m.append(e.Text)
	case agent.EventReasoningDelta:
		m.append(styleReason.Render(e.Text))
	case agent.EventToolStart:
		m.status = "running " + e.ToolName
		m.append("\n" + styleTool.Render("▸ "+e.ToolName+" "+compact(string(e.ToolArgs))) + "\n")
	case agent.EventToolResult:
		if e.IsError {
			m.append(styleErr.Render("  ✗ "+firstLine(e.Result)) + "\n")
		} else {
			m.append(styleStatus.Render("  ✓") + "\n")
		}
	case agent.EventDone:
		m.status = "done"
	}
}

func (m *model) View() string {
	if !m.ready {
		return "starting…"
	}
	var bar string
	if m.pending != nil {
		bar = styleAsk.Render("press y to approve, n to deny")
	} else if m.finished {
		bar = styleStatus.Render("done — press q to quit")
	} else {
		bar = m.sp.View() + " " + m.status
	}
	return m.vp.View() + "\n" + bar
}

// Run drives the agent under a Bubble Tea UI. task is the initial request.
func Run(a *agent.Agent, task string) (string, error) {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	m := &model{sp: sp, status: "thinking"}
	m.append(styleUser.Render("» "+task) + "\n")

	p := tea.NewProgram(m, tea.WithAltScreen())

	// The UI's approve sends a request to the program and blocks on the reply.
	a.Approve = func(ctx context.Context, name string, args json.RawMessage) (bool, error) {
		reply := make(chan bool, 1)
		p.Send(approvalMsg{name: name, args: args, reply: reply})
		select {
		case ok := <-reply:
			return ok, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	a.OnEvent = func(e agent.Event) { p.Send(agentEvent{e}) }

	go func() {
		out, err := a.Run(context.Background(), task)
		p.Send(doneMsg{out: out, err: err})
	}()

	final, err := p.Run()
	if err != nil {
		return "", err
	}
	fm := final.(*model)
	return fm.out, fm.err
}

func compact(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return compact(s)
}

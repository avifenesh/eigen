// Package tui renders an eigen session with Bubble Tea: a multi-turn REPL with
// a scrolling transcript of streamed model output, tool calls, and results,
// an input box, and inline gated approvals. It consumes the agent event sink.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
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

type uiState int

const (
	stInput uiState = iota
	stRunning
)

type agentEvent struct{ e agent.Event }

type approvalMsg struct {
	name  string
	args  json.RawMessage
	reply chan bool
}

type turnDoneMsg struct{ err error }

type submitMsg struct{ task string }

type model struct {
	vp          viewport.Model
	sp          spinner.Model
	ti          textinput.Model
	session     *agent.Session
	ctx         context.Context
	content     strings.Builder
	state       uiState
	pending     *approvalMsg
	status      string
	initialTask string
	width       int
	height      int
	ready       bool
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink, m.sp.Tick}
	if m.initialTask != "" {
		task := m.initialTask
		cmds = append(cmds, func() tea.Msg { return submitMsg{task} })
	}
	return tea.Batch(cmds...)
}

func (m *model) append(s string) {
	m.content.WriteString(s)
	if m.ready {
		m.vp.SetContent(m.content.String())
		m.vp.GotoBottom()
	}
}

func (m *model) submit(task string) tea.Cmd {
	m.append(styleUser.Render("» "+task) + "\n")
	m.state = stRunning
	m.status = "thinking"
	m.ti.Blur()
	return tea.Batch(m.sp.Tick, func() tea.Msg {
		_, err := m.session.Send(m.ctx, task)
		return turnDoneMsg{err: err}
	})
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		vpH := msg.Height - 2
		if vpH < 1 {
			vpH = 1
		}
		if !m.ready {
			m.vp = viewport.New(msg.Width, vpH)
			m.ready = true
		} else {
			m.vp.Width = msg.Width
			m.vp.Height = vpH
		}
		m.ti.Width = msg.Width - 4
		m.vp.SetContent(m.content.String())
		m.vp.GotoBottom()
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
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
		if m.state == stInput {
			if msg.String() == "enter" {
				task := strings.TrimSpace(m.ti.Value())
				if task == "" {
					return m, nil
				}
				m.ti.Reset()
				return m, m.submit(task)
			}
			var cmd tea.Cmd
			m.ti, cmd = m.ti.Update(msg)
			return m, cmd
		}
		// running: allow scrolling the transcript
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd

	case submitMsg:
		return m, m.submit(msg.task)

	case agentEvent:
		m.renderEvent(msg.e)
		return m, nil

	case approvalMsg:
		m.pending = &msg
		m.append(styleAsk.Render(fmt.Sprintf("  approve %s %s ? [y/N]", msg.name, compact(string(msg.args)))) + "\n")
		return m, nil

	case turnDoneMsg:
		if msg.err != nil {
			m.append(styleErr.Render("error: "+msg.err.Error()) + "\n")
		}
		m.state = stInput
		m.status = ""
		m.ti.Focus()
		return m, textinput.Blink

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
		m.append("\n")
		m.status = "done"
	}
}

func (m *model) View() string {
	if !m.ready {
		return "starting…"
	}
	var bottom string
	switch {
	case m.pending != nil:
		bottom = styleAsk.Render("press y to approve, n to deny")
	case m.state == stRunning:
		bottom = m.sp.View() + " " + m.status
	default:
		bottom = m.ti.View()
	}
	return m.vp.View() + "\n" + bottom
}

// Run drives the agent under a multi-turn Bubble Tea REPL. If initialTask is
// non-empty it is submitted automatically as the first turn.
func Run(a *agent.Agent, initialTask string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	ti := textinput.New()
	ti.Placeholder = "type a task…  (enter to send · ctrl+c to quit)"
	ti.Prompt = "› "
	ti.Focus()

	m := &model{
		sp:          sp,
		ti:          ti,
		session:     a.NewSession(),
		ctx:         ctx,
		state:       stInput,
		initialTask: initialTask,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())

	a.OnEvent = func(e agent.Event) { p.Send(agentEvent{e}) }
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

	_, err := p.Run()
	return err
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

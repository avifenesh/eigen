// Package tui renders an eigen session with Bubble Tea: a multi-turn REPL with
// a scrolling transcript of streamed model output, tool calls, and results,
// an input box, and inline gated approvals. It consumes the agent event sink.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/transcript"
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
	a           *agent.Agent
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

	// rebuild signals main to rebuild eigen and exec the new binary, resuming
	// the conversation saved at rebuildPath.
	rebuild     bool
	rebuildPath string
}

// Result reports why the TUI exited.
type Result struct {
	Rebuild     bool
	SessionPath string
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
		m.sync()
	}
}

// sync wraps the transcript to the viewport width (the viewport does not
// soft-wrap on its own) and scrolls to the bottom.
func (m *model) sync() {
	content := m.content.String()
	if w := m.vp.Width; w > 0 {
		content = lipgloss.NewStyle().Width(w).Render(content)
	}
	m.vp.SetContent(content)
	m.vp.GotoBottom()
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
		m.sync()
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
				if strings.HasPrefix(task, "/") {
					return m, m.command(task)
				}
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

// command handles a /slash command typed at the input. It returns a tea.Cmd
// (tea.Quit for /rebuild and /quit); most commands act in place.
func (m *model) command(line string) tea.Cmd {
	fields := strings.Fields(line)
	name := fields[0]
	arg := strings.TrimSpace(strings.TrimPrefix(line, name))
	switch name {
	case "/help":
		m.append(styleStatus.Render("/help  /save [path]  /resume <path>  /clear  /rebuild  /quit") + "\n")
	case "/clear":
		m.session = m.a.NewSession()
		m.content.Reset()
		m.append(styleStatus.Render("— cleared —") + "\n")
	case "/save":
		path := arg
		if path == "" {
			path = defaultSessionPath()
		}
		if err := transcript.Save(path, m.session.Messages()); err != nil {
			m.append(styleErr.Render("save failed: "+err.Error()) + "\n")
		} else {
			m.append(styleStatus.Render("saved → "+path) + "\n")
		}
	case "/resume":
		if arg == "" {
			m.append(styleErr.Render("usage: /resume <path>") + "\n")
			break
		}
		msgs, err := transcript.Import(arg)
		if err != nil {
			m.append(styleErr.Render("resume failed: "+err.Error()) + "\n")
			break
		}
		m.session = m.a.Resume(msgs)
		m.content.Reset()
		renderHistory(m, msgs)
		m.append(styleStatus.Render(fmt.Sprintf("— resumed %d messages —", len(msgs))) + "\n")
	case "/rebuild":
		path := filepath.Join(os.TempDir(), "eigen-rebuild.eigen.jsonl")
		if err := transcript.Save(path, m.session.Messages()); err != nil {
			m.append(styleErr.Render("rebuild: save failed: "+err.Error()) + "\n")
			break
		}
		m.rebuild = true
		m.rebuildPath = path
		return tea.Quit
	case "/quit", "/exit":
		return tea.Quit
	default:
		m.append(styleErr.Render("unknown command "+name+" (try /help)") + "\n")
	}
	return nil
}

func defaultSessionPath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".eigen", "sessions")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, time.Now().Format("20060102-150405")+".eigen.jsonl")
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
// non-empty it is submitted automatically as the first turn. If history is
// non-empty the session resumes that conversation.
func Run(a *agent.Agent, initialTask string, history []llm.Message) (Result, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	ti := textinput.New()
	ti.Placeholder = "type a task…  (enter to send · /help · ctrl+c to quit)"
	ti.Prompt = "› "
	ti.Focus()

	session := a.NewSession()
	if len(history) > 0 {
		session = a.Resume(history)
	}

	m := &model{
		a:           a,
		sp:          sp,
		ti:          ti,
		session:     session,
		ctx:         ctx,
		state:       stInput,
		initialTask: initialTask,
	}
	if len(history) > 0 {
		renderHistory(m, history)
		m.append(styleStatus.Render(fmt.Sprintf("— resumed %d messages —", len(history))) + "\n")
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

	final, err := p.Run()
	if err != nil {
		return Result{}, err
	}
	fm := final.(*model)
	return Result{Rebuild: fm.rebuild, SessionPath: fm.rebuildPath}, nil
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

// renderHistory pre-fills the transcript with a compact view of resumed
// messages so the user sees the conversation being continued.
func renderHistory(m *model, history []llm.Message) {
	for _, msg := range history {
		switch msg.Role {
		case llm.RoleUser:
			if msg.Text != "" {
				m.append(styleUser.Render("» "+compact(msg.Text)) + "\n")
			}
		case llm.RoleAssistant:
			if msg.Text != "" {
				m.append(compact(msg.Text) + "\n")
			}
			for _, tc := range msg.ToolCalls {
				m.append(styleTool.Render("▸ "+tc.Name) + "\n")
			}
		}
	}
}

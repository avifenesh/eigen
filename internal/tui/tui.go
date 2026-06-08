// Package tui renders an eigen session with Bubble Tea: a multi-turn REPL with
// a scrolling transcript of streamed model output, collapsible thinking and
// tool blocks, an input box, and inline gated approvals. It consumes the agent
// event sink.
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
	vp      viewport.Model
	sp      spinner.Model
	ti      textinput.Model
	a       *agent.Agent
	session *agent.Session
	ctx     context.Context

	blocks  []*block
	sel     int // index of the selected block (-1 = none / following tail)
	state   uiState
	pending *approvalMsg
	status  string

	initialTask string
	width       int
	height      int
	ready       bool

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

// --- block helpers ---------------------------------------------------------

func (m *model) push(b *block) *block {
	m.blocks = append(m.blocks, b)
	m.sel = -1 // new content: follow the tail
	m.sync()
	return b
}

func (m *model) note(s string) { m.push(&block{kind: blockNote, body: sb(s)}) }
func (m *model) text(role, s string) *block {
	return m.push(&block{kind: blockText, role: role, body: sb(s)})
}

func sb(s string) (b strings.Builder) { b.WriteString(s); return }

// lastOpen returns the most recent block of kind k that is still being streamed
// into (used to append deltas), or nil.
func (m *model) lastOpen(k blockKind) *block {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		if m.blocks[i].kind == k {
			return m.blocks[i]
		}
		if m.blocks[i].kind == blockText && m.blocks[i].role == "assistant" {
			continue
		}
		break
	}
	return nil
}

// sync rebuilds the viewport content from blocks, wrapping to width.
func (m *model) sync() {
	if !m.ready {
		return
	}
	var out strings.Builder
	for i, b := range m.blocks {
		out.WriteString(b.render(i == m.sel))
		out.WriteString("\n")
	}
	content := out.String()
	if w := m.vp.Width; w > 0 {
		content = lipgloss.NewStyle().Width(w).Render(content)
	}
	m.vp.SetContent(content)
	if m.sel < 0 {
		m.vp.GotoBottom()
	}
}

// collapsibleIdx returns block indices that can be selected/toggled.
func (m *model) collapsibleIdx() []int {
	var idx []int
	for i, b := range m.blocks {
		if b.collapsible() {
			idx = append(idx, i)
		}
	}
	return idx
}

func (m *model) moveSel(dir int) {
	idx := m.collapsibleIdx()
	if len(idx) == 0 {
		return
	}
	cur := -1
	for j, i := range idx {
		if i == m.sel {
			cur = j
			break
		}
	}
	switch {
	case cur == -1 && dir < 0:
		m.sel = idx[len(idx)-1] // entering from tail → last
	case cur == -1:
		m.sel = idx[0]
	default:
		n := cur + dir
		if n < 0 {
			n = 0
		}
		if n >= len(idx) {
			m.sel = -1 // past the end → back to following tail
			m.sync()
			return
		}
		m.sel = idx[n]
	}
	m.sync()
}

func (m *model) toggleSel() {
	if m.sel >= 0 && m.sel < len(m.blocks) && m.blocks[m.sel].collapsible() {
		m.blocks[m.sel].collapsed = !m.blocks[m.sel].collapsed
		m.sync()
	}
}

// --- update ----------------------------------------------------------------

func (m *model) submit(task string) tea.Cmd {
	m.text("user", task)
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
				m.note("approved")
				m.pending = nil
			case "n", "esc":
				m.pending.reply <- false
				m.note("denied")
				m.pending = nil
			}
			return m, nil
		}
		// Navigation/toggle works in any state (input box keeps focus for text).
		switch msg.String() {
		case "up", "ctrl+p":
			m.moveSel(-1)
			return m, nil
		case "down", "ctrl+n":
			m.moveSel(1)
			return m, nil
		case "tab":
			m.toggleSel()
			return m, nil
		case "pgup":
			m.vp.HalfViewUp()
			return m, nil
		case "pgdown":
			m.vp.HalfViewDown()
			return m, nil
		}
		if m.state == stInput {
			switch msg.String() {
			case "enter":
				task := strings.TrimSpace(m.ti.Value())
				if task == "" {
					return m, nil
				}
				m.ti.Reset()
				if strings.HasPrefix(task, "/") {
					return m, m.command(task)
				}
				return m, m.submit(task)
			case " ":
				if m.sel >= 0 { // space toggles when a block is selected
					m.toggleSel()
					return m, nil
				}
			}
			var cmd tea.Cmd
			m.ti, cmd = m.ti.Update(msg)
			return m, cmd
		}
		return m, nil

	case submitMsg:
		return m, m.submit(msg.task)

	case agentEvent:
		m.renderEvent(msg.e)
		return m, nil

	case approvalMsg:
		m.pending = &msg
		m.note(fmt.Sprintf("approve %s %s ? [y/N]", msg.name, compact(string(msg.args))))
		return m, nil

	case turnDoneMsg:
		if msg.err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("error: " + msg.err.Error())})
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
		if b := m.lastOpen(blockText); b != nil && b.role == "assistant" {
			b.body.WriteString(e.Text)
			m.sync()
		} else {
			m.text("assistant", e.Text)
		}
	case agent.EventReasoningDelta:
		if b := m.lastOpen(blockThinking); b != nil {
			b.body.WriteString(e.Text)
			m.sync()
		} else {
			m.push(&block{kind: blockThinking, title: "thinking", collapsed: true, body: sb(e.Text)})
		}
	case agent.EventToolStart:
		m.status = "running " + e.ToolName
		m.push(&block{
			kind:      blockTool,
			title:     e.ToolName + " " + compact(string(e.ToolArgs)),
			collapsed: true,
		})
	case agent.EventToolResult:
		// attach result to the matching open tool block (most recent)
		for i := len(m.blocks) - 1; i >= 0; i-- {
			if m.blocks[i].kind == blockTool && m.blocks[i].result == "" {
				m.blocks[i].result = e.Result
				m.blocks[i].isErr = e.IsError
				break
			}
		}
		m.sync()
	case agent.EventDone:
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
		bottom = m.sp.View() + " " + m.status + dim("   ↑↓ select · tab expand")
	default:
		bottom = m.ti.View()
	}
	return m.vp.View() + "\n" + bottom
}

func dim(s string) string { return styleReason.Render(s) }

// --- commands --------------------------------------------------------------

func (m *model) command(line string) tea.Cmd {
	fields := strings.Fields(line)
	name := fields[0]
	arg := strings.TrimSpace(strings.TrimPrefix(line, name))
	switch name {
	case "/help":
		m.note("/help  /save [path]  /resume <path>  /clear  /rebuild  /quit  ·  ↑↓ select, tab expand")
	case "/clear":
		m.session = m.a.NewSession()
		m.blocks = nil
		m.sel = -1
		m.note("— cleared —")
	case "/save":
		path := arg
		if path == "" {
			path = defaultSessionPath()
		}
		if err := transcript.Save(path, m.session.Messages()); err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("save failed: " + err.Error())})
		} else {
			m.note("saved → " + path)
		}
	case "/resume":
		if arg == "" {
			m.push(&block{kind: blockNote, isErr: true, body: sb("usage: /resume <path>")})
			break
		}
		msgs, err := transcript.Import(arg)
		if err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("resume failed: " + err.Error())})
			break
		}
		m.session = m.a.Resume(msgs)
		m.blocks = nil
		m.sel = -1
		renderHistory(m, msgs)
		m.note(fmt.Sprintf("— resumed %d messages —", len(msgs)))
	case "/rebuild":
		path := filepath.Join(os.TempDir(), "eigen-rebuild.eigen.jsonl")
		if err := transcript.Save(path, m.session.Messages()); err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("rebuild: save failed: " + err.Error())})
			break
		}
		m.rebuild = true
		m.rebuildPath = path
		return tea.Quit
	case "/quit", "/exit":
		return tea.Quit
	default:
		m.push(&block{kind: blockNote, isErr: true, body: sb("unknown command " + name + " (try /help)")})
	}
	return nil
}

func defaultSessionPath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".eigen", "sessions")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, time.Now().Format("20060102-150405")+".eigen.jsonl")
}

// Run drives the agent under a multi-turn Bubble Tea REPL.
func Run(a *agent.Agent, initialTask string, history []llm.Message) (Result, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	ti := textinput.New()
	ti.Placeholder = "type a task…  (enter to send · /help · ↑↓ select · tab expand · ctrl+c quit)"
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
		sel:         -1,
		state:       stInput,
		initialTask: initialTask,
	}
	if len(history) > 0 {
		renderHistory(m, history)
		m.note(fmt.Sprintf("— resumed %d messages —", len(history)))
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

// renderHistory pre-fills the transcript with resumed messages as blocks, so
// the user sees the conversation being continued — thinking and tool blocks
// start collapsed and can be expanded.
func renderHistory(m *model, history []llm.Message) {
	for _, msg := range history {
		switch msg.Role {
		case llm.RoleUser:
			if msg.Text != "" {
				m.text("user", msg.Text)
			}
		case llm.RoleAssistant:
			if msg.Reasoning != "" {
				m.push(&block{kind: blockThinking, title: "thinking", collapsed: true, body: sb(msg.Reasoning)})
			}
			if msg.Text != "" {
				m.text("assistant", msg.Text)
			}
			for _, tc := range msg.ToolCalls {
				m.push(&block{kind: blockTool, title: tc.Name + " " + compact(string(tc.Arguments)), collapsed: true})
			}
		case llm.RoleTool:
			for i := len(m.blocks) - 1; i >= 0; i-- {
				if m.blocks[i].kind == blockTool && m.blocks[i].result == "" {
					m.blocks[i].result = msg.Text
					m.blocks[i].isErr = msg.ToolError
					break
				}
			}
		}
	}
}

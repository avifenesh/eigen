// Package view is a thin terminal client for a daemon session: it attaches to
// the daemon, renders the session's streamed events as a scrolling transcript,
// and sends typed input over the socket. The agent runs in the daemon, not
// here — closing this window leaves the session running. This is intentionally
// independent of the agent-coupled chat model in package tui; it is a pure
// view.
package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/avifenesh/eigen/internal/daemon"
)

var (
	cTitle = lipgloss.NewStyle().Foreground(lipgloss.Color("44")).Bold(true)
	cUser  = lipgloss.NewStyle().Foreground(lipgloss.Color("44")).Bold(true)
	cTool  = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
	cDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	cErr   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	cFaint = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
)

// daemonEvent carries a streamed event into the Bubble Tea loop.
type daemonEvent struct {
	e      daemon.WireEvent
	replay bool
}

type connClosed struct{}

// Model is the view TUI.
type Model struct {
	client *daemon.Client
	id     string // session id
	title  string
	dir    string

	vp     viewport.Model
	ti     textarea.Model
	width  int
	height int
	ready  bool

	lines   []string // rendered transcript lines
	curAsst string   // accumulating assistant text for the current step
	status  string
	closed  bool

	// pending approval (daemon broadcast): answer with y/n from any view.
	approvalID   string
	approvalTool string
}

// New builds a view attached to session id (already attached on the client).
func New(client *daemon.Client, id, title, dir string) *Model {
	ti := textarea.New()
	ti.Placeholder = "message… (enter send · esc detach · ctrl+c quit)"
	ti.Prompt = "❯ "
	ti.ShowLineNumbers = false
	ti.SetHeight(1)
	ti.Focus()
	return &Model{client: client, id: id, title: title, dir: dir, ti: ti, status: "attached"}
}

func (m *Model) Init() tea.Cmd { return textarea.Blink }

// Listen returns a tea.Cmd that pumps daemon events into the program. The
// program ref is set via SetProgram before Run.
func (m *Model) flushAsst() {
	if strings.TrimSpace(m.curAsst) != "" {
		m.lines = append(m.lines, wrapLabeled("", m.curAsst))
	}
	m.curAsst = ""
}

func (m *Model) onEvent(e daemon.WireEvent) {
	switch e.Kind {
	case "reasoning":
		// fold reasoning into a dim prefix line (shown, not hidden)
		m.lines = append(m.lines, cDim.Render("  · "+firstLine(e.Text)))
	case "text":
		m.curAsst += e.Text
	case "tool_start":
		m.flushAsst()
		m.lines = append(m.lines, cTool.Render("  ▸ "+e.ToolName+" ")+cFaint.Render(firstLine(e.Text)))
		m.status = "running " + e.ToolName
	case "tool_result":
		if e.IsError {
			m.lines = append(m.lines, cErr.Render("    ✗ "+firstLine(e.Result)))
		}
	case "done":
		// The final answer arrives in done.Text. If the same text already
		// streamed as deltas this step (curAsst), flush that; otherwise render
		// done.Text directly. Either way the final answer always shows.
		if strings.TrimSpace(m.curAsst) != "" {
			m.flushAsst()
		} else if strings.TrimSpace(e.Text) != "" {
			m.lines = append(m.lines, e.Text)
		}
		m.status = "idle"
	case "approval":
		m.flushAsst()
		m.approvalID = e.Result
		m.approvalTool = e.ToolName
		m.lines = append(m.lines, cErr.Render("  ◆ approval needed: ")+cTool.Render(firstLine(e.Text)))
		m.status = "awaiting approval"
	case "note":
		m.flushAsst()
		m.lines = append(m.lines, cDim.Render("  "+e.Text))
		m.status = "idle" // a note is terminal for the turn (interrupt/error)
	}
	m.refresh()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.ready = true
		m.refresh()
		return m, nil
	case daemonEvent:
		m.onEvent(msg.e)
		return m, nil
	case connClosed:
		m.closed = true
		m.status = "daemon connection closed"
		m.refresh()
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			// While a turn runs, esc interrupts it (the daemon cancels the
			// turn); when idle, esc detaches (the session keeps running).
			if m.status == "thinking" || strings.HasPrefix(m.status, "running") {
				id, c := m.id, m.client
				m.status = "interrupting…"
				return m, func() tea.Msg { _ = c.Interrupt(id); return nil }
			}
			return m, tea.Quit
		case "y", "n":
			if m.approvalID != "" && strings.TrimSpace(m.ti.Value()) == "" {
				allow := msg.String() == "y"
				id, aid, c := m.id, m.approvalID, m.client
				m.approvalID, m.approvalTool = "", ""
				verdict := "denied"
				if allow {
					verdict = "approved"
				}
				m.lines = append(m.lines, cDim.Render("  ◆ "+verdict))
				m.status = "thinking"
				m.refresh()
				return m, func() tea.Msg { _ = c.Approve(id, aid, allow); return nil }
			}
			var cmd tea.Cmd
			m.ti, cmd = m.ti.Update(msg)
			return m, cmd
		case "enter":
			text := strings.TrimSpace(m.ti.Value())
			if text == "" || m.closed {
				return m, nil
			}
			m.lines = append(m.lines, cUser.Render("❯ "+text))
			m.ti.Reset()
			m.status = "thinking"
			m.refresh()
			id := m.id
			c := m.client
			return m, func() tea.Msg {
				_ = c.Input(id, text)
				return nil
			}
		}
		var cmd tea.Cmd
		m.ti, cmd = m.ti.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) layout() {
	h := m.height - 4
	if h < 3 {
		h = 3
	}
	if !m.ready {
		m.vp = viewport.New(m.width, h)
	} else {
		m.vp.Width, m.vp.Height = m.width, h
	}
	m.ti.SetWidth(m.width)
}

func (m *Model) refresh() {
	if !m.ready {
		return
	}
	m.vp.SetContent(strings.Join(m.lines, "\n"))
	m.vp.GotoBottom()
}

func (m *Model) View() string {
	if !m.ready {
		return "attaching…"
	}
	header := cTitle.Render(" "+m.titleOrID()) + cFaint.Render("  "+m.dir)
	statusLine := cFaint.Render(" "+m.status) + cFaint.Render("   (esc detach · session keeps running)")
	return header + "\n" + m.vp.View() + "\n" + statusLine + "\n" + m.ti.View()
}

func (m *Model) titleOrID() string {
	if m.title != "" {
		return m.title
	}
	return "session " + m.id
}

// Run attaches the view to the program's event pump and runs it.
func Run(client *daemon.Client, id, title, dir string) error {
	m := New(client, id, title, dir)
	p := tea.NewProgram(m, tea.WithAltScreen())
	// Pump daemon events into the program.
	if err := client.Attach(id, func(e daemon.WireEvent, replay bool) {
		p.Send(daemonEvent{e: e, replay: replay})
	}); err != nil {
		return err
	}
	go func() {
		<-client.Done()
		p.Send(connClosed{})
	}()
	_, err := p.Run()
	return err
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

func wrapLabeled(label, body string) string {
	if label == "" {
		return body
	}
	return fmt.Sprintf("%s %s", label, body)
}

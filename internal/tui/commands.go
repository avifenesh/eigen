package tui

// Slash-command dispatch and session load/save/export helpers.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/transcript"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *model) loadSessionByID(id string) {
	if m.store == nil {
		return
	}
	msgs, err := m.store.Load(id)
	if err != nil {
		m.push(&block{kind: blockNote, isErr: true, body: sb("resume failed: " + err.Error())})
		return
	}
	m.applyResumed(msgs)
}

// loadSession resumes from a store id or a transcript file path.
func (m *model) loadSession(arg string) {
	if m.store != nil && m.store.Get(arg) != nil {
		m.loadSessionByID(arg)
		return
	}
	msgs, err := transcript.Import(arg)
	if err != nil {
		m.push(&block{kind: blockNote, isErr: true, body: sb("resume failed: " + err.Error())})
		return
	}
	m.applyResumed(msgs)
}

func (m *model) applyResumed(msgs []llm.Message) {
	m.session = m.a.Resume(msgs)
	m.blocks = nil
	m.sel = -1
	renderHistory(m, msgs)
	m.refreshCtx()
	m.note(fmt.Sprintf("— resumed %d messages —", len(msgs)))
}

// safeWhileRunning reports whether a slash command can run while a turn is in
// flight. Settings and read-only commands are safe; commands that replace or
// mutate the session the agent goroutine is using (or exit) are not — they must
// wait until the turn finishes (press esc to interrupt).
func safeWhileRunning(name string) bool {
	switch name {
	case "/effort", "/search", "/perm", "/model", "/help",
		"/skills", "/tools", "/find", "/copy", "/read":
		return true
	default:
		// /clear, /compact, /resume, /rebuild, /save, /export, /quit, /exit
		return false
	}
}

func (m *model) command(line string) tea.Cmd {
	fields := strings.Fields(line)
	name := fields[0]
	arg := strings.TrimSpace(strings.TrimPrefix(line, name))
	switch name {
	case "/help":
		m.note("commands: /help  /resume  /save  /export  /clear  /compact  /model  /effort  /search  /perm  /skills  /tools  /find  /copy  /read  /rebuild  /quit")
		m.note("keys: / commands · @ files · ↑↓ history · select ctrl+p/n (or alt+↑/↓) · tab expand · drag select+copy · copy ctrl+y/alt+y · perm ctrl+a/alt+a · effort ctrl+e/alt+r · model ctrl+o/alt+m · pgup/pgdn scroll")
		m.note("multiplexer note: zellij/tmux capture ctrl+p/n/o — use the alt+… keys (alt+↑/↓ select, alt+m model, alt+r effort, alt+a perm, alt+y copy)")
		m.note("while running: enter queues a message · esc interrupts · settings commands (/effort /perm /model /search) run immediately")
	case "/clear":
		m.session = m.a.NewSession()
		m.blocks = nil
		m.sel = -1
		m.refreshCtx()
		m.note("— cleared —")
	case "/compact":
		if m.session == nil {
			break
		}
		m.state = stRunning
		m.status = "compacting…"
		m.relayout()
		return tea.Batch(m.sp.Tick, m.compactCmd())
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
			// open the picker
			if m.store == nil {
				m.push(&block{kind: blockNote, isErr: true, body: sb("no session store")})
				break
			}
			m.picks = m.store.List()
			if len(m.picks) == 0 {
				m.note("no sessions found")
				break
			}
			m.picking = true
			m.pickIdx = 0
			m.sync()
			break
		}
		m.loadSession(arg)
	case "/rebuild":
		if err := transcript.Save(m.sessionPath, m.session.Messages()); err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("rebuild: save failed: " + err.Error())})
			break
		}
		m.saveMeta()
		m.state = stRunning
		m.status = "rebuilding…"
		return tea.Batch(m.sp.Tick, m.buildCmd())
	case "/quit", "/exit":
		return tea.Quit
	case "/perm":
		switch agent.Permission(arg) {
		case agent.PermGated, agent.PermAuto:
			m.a.SetPerm(agent.Permission(arg))
			m.note("permission posture → " + arg)
		case "":
			m.note(fmt.Sprintf("permission posture: %s  (use /perm gated|auto to change)", m.a.Perm))
		default:
			m.push(&block{kind: blockNote, isErr: true, body: sb("unknown posture " + arg + " (want gated|auto)")})
		}
	case "/effort":
		es, ok := m.a.Provider.(llm.EffortSetter)
		if !ok {
			m.note("the current model does not support a reasoning-effort setting")
			break
		}
		if arg == "" {
			m.note(fmt.Sprintf("reasoning effort: %s   (/effort %s)", es.Effort(), strings.Join(llm.EffortLevels, "|")))
			break
		}
		if !es.SetEffort(arg) {
			m.push(&block{kind: blockNote, isErr: true, body: sb("unknown effort " + arg + " (want " + strings.Join(llm.EffortLevels, "|") + ")")})
			break
		}
		m.note("reasoning effort → " + es.Effort())
	case "/search":
		sr, ok := m.a.Provider.(llm.Searcher)
		if !ok {
			m.note("the current model does not support live search (grok only)")
			break
		}
		if arg == "" {
			m.note(fmt.Sprintf("live search: %s   (/search off|auto|on)", sr.SearchMode()))
			break
		}
		if !sr.SetSearch(arg) {
			m.push(&block{kind: blockNote, isErr: true, body: sb("unknown search mode " + arg + " (want off|auto|on)")})
			break
		}
		m.note("live search → " + sr.SearchMode())
	case "/model":
		if arg == "" {
			m.modelPicks = llm.Models()
			// Pre-select the currently active model.
			m.modelPickIdx = 0
			for i, mi := range m.modelPicks {
				if mi.ID == m.modelID {
					m.modelPickIdx = i
					break
				}
			}
			m.modelPicking = true
			m.sync()
			break
		}
		if m.newProvider == nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("model switching unavailable")})
			break
		}
		// Resolve provider + model. Forms:
		//   /model <provider> <id>   explicit provider
		//   /model <id>              provider inferred from the catalog, else
		//                            the current provider.
		prov, id := m.provName, arg
		if fs := strings.Fields(arg); len(fs) >= 2 {
			prov, id = fs[0], fs[1]
		}
		// Reconcile against the catalog so a known model never goes to the wrong
		// backend (e.g. /model us.anthropic.claude-opus-4-8 while on mantle).
		prov = llm.ResolveProvider(prov, id)
		np, perr := m.newProvider(prov, id)
		if perr != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("switch failed: " + perr.Error())})
			break
		}
		m.a.SetLive(np, llm.NewCompactor(np), m.contextBudgetFor(id))
		m.provName, m.modelID = prov, id
		// A manual switch takes precedence over any overload failover window.
		m.failoverFrom = nil
		m.failoverLeft = 0
		m.note("model → " + np.Name())
	case "/skills":
		if m.skills == nil || m.skills.Len() == 0 {
			m.note("no skills discovered (see --list-skills; add SKILL.md under ~/.eigen/skills or .eigen/skills)")
			break
		}
		// /skills <name> previews that skill's full body; bare /skills lists them.
		if arg != "" {
			body, err := m.skills.Body(arg)
			if err != nil {
				m.push(&block{kind: blockNote, isErr: true, body: sb(err.Error())})
				break
			}
			sk, _ := m.skills.Get(arg)
			m.push(&block{
				kind:      blockThinking, // reuse the collapsible block for a scrollable preview
				title:     "skill: " + arg,
				collapsed: false,
				body:      sb(strings.TrimSpace(sk.Description + "\n\n" + body)),
			})
			break
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("%d skill(s) — /skills <name> to preview, or let the model load one automatically:", m.skills.Len()))
		for _, sk := range m.skills.List() {
			b.WriteString("\n  • " + sk.Name)
			if d := firstLineOf(sk.Description); d != "" {
				b.WriteString(" — " + d)
			}
		}
		m.note(b.String())
	case "/tools":
		if m.a.Tools == nil {
			m.note("no tools")
			break
		}
		var b strings.Builder
		b.WriteString("tools:")
		for _, d := range m.a.Tools.Definitions() {
			posture := "·"
			if !d.ReadOnly {
				posture = "✎"
			}
			b.WriteString("\n  " + posture + " " + d.Name)
		}
		m.note(b.String())
	case "/read":
		if m.speaker == nil || !m.speaker.Available() {
			m.push(&block{kind: blockNote, isErr: true, body: sb("no TTS command found (set EIGEN_TTS_CMD or install espeak-ng)")})
			break
		}
		m.readAloud = !m.readAloud
		if m.readAloud {
			m.note("read-aloud on — assistant answers will be spoken")
		} else {
			m.speaker.Stop()
			m.note("read-aloud off")
		}
	case "/copy":
		if m.clip == nil || !m.clip.Available() {
			m.push(&block{kind: blockNote, isErr: true, body: sb("no clipboard command found (set EIGEN_CLIPBOARD_CMD or install wl-copy/xclip)")})
			break
		}
		text := m.copyTarget()
		if text == "" {
			m.note("nothing to copy")
			break
		}
		if err := m.clip.Copy(text); err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("copy failed: " + err.Error())})
		} else {
			m.note("copied to clipboard")
		}
	case "/export":
		path := arg
		if path == "" {
			path = defaultExportPath()
		}
		if err := os.WriteFile(path, []byte(sessionMarkdown(m.session.Messages())), 0o644); err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("export failed: " + err.Error())})
		} else {
			m.note("exported → " + path)
		}
	case "/find":
		if arg == "" {
			m.note("usage: /find <text>")
			break
		}
		matches := m.findBlocks(arg)
		if len(matches) == 0 {
			m.note("no matches for " + arg)
			break
		}
		// Note first: push resets the selection, so select after noting.
		m.note(fmt.Sprintf("%d match(es) for %q — showing the first", len(matches), arg))
		m.sel = matches[0]
		if m.blocks[m.sel].collapsible() {
			m.blocks[m.sel].collapsed = false
		}
		m.sync()
		m.scrollToSelected()
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

// defaultExportPath is where /export writes a markdown transcript by default.
func defaultExportPath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".eigen", "exports")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, time.Now().Format("20060102-150405")+".md")
}

// sessionMarkdown renders a conversation as a readable markdown transcript.
func sessionMarkdown(msgs []llm.Message) string {
	var b strings.Builder
	b.WriteString("# eigen session\n\n")
	for _, msg := range msgs {
		switch msg.Role {
		case llm.RoleUser:
			if t := strings.TrimSpace(msg.Text); t != "" {
				b.WriteString("## You\n\n" + t + "\n\n")
			}
		case llm.RoleAssistant:
			if t := strings.TrimSpace(msg.Text); t != "" {
				b.WriteString("## eigen\n\n" + t + "\n\n")
			}
			for _, tc := range msg.ToolCalls {
				b.WriteString("> tool `" + tc.Name + "` " + compact(string(tc.Arguments)) + "\n\n")
			}
		case llm.RoleTool:
			if t := strings.TrimSpace(msg.Text); t != "" {
				b.WriteString("```\n" + t + "\n```\n\n")
			}
		}
	}
	return b.String()
}

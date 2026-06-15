package tui

// Slash-command dispatch and session load/save/export helpers.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/hook"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/transcript"
	"github.com/avifenesh/eigen/internal/voice"
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
	m.backend.Reset(msgs)
	m.blocks = nil
	m.sel = -1
	renderHistory(m, msgs)
	m.refreshCtx()
	m.hooks.Fire(hook.Payload{Event: hook.OnSessionResume})
	m.note(fmt.Sprintf("— resumed %d messages —", len(msgs)))
}

// safeWhileRunning reports whether a slash command can run while a turn is in
// flight. Settings and read-only commands are safe; commands that replace or
// mutate the session the agent goroutine is using (or exit) are not — they must
// wait until the turn finishes (press esc to interrupt).
func safeWhileRunning(name string) bool {
	switch name {
	case "/effort", "/search", "/perm", "/model", "/help", "/goal", "/loop", "/config", "/route",
		"/skills", "/tools", "/find", "/copy", "/read", "/voice", "/mute", "/dictate", "/talk", "/speak", "/rail", "/changes", "/term", "/tasks", "/tray", "/workflow", "/rename", "/background", "/add-dir":
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
		m.note("commands: /help  /resume  /save  /export  /clear  /compact  /model  /effort  /search  /perm  /goal  /loop  /route  /review  /voice  /config  /skills  /tools  /add-dir  /find  /copy  /read  /rebuild  /quit")
		m.note("keys: / commands · ctrl+k palette · @ files · ↑↓ history · tab expand · drag-select+copy (ctrl+y) · alt+s switch session · alt+n tray (approvals/notifications across sessions) · alt+z background the running turn · ctrl+b/alt+b rail · ctrl+g/alt+g panel · ctrl+r right-tab (changes/git/term/tasks) · alt+a perm · alt+r effort · alt+m model · alt+v paste image · alt+t talk · pgup/pgdn scroll")
		m.note("terminal tab: /term (or ctrl+r to the term tab) opens a REAL shell in the right panel — click it or it's focused on open; your keystrokes (incl. esc/ctrl+c) go to the shell so vim/less/top work; ctrl+g returns keys to the chat, the shell keeps running")
		m.note("tasks tab: /tasks shows background delegations live (step/tool/elapsed) — click a task to expand its result or progress, click [cancel] to stop a running one; the sidebar shows ⚒ tasks N● while work runs")
		m.note("clickable: status-bar segments are buttons; header [home][sessions][+new][config]; side panel [x] closes rail/changes; click rail session to hop; click changes file to jump")
		m.note("multiplexer note: zellij/tmux capture ctrl+p/n/o, and zellij ALSO takes alt+arrows/alt+j/k (pane focus) — use shift+↑/↓ to select blocks there; alt+m model, alt+r effort, alt+a perm, alt+y copy still work")
		m.note("while running: enter STEERS (injects your message mid-turn, between tool rounds) · esc interrupts · alt+z (or click the status line, or /background) moves it to the background — the daemon keeps running it and wakes you when done · settings commands (/effort /perm /model /search) run immediately")
	case "/clear":
		m.backend.Reset(nil)
		m.blocks = nil
		m.sel = -1
		m.refreshCtx()
		m.note("— cleared —")
	case "/rename":
		if m.backend == nil {
			break
		}
		name := strings.TrimSpace(arg)
		if name == "" {
			// Bare /rename opens the interactive prompt (the single rename
			// surface, shared with the header title click).
			m.openRename()
			break
		}
		m.backend.SetTitle(name)
		m.saveMeta() // persist for local sessions (daemon persists its own)
		m.note("renamed → " + name)
	case "/compact":
		if m.backend == nil {
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
		if err := transcript.Save(path, m.backend.Messages()); err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("save failed: " + err.Error())})
		} else {
			m.note("saved → " + path)
		}
	case "/home":
		m.openApp = true
		return tea.Quit
	case "/background", "/bg":
		// Move the turn you're WAITING ON to the background: the daemon keeps
		// running it; this window returns to the dashboard. On completion the
		// daemon notifies (if notify_cmd is set). Reattach to collect.
		if !m.canBackgroundTurn() {
			if !m.isDaemonBacked() {
				m.note("/background needs a daemon session (this is a local chat)")
			} else {
				m.note("nothing running to background")
			}
			return nil
		}
		m.note("moved to background — the daemon keeps running it; reattach from the dashboard to collect")
		return m.backgroundTurn()
	case "/sessions":
		m.openSwitcher()
	case "/rail":
		return m.toggleRail()
	case "/changes":
		return m.toggleChanges()
	case "/term":
		return m.setRightTab(rightTabTerminal)
	case "/tasks":
		return m.setRightTab(rightTabTasks)
	case "/tray":
		m.openTray()
		return nil
	case "/workflow":
		return m.runWorkflowCmd(arg)
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
		// Guard the production-daemon disaster: on the DEFAULT instance with
		// other live sessions, /rebuild restarts the whole daemon and
		// interrupts them all. Require an explicit confirm (a second /rebuild,
		// or /rebuild!) and point at `eigen dev` — a separate instance whose
		// rebuilds never touch production. EIGEN_INSTANCE!="" means we're
		// already on an isolated instance, so no guard.
		if arg != "!" && !m.rebuildArmed && os.Getenv("EIGEN_INSTANCE") == "" {
			if n := m.siblingSessionCount(); n > 0 {
				m.rebuildArmed = true
				m.note(fmt.Sprintf("⚠ /rebuild restarts the PRODUCTION daemon — it interrupts all %d running session(s), not just this one. For iterating on eigen without breaking your work, run a separate instance: `eigen dev` (its own sessions; /rebuild there is isolated). To rebuild production anyway, run /rebuild again (or /rebuild!).", n+1))
				break
			}
		}
		m.rebuildArmed = false
		// Local sessions save a transcript for the exec-resume; daemon-hosted
		// sessions (sessionPath == "") need nothing — the daemon persists, and
		// the caller restarts it and reattaches to the same session id.
		if m.sessionPath != "" {
			if err := transcript.Save(m.sessionPath, m.backend.Messages()); err != nil {
				m.push(&block{kind: blockNote, isErr: true, body: sb("rebuild: save failed: " + err.Error())})
				break
			}
			m.saveMeta()
		}
		m.state = stRunning
		m.status = "rebuilding…"
		return tea.Batch(m.sp.Tick, m.buildCmd())
	case "/quit", "/exit":
		return tea.Quit
	case "/perm":
		switch agent.Permission(arg) {
		case agent.PermGated, agent.PermAuto:
			m.backend.SetPerm(agent.Permission(arg))
			m.note("permission posture → " + arg)
		case "":
			m.note(fmt.Sprintf("permission posture: %s  (use /perm gated|auto to change)", m.backend.Perm()))
		default:
			m.push(&block{kind: blockNote, isErr: true, body: sb("unknown posture " + arg + " (want gated|auto)")})
		}
	case "/review":
		if m.state != stInput {
			m.note("/review runs after the current turn finishes")
			break
		}
		target := arg
		if target == "" {
			target = "the work you just did in this session"
		}
		return m.submit("Use the review tool to get a cross-vendor critique of " + target + ". Package the relevant artifact (the plan, diff, or code) into the tool's `artifact` argument with enough context to judge it, set an appropriate `focus`, then act on the critique — fix real issues it raises and note anything you disagree with and why.")
	case "/voice":
		if strings.EqualFold(arg, "setup") || strings.EqualFold(arg, "doctor") {
			for _, line := range strings.Split(voice.Report(), "\n") {
				m.note(line)
			}
			return nil
		}
		return m.toggleVoice()
	case "/mute":
		return m.toggleMute()
	case "/dictate", "/talk":
		return m.dictateOnce()
	case "/speak":
		m.speakLastAnswer()
	case "/goal":
		switch arg {
		case "":
			if g := m.backend.Goal(); g != "" {
				m.note("goal: " + g + "   (/goal clear to unset)")
			} else {
				m.note("no goal set  (/goal <text> to set a persistent north star)")
			}
		case "clear", "none", "off":
			m.backend.SetGoal("")
			m.saveMeta()
			m.idleGen++ // invalidate any pending goal nag
			m.note("goal cleared")
		default:
			m.backend.SetGoal(arg)
			m.saveMeta()
			m.note("goal → " + arg)
			// Setting a goal IS the work order: when idle, start working
			// toward it right now (the goal rides in the system prompt). When
			// a turn is running, the goal takes effect from its next step.
			if m.state == stInput {
				return m.submit("A goal was just set (see CURRENT GOAL in your instructions). Start working toward it now: assess the current state, plan briefly, then take the first concrete actions. When it is fully achieved, call goal_achieved with evidence.")
			}
			// Arm the idle nag for when the running turn ends.
			m.idleGen++
			return m.scheduleGoalNag()
		}
	case "/add-dir":
		switch arg {
		case "":
			roots := m.backend.Roots()
			if len(roots) == 0 {
				m.note("no sandbox roots reported")
				break
			}
			lines := "working directories (tools may read/write/run here):\n  " + roots[0] + "  (primary)"
			for _, r := range roots[1:] {
				lines += "\n  " + r
			}
			lines += "\n(/add-dir <path> to grant another)"
			m.note(lines)
		default:
			root, err := m.backend.AddDir(expandHome(arg))
			if err != nil {
				m.note("add-dir: " + err.Error())
				break
			}
			m.saveMeta()
			m.note("added working directory → " + root + "   (tools can now read/write/run here)")
		}
	case "/route":
		if m.router == nil {
			m.note("auto-router unavailable")
			break
		}
		switch arg {
		case "":
			status := "off"
			if m.router.Enabled() {
				status = "on"
			}
			scope := "current provider only"
			if p := m.router.Providers(); len(p) > 0 {
				scope = "across " + strings.Join(p, " ")
			}
			m.note(fmt.Sprintf("routing: %s (%s) — your model orchestrates: task-tool delegations with stated kind/difficulty always route; heuristic routing of unstated subtasks is %s (/route on|off)", status, scope, status))
		case "on":
			m.router.SetEnabled(true)
			m.note("heuristic routing ON — unstated subtasks also route by prompt classification (orchestrator-stated kind/difficulty always routes)")
		case "off":
			m.router.SetEnabled(false)
			m.note("heuristic routing OFF — routing still happens when YOU state kind/difficulty on a task delegation (and for vision needs)")
		default:
			m.push(&block{kind: blockNote, isErr: true, body: sb("usage: /route on|off  (cross-provider scope: /config route_providers <list>)")})
		}
	case "/config":
		fields := strings.Fields(arg)
		switch {
		case arg == "":
			// Live editable panel — same interaction language as the app
			// shell's config page (cursor, space-cycle, dropdowns).
			m.openConfigPanel()
		case len(fields) == 1:
			// /config <key> — describe the field: what it means + valid values.
			fld := config.FieldFor(fields[0])
			if fld.Key == "" {
				m.push(&block{kind: blockNote, isErr: true, body: sb("config: unknown key " + fields[0] + " (keys: " + strings.Join(config.Keys(), " ") + ")")})
				break
			}
			body := fld.Key + " = " + valueOrUnset(config.Get(config.Load(), fld.Key)) + "\n" + fld.Desc
			if opts := configOptionsHint(fld); opts != "" {
				body += "\nvalues: " + opts
			}
			m.note(body)
		case len(fields) >= 2:
			key := fields[0]
			value := strings.TrimSpace(strings.TrimPrefix(arg, key))
			c := config.Load()
			if err := config.Set(&c, key, value); err != nil {
				m.push(&block{kind: blockNote, isErr: true, body: sb("config: " + err.Error())})
				break
			}
			if err := config.Save(c); err != nil {
				m.push(&block{kind: blockNote, isErr: true, body: sb("config: save: " + err.Error())})
				break
			}
			m.note("config: " + key + " = " + value + "   (applies to new sessions)")
		default:
			m.push(&block{kind: blockNote, isErr: true, body: sb("usage: /config            (show)\n       /config <key>          (describe)\n       /config <key> <value>  (set)")})
		}
	case "/loop":
		switch arg {
		case "":
			if m.loopPrompt != "" {
				m.note(fmt.Sprintf("loop: every %s → %s   (fired %d time(s); /loop clear to stop)", m.loopEvery, m.loopPrompt, m.loopRuns))
			} else {
				m.note("no loop set  (/loop [interval] <prompt> — e.g. /loop 10m read GOALS.md and do the next item)")
			}
		case "clear", "none", "off", "stop":
			m.loopPrompt, m.loopEvery, m.loopRuns = "", 0, 0
			m.idleGen++ // invalidate pending fires
			m.note("loop cleared")
		default:
			every, prompt, err := parseLoopArgs(arg)
			if err != nil {
				m.push(&block{kind: blockNote, isErr: true, body: sb("loop: " + err.Error() + "  (usage: /loop [interval] <prompt>)")})
				break
			}
			m.loopPrompt, m.loopEvery, m.loopRuns = prompt, every, 0
			m.idleGen++ // restart the schedule cleanly
			m.note(fmt.Sprintf("loop set: every %s → %s   (first fire in %s; /loop clear to stop)", every, prompt, every))
			return m.scheduleLoop()
		}
	case "/effort":
		if m.backend.Effort() == "" {
			m.note("the current model does not support a reasoning-effort setting")
			break
		}
		if arg == "" {
			m.note(fmt.Sprintf("reasoning effort: %s   (/effort %s)", m.backend.Effort(), strings.Join(m.effortLevels(), "|")))
			break
		}
		if !m.backend.SetEffort(arg) {
			m.push(&block{kind: blockNote, isErr: true, body: sb("unknown effort " + arg + " for " + m.modelID + " (want " + strings.Join(m.effortLevels(), "|") + ")")})
			break
		}
		m.note("reasoning effort → " + m.backend.Effort())
	case "/search":
		if m.backend.SearchMode() == "" {
			m.note("the current model does not support live search (grok only)")
			break
		}
		if arg == "" {
			m.note(fmt.Sprintf("live search: %s   (/search off|auto|on)", m.backend.SearchMode()))
			break
		}
		if !m.backend.SetSearch(arg) {
			m.push(&block{kind: blockNote, isErr: true, body: sb("unknown search mode " + arg + " (want off|auto|on)")})
			break
		}
		m.note("live search → " + m.backend.SearchMode())
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
		//   /model <provider>:<id>   ref form — explicit tag forces the backend
		//   /model <provider> <id>   explicit provider
		//   /model <id>              provider inferred from the catalog, else
		//                            the current provider.
		prov, id := m.provName, arg
		if fs := strings.Fields(arg); len(fs) >= 2 {
			prov, id = fs[0], fs[1]
		}
		if tag, bare := llm.ParseRef(id); tag != "" {
			// An explicit tag wins outright — no catalog second-guessing.
			prov, id = tag, bare
		} else {
			// Reconcile against the catalog so a known model never goes to the
			// wrong backend (e.g. /model us.anthropic.claude-opus-4-8 on mantle).
			prov = llm.ResolveProvider(prov, id)
		}
		np, perr := m.newProvider(prov, id)
		if perr != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("switch failed: " + perr.Error())})
			break
		}
		m.backend.SetModel(np, m.compactorFor(np), m.contextBudgetFor(id))
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
		tools := m.backend.Tools()
		if len(tools) == 0 {
			m.note("no tools")
			break
		}
		var b strings.Builder
		b.WriteString("tools:")
		for _, d := range tools {
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
			m.dropSpeech()
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
			return m.showFlash(fmt.Sprintf("copied %d chars", len([]rune(text))))
		}
	case "/export":
		path := arg
		if path == "" {
			path = defaultExportPath()
		}
		if err := os.WriteFile(path, []byte(sessionMarkdown(m.backend.Messages())), 0o644); err != nil {
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

// valueOrUnset renders a config value or "(unset)" for the empty string.
func valueOrUnset(v string) string {
	if v == "" {
		return "(unset)"
	}
	return v
}

// configOptionsHint describes a config field's accepted values for /config
// <key>: the static enum, or the dynamic catalog source. "" = free text.
func configOptionsHint(f config.Field) string {
	if len(f.Options) > 0 {
		return strings.Join(f.Options, " | ")
	}
	switch f.Dynamic {
	case "providers":
		var names []string
		for _, p := range llm.Catalog {
			seen := false
			for _, n := range names {
				if n == p.Provider {
					seen = true
					break
				}
			}
			if !seen {
				names = append(names, p.Provider)
			}
		}
		hint := "any provider — " + strings.Join(names, " ")
		if f.Multi {
			hint = "space-separated; " + hint
		}
		return hint
	case "models":
		return "any catalog model id (see the models page or `eigen models`)"
	}
	return ""
}

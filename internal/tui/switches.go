package tui

// Live config switches: perm toggle, effort/model cycling, the overload
// failover window, and the context-budget rule they all share.

import (
	"fmt"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/chat"
	"github.com/avifenesh/eigen/internal/llm"
	tea "github.com/charmbracelet/bubbletea"
)

// togglePerm flips the permission posture between gated and auto — the keyboard
// shortcut (ctrl+a) for fast mode changes, equivalent to /perm gated|auto. It
// persists the new posture to the session meta so it survives rebuild/resume.
func (m *model) togglePerm() tea.Cmd {
	if m.backend == nil {
		return nil
	}
	if m.backend.Perm() == agent.PermAuto {
		m.backend.SetPerm(agent.PermGated)
	} else {
		m.backend.SetPerm(agent.PermAuto)
	}
	m.saveMeta()
	return m.showFlash("perm · " + string(m.backend.Perm()))
}

// cycleEffort steps the reasoning effort to the next level (wrapping) — the
// keyboard shortcut (ctrl+e) for fast effort changes, equivalent to /effort. It
// is a no-op (with a note) when the current model has no effort setting.
func (m *model) cycleEffort() tea.Cmd {
	if m.backend == nil {
		return nil
	}
	cur := m.backend.Effort()
	if cur == "" {
		m.note("the current model does not support a reasoning-effort setting")
		return nil
	}
	// Cycle within the CURRENT MODEL's level set (per-catalog: opus is
	// adaptive auto|low|medium|high, mantle GPT low..xhigh, sonnet off..high).
	levels := m.effortLevels()
	next := cur
	for i, l := range levels {
		if l == cur {
			next = levels[(i+1)%len(levels)]
			break
		}
	}
	if next == cur || !m.backend.SetEffort(next) {
		// Current level not found in the list, or set failed: start at the first.
		if len(levels) > 0 {
			_ = m.backend.SetEffort(levels[0])
		}
	}
	m.saveMeta()
	return m.showFlash("effort · " + m.backend.Effort())
}

// switchModelTo performs a live model switch to the given provider/model id,
// reusing the exact /model resolution + SetModel path. It returns an error
// string (empty on success) so callers can surface failures their own way. The
// provider name/id can be a ref (provider:id), an explicit provider, or a bare
// id reconciled against the catalog — mirroring the /model command forms.
func (m *model) switchModelTo(provName, id string) string {
	if m.newProvider == nil {
		return "model switching unavailable"
	}
	prov := provName
	if prov == "" {
		prov = m.provName
	}
	if tag, bare := llm.ParseRef(id); tag != "" {
		prov, id = tag, bare
	} else {
		prov = llm.ResolveProvider(prov, id)
	}
	np, err := m.newProvider(prov, id)
	if err != nil {
		return "switch failed: " + err.Error()
	}
	m.backend.SetModel(np, m.compactorFor(np), m.contextBudgetFor(id))
	m.provName, m.modelID = prov, id
	// A manual switch takes precedence over any overload failover window.
	m.failoverFrom = nil
	m.failoverLeft = 0
	m.saveMeta()
	return ""
}

// cycleModel switches to the next model in the catalog (wrapping) — the
// keyboard shortcut (ctrl+o) for fast model changes, equivalent to /model. The
// provider is reconciled from the catalog so it never desyncs.
func (m *model) cycleModel() tea.Cmd {
	if m.newProvider == nil {
		m.push(&block{kind: blockNote, isErr: true, body: sb("model switching unavailable")})
		return nil
	}
	models := llm.Models()
	if len(models) == 0 {
		return nil
	}
	// Find the current model, then advance to the next entry (wrapping).
	idx := -1
	for i, mi := range models {
		if mi.ID == m.modelID {
			idx = i
			break
		}
	}
	next := models[(idx+1)%len(models)]
	prov := llm.ResolveProvider(next.Provider, next.ID)
	np, err := m.newProvider(prov, next.ID)
	if err != nil {
		m.push(&block{kind: blockNote, isErr: true, body: sb("switch failed: " + err.Error())})
		return nil
	}
	m.backend.SetModel(np, m.compactorFor(np), m.contextBudgetFor(next.ID))
	m.provName, m.modelID = prov, next.ID
	// A manual switch takes precedence over any overload failover window.
	m.failoverFrom = nil
	m.failoverLeft = 0
	m.saveMeta()
	return m.showFlash("model · " + np.Name())
}

// startFailover switches the live provider to fallback for failoverTurns
// turns, remembering the origin. Returns false when failover is not applicable
// (no target, already failed over, no constructor, or switch failed).
func (m *model) startFailover(fallback string) bool {
	if m.newProvider == nil || fallback == "" || m.modelID == fallback || m.failoverFrom != nil {
		return false
	}
	prov := llm.ResolveProvider(m.provName, fallback)
	np, err := m.newProvider(prov, fallback)
	if err != nil {
		return false
	}
	m.failoverFrom = &failoverOrigin{provider: m.provName, model: m.modelID}
	m.failoverLeft = failoverTurns
	m.backend.SetModel(np, m.compactorFor(np), m.contextBudgetFor(fallback))
	m.provName, m.modelID = prov, fallback
	return true
}

// endFailover switches back to the original model after the failover window.
// Best-effort: if the original cannot be constructed, stay on the fallback.
func (m *model) endFailover() {
	if m.failoverFrom == nil || m.newProvider == nil {
		return
	}
	orig := *m.failoverFrom
	np, err := m.newProvider(orig.provider, orig.model)
	if err != nil {
		m.note("failover: could not switch back to " + orig.model + " (" + err.Error() + ") — staying on " + m.modelID)
		m.failoverFrom = nil
		m.failoverLeft = 0
		return
	}
	m.backend.SetModel(np, m.compactorFor(np), m.contextBudgetFor(orig.model))
	m.provName, m.modelID = orig.provider, orig.model
	m.failoverFrom = nil
	m.failoverLeft = 0
	m.note("overload window over — switched back to " + orig.model)
}

// cycleSearch steps live search off→auto→on→off (wrapping). No-op (with a
// note) when the current model has no live-search setting.
func (m *model) cycleSearch() tea.Cmd {
	if m.backend == nil {
		return nil
	}
	cur := m.backend.SearchMode()
	if cur == "" {
		m.note("the current model does not support live search (grok only)")
		return nil
	}
	order := []string{"off", "auto", "on"}
	next := order[0]
	for i, s := range order {
		if s == cur {
			next = order[(i+1)%len(order)]
			break
		}
	}
	if !m.backend.SetSearch(next) {
		_ = m.backend.SetSearch(order[0])
	}
	m.saveMeta()
	return m.showFlash("search · " + m.backend.SearchMode())
}

// toggleFast flips the Codex fast/priority service tier on the active provider.
func (m *model) toggleFast() tea.Cmd {
	if m.backend == nil {
		return nil
	}
	// Refresh state before deciding: a stale cached snapshot (from before a
	// /model switch) would report fast_ok=false for a model that now supports
	// it (or vice-versa). The daemon is authoritative.
	if rb, ok := m.backend.(*chat.Remote); ok {
		rb.Refresh()
	}
	if !m.backend.FastSupported() {
		m.note("the current model has no fast mode (Codex gpt-5.x only)")
		return nil
	}
	if !m.backend.SetFast(!m.backend.FastMode()) {
		m.note("fast mode unavailable")
		return nil
	}
	m.saveMeta()
	state := "off"
	if m.backend.FastMode() {
		state = "on"
	}
	return m.showFlash("fast · " + state)
}

// contextBudgetFor returns the budget for a model id, capped by
// min(user ceiling, model window minus headroom) via llm.ContextBudget — the
// same rule as main's startup budget, so live /model switches stay consistent.
func (m *model) contextBudgetFor(model string) int {
	return llm.ContextBudget(m.maxTokens, model, 0)
}

// compactorFor builds the compactor for a freshly switched provider, chaining
// the cheap small-model summarizer (when configured) before the main one.
func (m *model) compactorFor(np llm.Provider) llm.Compactor {
	return llm.CompactorChain(m.smallCompactor, llm.NewCompactor(np))
}

// effortLevels returns the effort set valid for the CURRENT model (per-model
// catalog set when known, the global list otherwise).
func (m *model) effortLevels() []string {
	if levels := llm.ModelEffortLevels(m.modelID); len(levels) > 0 {
		return levels
	}
	return llm.EffortLevels
}

// normalizeInputMode coerces a config value to a valid input mode (default steer).
func normalizeInputMode(s string) string {
	if s == "queue" {
		return "queue"
	}
	return "steer"
}

// steering reports whether Enter steers (vs queues) while a turn runs.
func (m *model) steering() bool { return normalizeInputMode(m.inputMode) != "queue" }

// toggleInputMode flips steer↔queue (the input= status segment, alt+q, /steer,
// /queue). A flash shows the new mode; it rides in saveMeta for daemon sessions.
func (m *model) toggleInputMode() tea.Cmd {
	if m.steering() {
		m.inputMode = "queue"
	} else {
		m.inputMode = "steer"
	}
	m.saveMeta()
	if m.steering() {
		return m.showFlash("input · steer (Enter injects mid-turn)")
	}
	return m.showFlash("input · queue (Enter waits for the next turn)")
}

// steerOrQueue handles a message typed while a turn is running, honoring the
// input mode: steer injects it mid-turn (between tool rounds); queue holds it
// for the next turn. Shared by the stRunning and attachedRunning Enter paths.
func (m *model) steerOrQueue(task string) {
	if m.steering() && m.backend != nil && m.backend.Steer(task, nil) {
		m.note("↪ steering: " + compact(task))
		return
	}
	// A remote backend's input op is atomic: Steer either injects into a
	// running turn (returns true, handled above) or — if the daemon went idle
	// between the TUI seeing "running" and the op landing — STARTS A NEW TURN
	// with this message (returns false). Either way the message was delivered,
	// so queueing it again would send it twice. Only the local backend's false
	// truly means "idle, not sent", and there only when we tried to steer.
	if m.steering() {
		if _, remote := m.backend.(*chat.Remote); remote {
			m.note("↪ started a new turn: " + compact(task))
			return
		}
	}
	m.queued = append(m.queued, task)
	m.note(fmt.Sprintf("queued (%d): %s", len(m.queued), compact(task)))
}

// detachRunningBash backgrounds the bash command running in the current turn
// (the alt+d / ctrl+b key) — the agent stops waiting on it and keeps working in
// the same turn, while the command runs on as a background shell. Flashes
// feedback; a no-op note when the current step isn't a foreground bash.
func (m *model) detachRunningBash() {
	if m.backend == nil {
		return
	}
	if m.backend.DetachBash() {
		m.showFlash("backgrounded the running command → shells panel")
	} else {
		m.showFlashTone("nothing to background (no bash running this step)", flashWarn)
	}
}

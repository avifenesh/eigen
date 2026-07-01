package gui

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/command"
	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/feed"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/workflow"
	"golang.org/x/sync/singleflight"
)

// Bridge is the host-agnostic service exposed to the frontend. Under the
// `wails` tag every method becomes a generated TS binding (wails.go); headless
// (guiserver) the same methods are reached over the socket dispatcher. It holds
// ONE long-lived control client for request/response RPCs, and a map of
// streaming pumps (one dedicated daemon connection per subscribed session —
// see pump.go).
//
// All daemon IO (ensure/retry/sleep) is done OUTSIDE the mutex so a down daemon
// can never stall unrelated RPCs, and every teardown path is guarded by
// sync.Once so concurrent Shutdown/Unsubscribe/watchdog races can't double-close.
type Bridge struct {
	// emitter delivers events to whatever frontend host is attached (Wails app
	// or guiserver socket fan-out — see Emitter in emitter.go). nil = no host
	// yet; emit() then drops events, matching the old nil-app behavior.
	emitter Emitter
	ensure  func() (*daemon.Client, error)

	// Proactive-feed inputs (the home base's "act on" surface). suggest is the
	// LLM suggester (nil = suggestions off; git/github/memory signals still
	// flow); dirs supplies the project universe to scan. Both injected by main
	// so the bridge owns no model/provider construction.
	suggest feed.Suggester
	dirs    func() []string

	mu       sync.Mutex
	ctrl     *daemon.Client
	pumps    map[string]*sessionPump
	closing  bool
	pollStop chan struct{}
	feedStop chan struct{}

	// Remote control clients, one pooled per ssh target, dialed lazily when a
	// remote session ref is first acted on and reused while alive (remote_ref.go).
	// remoteDial singleflights the per-target ssh dial so concurrent RPCs for a
	// cold target share one spawn. Both guarded by mu (IO outside the lock).
	remoteCtrls map[string]*daemon.Client
	remoteDial  singleflight.Group
	lastFeed    feed.Feed // most-recent scan, so DismissFeed can rebuild an Item from its key

	// GPU history ring + alert state for the Machine panel sparkline + training
	// hot-GPU notifications (gpuhist.go). nil until the GPU sample loop starts.
	gpuHist *gpuHist

	// Voice controller (lazily built): STT/TTS backends + the one-shot and
	// conversation-mode lifecycle. See voice.go.
	voiceOnce sync.Once
	voiceCtl  *voiceCtl

	// wailsHost carries the Wails-only state (the *application.App handle for
	// native dialogs). Under `wails` it holds the app (wails.go); tagless it is
	// an empty struct (wails_stub.go) — the pattern that keeps this file, and
	// therefore the whole package, free of any Wails import.
	wailsHost
}

// NewBridge constructs the bridge. ensure connects to (and lazily spawns) the
// daemon; suggest + dirs power the proactive feed (both may be nil/empty — the
// feed then yields only signal-derived items or nothing).
func NewBridge(ensure func() (*daemon.Client, error), suggest feed.Suggester, dirs func() []string) *Bridge {
	return &Bridge{ensure: ensure, suggest: suggest, dirs: dirs, pumps: map[string]*sessionPump{}, remoteCtrls: map[string]*daemon.Client{}}
}

// SetEmitter wires the event sink for emission. Called from the bootstrap
// before the host runs (Wails: the wailsEmitter adapter in wails.go; guiserver:
// its socket fan-out).
func (b *Bridge) SetEmitter(e Emitter) { b.emitter = e }

// Start launches the bridge's background loops (health/feed/GPU) and kicks the
// initial daemon probe. Host-agnostic: the Wails ServiceStartup hook (wails.go)
// and guiserver both call this — the lifecycle must not live behind the wails
// tag or the tagless build would silently run no loops.
func (b *Bridge) Start() {
	b.mu.Lock()
	b.pollStop = make(chan struct{})
	b.feedStop = make(chan struct{})
	stop := b.pollStop
	feedStop := b.feedStop
	b.mu.Unlock()
	// The initial daemon connect MUST NOT run inline here: on a cold start
	// control()→ensure() spawns `eigen daemon` and blocks up to ~10s for its
	// socket. Wails calls ServiceStartup synchronously on the main goroutine
	// BEFORE the window is shown, so a blocking probe leaves the desktop with no
	// window for that whole spawn. Probe in a goroutine; the frontend daemon
	// store shows connecting→online/offline meanwhile, and healthLoop also
	// probes within its first 1s tick.
	go func() {
		if _, err := b.control(); err != nil {
			b.emit(eventDaemonHealth, HealthDTO{OK: false, Error: err.Error()})
		}
	}()
	go b.healthLoop(stop)
	go b.feedLoop(feedStop)
	go b.gpuSampleLoop(stop) // GPU history + training hot-GPU alerts (no-op without a GPU)
}

// Stop tears down every pump + the control client so no goroutine, connection,
// or daemon-side view leaks. Alias for Shutdown kept as Start's symmetric peer
// so guiserver reads Start()/Stop().
func (b *Bridge) Stop() { b.Shutdown() }

const (
	eventDaemonStats  = "eigen:daemon:stats"
	eventDaemonHealth = "eigen:daemon:health"
)

// DaemonStats parity contract (GUI-096): the daemon resource-health snapshot is
// the ONE shape emitted RAW — both on the eventDaemonStats stream (healthLoop)
// and from Stats() — as the *daemon.DaemonStats value with its native snake_case
// JSON tags (uptime_sec, heap_alloc_b, binary_sha256, vcs_revision, …). It does
// NOT pass through the dto.go camelCase DTO layer that every other type uses, so
// the frontend's types.ts `DaemonStats` block hand-mirrors those snake_case tags
// 1:1. Because there is no DTO+mapper enforcing the mapping, a daemon-side field
// rename silently desyncs the two: KEEP the daemon tags and the types.ts keys in
// lockstep (esp. the identity fields version/executable/binary_sha256/
// vcs_revision/vcs_modified). A full DTO+mapper is a deliberate non-goal here —
// the documented-parity rule avoids churn while making the requirement explicit.

// healthLoop pushes a DaemonStats snapshot to the frontend at ~1Hz while
// online, backing off to 5s while the daemon is unreachable so a down daemon
// never becomes a busy reconnect loop.
func (b *Bridge) healthLoop(stop chan struct{}) {
	const fast, slow = time.Second, 5 * time.Second
	t := time.NewTicker(fast)
	defer t.Stop()
	fails := 0
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			c, err := b.control()
			if err == nil {
				if st, e := c.Stats(); e == nil {
					if fails != 0 {
						fails = 0
						t.Reset(fast)
					}
					// Emitted RAW (snake_case daemon tags, no DTO) — see the
					// DaemonStats parity contract above; types.ts must mirror.
					b.emit(eventDaemonStats, st)
					continue
				} else {
					err = e
				}
			}
			b.emit(eventDaemonHealth, HealthDTO{OK: false, Error: err.Error()})
			if fails == 0 {
				t.Reset(slow)
			}
			fails++
		}
	}
}

// emit delivers one event to the attached host, dropping it when no host is
// wired yet (startup, tests). Every emission in the package MUST route through
// here (or b.emitter directly) — never through a Wails type — so the package
// stays tagless-compilable.
func (b *Bridge) emit(name string, data any) {
	if b.emitter != nil {
		b.emitter.Emit(name, data)
	}
}

// control returns the long-lived control client, (re)connecting on demand.
// IO (ensure/retry/sleep) runs OUTSIDE the lock; the stale client is Closed
// before replacement so its readLoop/eventLoop goroutines terminate.
func (b *Bridge) control() (*daemon.Client, error) {
	b.mu.Lock()
	if b.closing {
		b.mu.Unlock()
		return nil, fmt.Errorf("bridge shutting down")
	}
	if c := b.ctrl; c != nil {
		select {
		case <-c.Done(): // stale: drop + close, reconnect below
			b.ctrl = nil
			b.mu.Unlock()
			_ = c.Close()
		default:
			b.mu.Unlock()
			return c, nil
		}
	} else {
		b.mu.Unlock()
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		c, err := b.ensure()
		if err == nil {
			b.mu.Lock()
			if b.closing {
				b.mu.Unlock()
				_ = c.Close()
				return nil, fmt.Errorf("bridge shutting down")
			}
			if b.ctrl != nil { // a racing caller already reconnected
				existing := b.ctrl
				b.mu.Unlock()
				_ = c.Close()
				return existing, nil
			}
			b.ctrl = c
			b.mu.Unlock()
			return c, nil
		}
		lastErr = err
		time.Sleep(time.Duration(150*(1<<attempt)) * time.Millisecond)
	}
	return nil, fmt.Errorf("daemon unavailable: %w", lastErr)
}

// Shutdown stops the health loop, tears down every pump, and closes the control
// client. Idempotent-safe via the closing flag + per-pump sync.Once guards.
func (b *Bridge) Shutdown() {
	b.mu.Lock()
	b.closing = true
	pumps := make([]*sessionPump, 0, len(b.pumps))
	for _, p := range b.pumps {
		pumps = append(pumps, p)
	}
	b.pumps = map[string]*sessionPump{}
	ctrl := b.ctrl
	b.ctrl = nil
	remoteCtrls := make([]*daemon.Client, 0, len(b.remoteCtrls))
	for _, c := range b.remoteCtrls {
		remoteCtrls = append(remoteCtrls, c)
	}
	b.remoteCtrls = map[string]*daemon.Client{}
	stop := b.pollStop
	b.pollStop = nil
	feedStop := b.feedStop
	b.feedStop = nil
	b.mu.Unlock()

	if stop != nil {
		close(stop)
	}
	if feedStop != nil {
		close(feedStop)
	}
	for _, p := range pumps {
		p.stopOnce.Do(func() { close(p.stop) })
		if p.client != nil {
			p.closeOnce.Do(func() { _ = p.client.Close() })
		}
	}
	if ctrl != nil {
		_ = ctrl.Close()
	}
	// Close every pooled remote control client — each closes its underlying ssh
	// process, so no remote daemon connection / ssh subprocess outlives the GUI.
	for _, c := range remoteCtrls {
		_ = c.Close()
	}
	// Stop any voice loop / in-flight mic op so its goroutine + subprocess don't
	// outlive the window.
	if b.voiceCtl != nil {
		_ = b.VoiceModeStop()
	}
	// Kill every live PTY terminal so its shell + reader/waiter goroutines don't
	// outlive the window (terminal.go owns the registry).
	terminalShutdownAll()
}

// ---- health ----

// Ping verifies the daemon connection.
func (b *Bridge) Ping() error {
	c, err := b.control()
	if err != nil {
		return err
	}
	return c.Ping()
}

// Stats returns the daemon resource-health snapshot. NOTE: this is the RAW
// *daemon.DaemonStats (snake_case daemon tags, no DTO) — see the DaemonStats
// parity contract near eventDaemonStats; the types.ts DaemonStats block mirrors
// those tags 1:1.
func (b *Bridge) Stats() (*daemon.DaemonStats, error) {
	c, err := b.control()
	if err != nil {
		return nil, err
	}
	return c.Stats()
}

// GUIVersion returns the build-stamped version of THIS gui binary (the one the
// desktop shell is running), independent of the daemon's version. The frontend
// compares it against DaemonStats.version to flag a daemon/gui mismatch — they
// can diverge when the daemon was started from an older build than the GUI.
func (b *Bridge) GUIVersion() string {
	return llm.FullVersion()
}

// ---- session lifecycle ----

// Sessions lists hosted sessions, newest-updated first.
func (b *Bridge) Sessions() ([]SessionInfoDTO, error) {
	c, err := b.control()
	if err != nil {
		return nil, err
	}
	infos, err := c.List()
	if err != nil {
		return nil, err
	}
	out := make([]SessionInfoDTO, 0, len(infos))
	for _, in := range infos {
		out = append(out, toSessionInfoDTO(in))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated > out[j].Updated })
	return out, nil
}

// NewSession creates a session rooted at dir (default: cwd) and returns its id.
func (b *Bridge) NewSession(dir, model, perm string) (string, error) {
	c, err := b.control()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(dir) == "" {
		if wd, e := os.Getwd(); e == nil {
			dir = wd
		}
	}
	return c.NewSession(dir, model, perm, nil)
}

// RemoveSession stops the session's pump and removes it from the daemon. A
// remote session is removed on its own daemon; the ref resolves to that daemon's
// pooled control client.
func (b *Bridge) RemoveSession(id string) error {
	b.stopPump(id)
	c, realID, err := b.clientFor(id)
	if err != nil {
		return err
	}
	return c.Remove(realID)
}

// PruneSessions removes idle/empty sessions and stops their pumps.
func (b *Bridge) PruneSessions() ([]string, error) {
	c, err := b.control()
	if err != nil {
		return nil, err
	}
	pruned, err := c.Prune()
	if err != nil {
		return nil, err
	}
	for _, id := range pruned {
		b.stopPump(id)
	}
	return pruned, nil
}

// State returns the full session snapshot (history + status).
func (b *Bridge) State(id string) (*SessionStateDTO, error) {
	c, realID, err := b.clientFor(id)
	if err != nil {
		return nil, err
	}
	st, err := c.State(realID)
	if err != nil {
		return nil, err
	}
	return toSessionStateDTO(st), nil
}

// ---- turn I/O ----

// SendInput delivers text+images via the daemon `input` op (carries allowTools).
// Returns only error; the UI derives running-state from the stream + State(),
// never a racy synthetic guess.
func (b *Bridge) SendInput(id, text string, images []ImageDTO, allowTools []string) error {
	c, realID, err := b.clientFor(id)
	if err != nil {
		return err
	}
	imgs, err := fromImageDTOs(images)
	if err != nil {
		return err
	}
	return c.Input(realID, text, imgs, allowTools)
}

// SteerInput injects a message mid-turn (between tool rounds) when a turn is
// running, returning true if it was delivered as a steer (vs starting a fresh
// turn). The composer routes through this while the session is running.
func (b *Bridge) SteerInput(id, text string, images []ImageDTO) (bool, error) {
	c, realID, err := b.clientFor(id)
	if err != nil {
		return false, err
	}
	imgs, err := fromImageDTOs(images)
	if err != nil {
		return false, err
	}
	return c.SteerInput(realID, text, imgs)
}

// Interrupt cancels the in-flight turn. Returns whether a running turn was
// actually cancelled (false when the session was already idle).
func (b *Bridge) Interrupt(id string) (bool, error) {
	c, realID, err := b.clientFor(id)
	if err != nil {
		return false, err
	}
	return c.Interrupt(realID)
}

// Resend retries the last turn.
func (b *Bridge) Resend(id string) error {
	c, realID, err := b.clientFor(id)
	if err != nil {
		return err
	}
	return c.Resend(realID)
}

// Approve resolves a gated tool-call approval.
func (b *Bridge) Approve(id, approvalID string, allow bool) error {
	c, realID, err := b.clientFor(id)
	if err != nil {
		return err
	}
	return c.Approve(realID, approvalID, allow)
}

// ---- maintenance ----

// Compact summarizes the conversation toward target tokens.
func (b *Bridge) Compact(id string, target int) (CompactResultDTO, error) {
	c, realID, err := b.clientFor(id)
	if err != nil {
		return CompactResultDTO{}, err
	}
	before, after, err := c.Compact(realID, target)
	return CompactResultDTO{Before: before, After: after}, err
}

// Clear wipes the session conversation.
func (b *Bridge) Clear(id string) error {
	c, realID, err := b.clientFor(id)
	if err != nil {
		return err
	}
	return c.Clear(realID)
}

// ---- settings (each returns the fresh state so the UI reconciles optimism) ----

func (b *Bridge) setThen(id string, fn func(c *daemon.Client, realID string) error) (*SessionStateDTO, error) {
	c, realID, err := b.clientFor(id)
	if err != nil {
		return nil, err
	}
	if err := fn(c, realID); err != nil {
		return nil, err
	}
	st, err := c.State(realID)
	if err != nil {
		return nil, err
	}
	return toSessionStateDTO(st), nil
}

// SetModel switches the session's model.
func (b *Bridge) SetModel(id, model string) (*SessionStateDTO, error) {
	return b.setThen(id, func(c *daemon.Client, realID string) error { return c.SetModel(realID, model) })
}

// SetPerm switches the permission posture (gated|auto).
func (b *Bridge) SetPerm(id, perm string) (*SessionStateDTO, error) {
	return b.setThen(id, func(c *daemon.Client, realID string) error { return c.SetPerm(realID, perm) })
}

// SetGoal sets the session goal.
func (b *Bridge) SetGoal(id, goal string) (*SessionStateDTO, error) {
	return b.setThen(id, func(c *daemon.Client, realID string) error { return c.SetGoal(realID, goal) })
}

// SetTitle renames the session.
func (b *Bridge) SetTitle(id, title string) (*SessionStateDTO, error) {
	return b.setThen(id, func(c *daemon.Client, realID string) error { return c.SetTitle(realID, title) })
}

// SetEffort sets the reasoning-effort level.
func (b *Bridge) SetEffort(id, level string) (*SessionStateDTO, error) {
	return b.setThen(id, func(c *daemon.Client, realID string) error { return c.SetEffort(realID, level) })
}

// SetSearch sets the provider search mode.
func (b *Bridge) SetSearch(id, mode string) (*SessionStateDTO, error) {
	return b.setThen(id, func(c *daemon.Client, realID string) error { return c.SetSearch(realID, mode) })
}

// SetFast toggles the fast/priority service tier.
func (b *Bridge) SetFast(id string, on bool) (*SessionStateDTO, error) {
	return b.setThen(id, func(c *daemon.Client, realID string) error { return c.SetFast(realID, on) })
}

// ---- sandbox / shells / dirs ----

// AddDir grants the session's tools an extra working directory.
func (b *Bridge) AddDir(id, path string) (string, error) {
	c, realID, err := b.clientFor(id)
	if err != nil {
		return "", err
	}
	return c.AddDir(realID, path)
}

// KillShell signals a backgrounded shell.
func (b *Bridge) KillShell(id, shellID string) (bool, error) {
	c, realID, err := b.clientFor(id)
	if err != nil {
		return false, err
	}
	return c.KillShell(realID, shellID)
}

// DetachBash backgrounds the foreground bash, freeing the turn.
func (b *Bridge) DetachBash(id string) (bool, error) {
	c, realID, err := b.clientFor(id)
	if err != nil {
		return false, err
	}
	return c.DetachBash(realID)
}

// ---- workflows (internal/workflow) ----

// WorkflowInfoDTO is one authored workflow for the GUI menu: its name (the file
// basename, i.e. the arg `/workflow <name>` takes), the frontmatter description,
// and how many steps it has.
type WorkflowInfoDTO struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Steps       int    `json:"steps"`
}

// WorkflowResultDTO summarizes a finished RunWorkflow over the carried session:
// the step ids that ran, the step that stopped the run ("" = all ok), and each
// step's output text keyed by step id. It mirrors workflow.Result.
type WorkflowResultDTO struct {
	Completed []string          `json:"completed"`
	FailedAt  string            `json:"failedAt,omitempty"`
	Outputs   map[string]string `json:"outputs"`
}

// Workflows lists the authored workflows under ~/.eigen/workflows. Each is
// loaded so its description and step count are surfaced; a workflow that fails
// to parse is skipped (it can't be run anyway). This is the GUI peer of the
// TUI's `/workflow` (no args) listing.
func (b *Bridge) Workflows() ([]WorkflowInfoDTO, error) {
	names := workflow.List()
	out := make([]WorkflowInfoDTO, 0, len(names))
	for _, name := range names {
		wf, err := workflow.Load(name)
		if err != nil {
			continue // unparseable workflow — not runnable, omit
		}
		out = append(out, WorkflowInfoDTO{Name: wf.Name, Description: wf.Description, Steps: len(wf.Steps)})
	}
	return out, nil
}

// RunWorkflow plays an authored workflow's steps in order on ONE daemon session
// (so step N sees prior steps' work), driving workflow.Run with a StepRunner
// that submits each step's prompt over the daemon and waits for that turn to
// finish. {{var.NAME}} placeholders are filled from vars.
//
// No Judge is wired here, so a step carrying a `check:` directive fails closed
// with a clear error (matching the headless `eigen run` contract that a check
// needs a judge) — judged checks + on_failure remain a headless / follow-up
// concern, exactly as the TUI's in-chat `/workflow` keeps it to "play the
// prompts in order". A richer GUI surface (live step progress via the existing
// session stream, judged checks) is a follow-up; this is the bridge seam.
func (b *Bridge) RunWorkflow(sessionID, name string, vars map[string]string) (*WorkflowResultDTO, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session id required")
	}
	wf, err := workflow.Load(name)
	if err != nil {
		return nil, err
	}
	c, err := b.control()
	if err != nil {
		return nil, err
	}
	res, runErr := wf.Run(context.Background(), workflow.RunOpts{
		Vars: vars,
		Run:  b.daemonStepRunner(c, sessionID),
	})
	var dto *WorkflowResultDTO
	if res != nil {
		dto = &WorkflowResultDTO{Completed: res.Completed, FailedAt: res.FailedAt, Outputs: res.Outputs}
	}
	return dto, runErr
}

// daemonStepRunner returns a workflow.StepRunner that drives one step on the
// daemon session: submit the step's prompt, then poll the session until the turn
// finishes and return its final assistant text. The model arg (a step's explicit
// `model:`) is honored as a live switch on the session for the step's duration,
// then restored — so step N keeps the carried context. A failed switch keeps
// context by running on the session's current model.
func (b *Bridge) daemonStepRunner(c *daemon.Client, sessionID string) workflow.StepRunner {
	return func(ctx context.Context, prompt, model string) (string, error) {
		if model != "" {
			if err := c.SetModel(sessionID, model); err == nil {
				if prev, perr := c.State(sessionID); perr == nil && prev.Model != "" {
					defer func() { _ = c.SetModel(sessionID, prev.Model) }()
				}
			}
			// A failed switch is non-fatal: keep context on the current model.
		}
		if err := c.Input(sessionID, prompt, nil, nil); err != nil {
			return "", err
		}
		return b.awaitTurn(ctx, c, sessionID)
	}
}

// awaitTurn blocks until the session's in-flight turn finishes (Running goes
// false) or ctx is cancelled, then returns the last assistant message's text.
// It polls State at ~4Hz; the daemon's `input` op marks the session running
// before returning, so there's no start-race where we observe "not running"
// before the turn begins.
func (b *Bridge) awaitTurn(ctx context.Context, c *daemon.Client, sessionID string) (string, error) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-t.C:
			st, err := c.State(sessionID)
			if err != nil {
				return "", err
			}
			if st.Running {
				continue
			}
			return lastAssistantText(st.Messages), nil
		}
	}
}

// lastAssistantText returns the text of the most recent assistant message (the
// step's "answer"), or "" if there is none.
func lastAssistantText(msgs []llm.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llm.RoleAssistant && strings.TrimSpace(msgs[i].Text) != "" {
			return msgs[i].Text
		}
	}
	return ""
}

// ---- custom commands (internal/command) ----

// CommandInfoDTO is one custom slash command for the GUI menu, mirroring the
// TUI's `/<name>` surface: its name, frontmatter description + argument-hint,
// optional model, allowed-tools restriction, and scope ("project" | "user").
type CommandInfoDTO struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ArgHint      string   `json:"argHint,omitempty"`
	Model        string   `json:"model,omitempty"`
	AllowedTools []string `json:"allowedTools,omitempty"`
	Scope        string   `json:"scope"`
}

// Commands lists the custom slash commands discovered under the project and user
// command dirs (project shadows user), the GUI peer of the TUI's `/<name>` menu.
func (b *Bridge) Commands() ([]CommandInfoDTO, error) {
	set := command.Load(command.Dirs()...)
	cmds := set.All()
	out := make([]CommandInfoDTO, 0, len(cmds))
	for _, cmd := range cmds {
		out = append(out, CommandInfoDTO{
			Name:         cmd.Name,
			Description:  cmd.Description,
			ArgHint:      cmd.ArgHint,
			Model:        cmd.Model,
			AllowedTools: cmd.AllowedTools,
			Scope:        cmd.Scope,
		})
	}
	return out, nil
}

// RunCommand expands a custom command's body with args ($ARGUMENTS / $1..$9, per
// command.Expand) and submits it as a normal turn on the daemon session — the
// GUI peer of the TUI's runCustomCommand. A command's `model:` frontmatter does
// a live model switch on the session first (best-effort: a failed switch runs on
// the current model). `allowed-tools` frontmatter scopes the turn to those tools
// (the daemon enforces and clears it after the turn). Returns the expanded
// prompt that was submitted so the caller can echo it into the transcript.
func (b *Bridge) RunCommand(sessionID, name, args string) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", fmt.Errorf("session id required")
	}
	cmd, ok := command.Load(command.Dirs()...).Get(name)
	if !ok {
		return "", fmt.Errorf("unknown command %q", name)
	}
	prompt := command.Expand(cmd.Body, args)
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("command %q has an empty body", name)
	}
	c, err := b.control()
	if err != nil {
		return "", err
	}
	if cmd.Model != "" {
		_ = c.SetModel(sessionID, cmd.Model) // best-effort; keep going on failure
	}
	if err := c.Input(sessionID, prompt, nil, cmd.AllowedTools); err != nil {
		return "", err
	}
	return prompt, nil
}

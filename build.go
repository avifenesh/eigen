package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/hook"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/lsp"
	"github.com/avifenesh/eigen/internal/mcp"
	"github.com/avifenesh/eigen/internal/memory"
	"github.com/avifenesh/eigen/internal/observe"
	"github.com/avifenesh/eigen/internal/skill"
	"github.com/avifenesh/eigen/internal/tool"
)

// sessionDeps are the per-session resources built by buildSession: the agent
// plus the things the caller must keep alive (MCP clients, the LSP manager) or
// reuse (memory stores, the router). One set per chat — rooted at the chat's
// working directory — so the daemon can host many concurrent sessions, each a
// whole as today, sharing only global memory + config + the session store.
type sessionDeps struct {
	Agent      *agent.Agent
	Provider   llm.Provider
	Router     *autoRouter
	Mem        *memory.Store // project memory (rooted at dir)
	GlobalMem  *memory.Store
	mcpClients []*mcp.Client
	lspMgr     *lsp.Manager
	obsLog     *observe.Logger

	// eventWrap composes observability + hooks under a front-end event sink;
	// hooks is the runner for session-lifecycle hooks (start/stop/resume).
	eventWrap func(agent.EventSink) agent.EventSink
	hooks     *hook.Runner
}

// Close releases the session's external resources.
func (d *sessionDeps) Close() {
	for _, c := range d.mcpClients {
		_ = c.Close()
	}
	if d.lspMgr != nil {
		d.lspMgr.Close()
	}
	if d.obsLog != nil {
		_ = d.obsLog.Close()
	}
}

// buildParams configures a session build.
type buildParams struct {
	Dir       string // working directory (project root) for this chat
	Provider  string
	Model     string
	Perm      string
	MaxTokens int
	Goal      string
	Cfg       config.Config
	Skills    *skill.Set
	GlobalMem *memory.Store // shared across sessions
}

// buildSession constructs a complete agent for a chat rooted at p.Dir: tools
// (rooted at the dir), per-directory project memory, MCP/LSP/plugins, the
// router, cross-vendor review/judge, observability + hooks, and the small-model
// compactor. It mirrors main's inline wiring but is reusable per session so the
// daemon can host several at once. Tool/plugin/MCP/LSP load errors are written
// to stderr (non-fatal), matching main.
func buildSession(p buildParams) (*sessionDeps, error) {
	deps := &sessionDeps{GlobalMem: p.GlobalMem}

	// Tools are rooted at the SESSION's dir, not the daemon's cwd. Relative
	// paths in tool args (and bash's working dir) must resolve against the
	// project the chat lives in.
	policy := tool.NewPolicy(p.Dir)
	mem, _ := memory.Open(p.Dir) // project memory rooted at this chat's dir
	deps.Mem = mem

	if p.Cfg.ObserveEnabled() {
		if lg, err := observe.Open(observe.DefaultPath(), ""); err == nil {
			deps.obsLog = lg
		}
	}
	hookRunner, herr := hook.Load(hookConfigPath())
	if herr != nil {
		fmt.Fprintln(os.Stderr, "eigen: hooks:", herr)
	}

	router := newAutoRouter(p.Cfg.Route, p.Cfg.RouteProviders, p.Provider)
	deps.Router = router

	taskRun := func(ctx context.Context, t string, opts tool.TaskOpts, background bool) (string, error) {
		if deps.Agent == nil {
			return "", fmt.Errorf("subtasks unavailable")
		}
		aopts := agent.SubtaskOpts{Kind: opts.Kind, Difficulty: opts.Difficulty, Model: opts.Model}
		if background {
			return deps.Agent.SubtaskBackground(ctx, t, aopts)
		}
		return deps.Agent.SubtaskWith(ctx, t, aopts)
	}
	taskStatus := func(ctx context.Context, id string, all bool) (string, error) {
		if deps.Agent == nil || deps.Agent.Bg == nil {
			return "", fmt.Errorf("background tasks unavailable")
		}
		return formatTaskStatus(deps.Agent.Bg, id, all), nil
	}
	taskGroup := func(ctx context.Context, subs []tool.GroupSubtaskArg, workers int, synthesize string) (string, error) {
		if deps.Agent == nil {
			return "", fmt.Errorf("task_group unavailable")
		}
		gs := make([]agent.GroupSubtask, len(subs))
		for i, s := range subs {
			gs[i] = agent.GroupSubtask{Task: s.Task, Role: s.Role, Kind: s.Kind, Difficulty: s.Difficulty, Model: s.Model}
		}
		return deps.Agent.TaskGroup(ctx, gs, workers, synthesize)
	}
	taskGroupMut := func(ctx context.Context, subs []tool.GroupSubtaskArg, workers int) (string, error) {
		if deps.Agent == nil {
			return "", fmt.Errorf("task_group_mutating unavailable")
		}
		gs := make([]agent.GroupSubtask, len(subs))
		for i, s := range subs {
			gs[i] = agent.GroupSubtask{Task: s.Task, Kind: s.Kind, Difficulty: s.Difficulty, Model: s.Model}
		}
		// Apply-time approval reuses the session's tool-approval channel: one
		// prompt naming the merge + diffstat. In auto mode Approve is nil →
		// applies freely.
		approve := func(ctx context.Context, summary string, diff []byte) (bool, error) {
			// Auto mode applies without prompting (read CurrentPerm, not just
			// Approve!=nil — the callback stays wired in auto so /perm can flip
			// to gated live). Only a GATED session gates the apply.
			if deps.Agent.CurrentPerm() != agent.PermGated || deps.Agent.Approve == nil {
				return true, nil
			}
			args, _ := json.Marshal(map[string]string{"summary": summary, "diffstat": agent.PatchStat(diff)})
			return deps.Agent.Approve(ctx, "task_group_mutating (apply)", args)
		}
		return deps.Agent.TaskGroupMutating(ctx, gs, workers, approve)
	}
	goalJudge := func(ctx context.Context, evidence string) (bool, string, error) {
		if deps.Agent == nil {
			return false, "", fmt.Errorf("goal judging unavailable")
		}
		var judge llm.Provider
		if jm := firstNonEmpty(os.Getenv("EIGEN_JUDGE_MODEL"), p.Cfg.JudgeModel); jm != "" {
			if jp, err := llm.New("", jm); err == nil {
				judge = jp
			}
		}
		if judge == nil {
			author := effectiveModel(p.Provider, p.Model)
			if rev := llm.CrossReviewer(author, llm.AllCredentialedModels()); rev != "" {
				if jp, err := router.providerFor(rev); err == nil {
					judge = jp
				}
			}
		}
		return deps.Agent.JudgeGoal(ctx, judge, evidence)
	}
	reviewRun := router.crossReviewer(func() string { return effectiveModel(p.Provider, p.Model) })

	defs := []tool.Definition{
		tool.Read(policy), tool.List(policy), tool.Glob(policy), tool.Grep(policy),
		tool.Symbols(policy), tool.Tree(policy), tool.Diff(policy), tool.Write(policy),
		tool.Edit(policy), tool.MultiEdit(policy), tool.Patch(policy), tool.Move(policy),
		tool.Bash(policy), tool.Fetch(), tool.Todo(), tool.Skill(p.Skills),
		tool.Memory(mem, p.GlobalMem), tool.Task(taskRun), tool.TaskStatus(taskStatus),
		tool.TaskGroup(taskGroup), tool.TaskGroupMutating(taskGroupMut),
		tool.Retrieve(retrieveRunner(p.Dir)),
		tool.GenerateImage(imageGenRunner(p.Dir)),
		tool.GoalAchieved(goalJudge), tool.Review(reviewRun),
		tool.WebSearch(), // always available: keyless chain, keyed/SearXNG preferred
	}
	builtin := map[string]bool{}
	for _, d := range defs {
		builtin[d.Name] = true
	}
	if plugins, perr := tool.LoadPlugins(pluginPaths()...); perr != nil {
		fmt.Fprintln(os.Stderr, "eigen: plugins:", perr)
	} else {
		for _, pl := range plugins {
			if builtin[pl.Name] {
				continue
			}
			defs = append(defs, pl)
			builtin[pl.Name] = true
		}
	}
	mcpDefs, mcpClients, mcpErrs := mcp.LoadTools(context.Background(), mcpConfigPath())
	for _, e := range mcpErrs {
		fmt.Fprintln(os.Stderr, "eigen: mcp:", e)
	}
	deps.mcpClients = mcpClients
	for _, d := range mcpDefs {
		if builtin[d.Name] {
			continue
		}
		defs = append(defs, d)
		builtin[d.Name] = true
	}
	lspDefs, lspMgr, lspErrs := lsp.LoadTools(p.Dir, lspConfigPath())
	for _, e := range lspErrs {
		fmt.Fprintln(os.Stderr, "eigen: lsp:", e)
	}
	deps.lspMgr = lspMgr
	for _, d := range lspDefs {
		if builtin[d.Name] {
			continue
		}
		defs = append(defs, d)
		builtin[d.Name] = true
	}

	registry, err := tool.NewRegistry(defs...)
	if err != nil {
		deps.Close()
		return nil, err
	}

	prov, err := llm.New(p.Provider, p.Model)
	if err != nil {
		deps.Close()
		return nil, err
	}
	deps.Provider = prov

	// ExtraSystem: skills catalog + this dir's AGENTS.md guidance.
	extraSystem := p.Skills.Catalog()
	if g := agentsGuidance(p.Dir); g != "" {
		if extraSystem != "" {
			extraSystem += "\n\n"
		}
		extraSystem += g
	}

	smallCompactor := llm.NewCompactor(smallProvider(prov))
	a := &agent.Agent{
		Provider:         prov,
		Tools:            registry,
		Perm:             agent.Permission(p.Perm),
		MaxContextTokens: contextBudget(p.MaxTokens, p.Provider, p.Model),
		Compactor:        llm.CompactorChain(smallCompactor, llm.NewCompactor(prov)),
		ExtraSystem:      extraSystem,
		Memory:           memory.Sections(p.GlobalMem, mem),
		Goal:             p.Goal,
		Router:           router.Route,
		ModelProvider:    router.providerFor,
		Bg:               agent.NewBgRegistry(agent.TasksDir()),
		SessionDir:       p.Dir,
		WorktreeTools:    worktreeTools,
	}
	deps.Agent = a
	deps.eventWrap = func(next agent.EventSink) agent.EventSink {
		return deps.obsLog.Wrap(hookRunner.Wrap(next, ""))
	}
	a.EventWrap = deps.eventWrap
	deps.hooks = hookRunner
	return deps, nil
}

// worktreeTools builds the IMPLEMENTER toolset for a mutating fan-out child,
// rooted at its isolated git worktree: read/search/write/edit/move only — NO
// bash, NO git, NO network (a worktree confines file writes but not shelling
// out). The policy denies .git (global deniedSegments) so a child can't break
// its worktree's git linkage. A bad registry build yields an empty toolset
// (the child simply can't act) rather than a panic.
func worktreeTools(dir string) *tool.Registry {
	policy := tool.NewPolicy(dir)
	reg, err := tool.NewRegistry(
		tool.Read(policy), tool.List(policy), tool.Glob(policy), tool.Grep(policy),
		tool.Symbols(policy), tool.Tree(policy), tool.Diff(policy),
		tool.Write(policy), tool.Edit(policy), tool.MultiEdit(policy),
		tool.Patch(policy), tool.Move(policy), tool.Todo(),
	)
	if err != nil {
		reg, _ = tool.NewRegistry()
	}
	return reg
}

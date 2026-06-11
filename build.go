package main

import (
	"context"
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

	taskRun := func(ctx context.Context, t, kind, difficulty string) (string, error) {
		if deps.Agent == nil {
			return "", fmt.Errorf("subtasks unavailable")
		}
		return deps.Agent.Subtask(ctx, t, kind, difficulty)
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
		tool.Memory(mem, p.GlobalMem), tool.Task(taskRun),
		tool.GoalAchieved(goalJudge), tool.Review(reviewRun),
	}
	if ws, ok := tool.WebSearch(); ok {
		defs = append(defs, ws)
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
	}
	deps.Agent = a
	deps.eventWrap = func(next agent.EventSink) agent.EventSink {
		return deps.obsLog.Wrap(hookRunner.Wrap(next, ""))
	}
	a.EventWrap = deps.eventWrap
	deps.hooks = hookRunner
	return deps, nil
}

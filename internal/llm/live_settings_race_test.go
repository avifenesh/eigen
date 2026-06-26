package llm

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestProviderLiveSettingsConcurrentRequestBuild(t *testing.T) {
	req := Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}}
	levels := []string{"low", "medium", "high"}
	searchModes := []string{"off", "auto", "on"}

	t.Run("mantle effort", func(t *testing.T) {
		m := &Mantle{Model: "openai.gpt-5", effort: "high"}
		runConcurrent(200, func(i int) { _ = m.SetEffort(levels[i%len(levels)]) }, func(i int) {
			effort := m.snapshot()
			_, _ = json.Marshal(responsesRequest{Model: m.Model, Input: buildInput(req), Reasoning: &reasoningConfig{Effort: effort}})
		})
	})

	t.Run("anthropic effort", func(t *testing.T) {
		a := &Anthropic{Model: "claude-sonnet-4-5-20250929", adaptive: true, effort: "medium"}
		runConcurrent(200, func(i int) { _ = a.SetEffort(levels[i%len(levels)]) }, func(i int) {
			thinkingBudget, effort, adaptive := a.snapshotThinking()
			payload := anthropicRequest{Model: a.Model, MaxTokens: anthropicMaxTok, Messages: anthropicMessages(req)}
			switch {
			case adaptive && effort != "" && effort != "minimal" && effort != "off":
				payload.Thinking = json.RawMessage(`{"type":"adaptive"}`)
				payload.OutputConfig = json.RawMessage(`{"effort":"` + effort + `"}`)
			case !adaptive && thinkingBudget > 0:
				payload.Thinking = json.RawMessage(`{"type":"enabled"}`)
			}
			_, _ = json.Marshal(payload)
		})
	})

	t.Run("converse effort", func(t *testing.T) {
		c := &Converse{Model: "us.anthropic.claude-opus-4-8", context1M: true, adaptive: true, effort: "medium"}
		runConcurrent(200, func(i int) { _ = c.SetEffort(levels[i%len(levels)]) }, func(i int) {
			context1M, thinkingBudget, effort, adaptive := c.snapshotSettings()
			_ = additionalConverseFields(context1M, thinkingBudget, effort, adaptive)
		})
	})

	t.Run("grok search", func(t *testing.T) {
		g := &Grok{c: newChatClient("https://example.invalid/v1", "grok-4", "key", "grok"), search: "auto", sources: []string{"web", "x"}}
		g.c.extra = g.searchParams
		runConcurrent(200, func(i int) { _ = g.SetSearch(searchModes[i%len(searchModes)]) }, func(i int) {
			search, sources := g.snapshot()
			cc := *g.c
			cc.extra = func() map[string]any { return grokSearchParams(search, sources) }
			_, _ = cc.body(grokPrepare(req, search, sources), false)
		})
	})

	t.Run("glm search and effort", func(t *testing.T) {
		g := &GLM{c: newChatClient("https://example.invalid/v1", "glm-5.1", "key", "glm"), search: "auto", thinking: "enabled"}
		g.c.extraTools = g.webSearchTool
		g.c.extra = g.bodyExtra
		runConcurrent(200, func(i int) {
			_ = g.SetSearch(searchModes[i%len(searchModes)])
			if i%2 == 0 {
				_ = g.SetEffort("off")
			} else {
				_ = g.SetEffort("on")
			}
		}, func(i int) {
			search, thinking, effort, clearThinking := g.snapshot()
			cc := *g.c
			cc.extraTools = func() []map[string]any { return glmWebSearchTool(search) }
			cc.extra = func() map[string]any { return glmBodyExtra(thinking, effort, clearThinking) }
			_, _ = cc.body(glmPrepare(req, search), false)
		})
	})
}

func runConcurrent(n int, write func(int), read func(int)) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			write(i)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			read(i)
		}
	}()
	wg.Wait()
}

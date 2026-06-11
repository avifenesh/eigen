package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/avifenesh/eigen/internal/llm"
)

// autoRouter implements the opt-in per-task model router. It is the glue
// between the pure policy (llm.Route), classification (llm.Classify /
// orchestrator-stated kind+difficulty), candidate detection (llm.RouteCandidates),
// and provider construction (llm.New). Constructed providers are cached so
// repeated routing to the same model is cheap.
type autoRouter struct {
	mu        sync.Mutex
	enabled   bool
	providers []string // cross-provider allowlist (canonical); empty = current only
	current   string   // the user's base provider (always a candidate)
	cache     map[string]llm.Provider
}

func newAutoRouter(enabled bool, providers []string, current string) *autoRouter {
	return &autoRouter{
		enabled:   enabled,
		providers: providers,
		current:   current,
		cache:     map[string]llm.Provider{},
	}
}

func (r *autoRouter) SetEnabled(on bool) {
	r.mu.Lock()
	r.enabled = on
	r.mu.Unlock()
}

func (r *autoRouter) Enabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enabled
}

// route picks a provider+model for a task. Returns (provider, modelID, label)
// or (nil, "", "") to keep the current model — when routing is off, no
// candidate is capable, or the chosen model can't be built. The label is a
// short reason for the UI ("routed → glm-4.7: medium").
func (r *autoRouter) Route(ctx context.Context, prompt, kind, difficulty string, hasImage bool) (llm.Provider, string, string) {
	r.mu.Lock()
	enabled := r.enabled
	providers := append([]string(nil), r.providers...)
	current := r.current
	r.mu.Unlock()
	// An attached image is a capability NEED: even with routing off, a
	// non-vision model can't see it — the TUI calls Route in that case and we
	// honor it. Plain prompts respect the toggle.
	if !enabled && !hasImage {
		return nil, "", ""
	}

	// Kind/difficulty: orchestrator-stated wins; else classify the prompt.
	k, kExplicit := llm.ParseTaskKind(kind)
	d, dExplicit := llm.ParseDifficulty(difficulty)
	ck, cd := llm.Classify(prompt, hasImage)
	if !kExplicit {
		k = ck
	}
	if !dExplicit {
		d = cd
	}
	// An attached image always forces vision regardless of stated kind.
	if hasImage {
		k = llm.TaskVision
	}

	candidates := llm.RouteCandidates(current, providers)
	chosen, ok := llm.Route(llm.RouteRequest{
		Kind:       k,
		Difficulty: d,
		Frontend:   llm.IsFrontend(prompt),
		Candidates: candidates,
	})
	if !ok {
		return nil, "", ""
	}

	prov, err := r.providerFor(chosen)
	if err != nil {
		return nil, "", ""
	}
	label := fmt.Sprintf("routed → %s (%s/%s)", chosen, kindName(k), diffName(d))
	return prov, chosen, label
}

// providerFor builds (and caches) the provider for a model id.
func (r *autoRouter) providerFor(model string) (llm.Provider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.cache[model]; ok {
		return p, nil
	}
	p, err := llm.New("", model) // provider resolved from the catalog
	if err != nil {
		return nil, err
	}
	r.cache[model] = p
	return p, nil
}

func kindName(k llm.TaskKind) string {
	switch k {
	case llm.TaskSearch:
		return "search"
	case llm.TaskVision:
		return "vision"
	case llm.TaskSocial:
		return "social"
	default:
		return "general"
	}
}

func diffName(d llm.Difficulty) string {
	switch d {
	case llm.DiffTrivial:
		return "trivial"
	case llm.DiffEasy:
		return "easy"
	case llm.DiffHard:
		return "hard"
	default:
		return "medium"
	}
}

// Providers returns the cross-provider allowlist (canonical names).
func (r *autoRouter) Providers() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.providers...)
}

// crossReviewer builds the cross-vendor review function: it picks a reviewer
// model from the OTHER vendor (GPT reviews Claude, Claude reviews GPT — never
// self-review), constructs it, and asks for a critique of the artifact. The
// reviewer is chosen over the router's candidate set (credentialed + allowed
// providers); falls back to any credentialed cross-vendor model when routing
// is restricted. authorModel is read live so it tracks the active model.
func (r *autoRouter) crossReviewer(authorModel func() string) func(context.Context, string, string) (string, error) {
	return func(ctx context.Context, artifact, focus string) (string, error) {
		author := authorModel()
		// Candidates: cross-provider when allowed, else every credentialed
		// provider (review correctness matters more than sparing Bedrock).
		cands := llm.RouteCandidates(r.current, r.Providers())
		if len(cands) == 0 {
			cands = llm.AllCredentialedModels()
		}
		reviewer := llm.CrossReviewer(author, cands)
		if reviewer == "" {
			return "", fmt.Errorf("no cross-vendor reviewer available (need a model from the other vendor)")
		}
		prov, err := r.providerFor(reviewer)
		if err != nil {
			return "", err
		}
		return llm.ReviewArtifact(ctx, prov, reviewer, author, artifact, focus)
	}
}

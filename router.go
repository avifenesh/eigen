package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/llm"
)

// autoRouter implements the opt-in per-task model router. It is the glue
// between the pure policy (llm.Route), prompt-router/orchestrator routing
// assessment, candidate detection (llm.RouteCandidates), and provider
// construction (llm.New). Constructed providers are cached so repeated routing
// to the same model is cheap.
type autoRouter struct {
	mu                 sync.Mutex
	enabled            bool
	providers          []string // cross-provider allowlist (canonical); empty = current only
	current            string   // the user's base provider (always a candidate)
	cache              map[string]llm.Provider
	routeModel         string // local prompt-router model ref (llama by default)
	routeProvider      llm.Provider
	routeProviderRef   string
	assessor           routeAssessor
	localRouteAssessor bool
}

type routeAssessment struct {
	Kind       llm.TaskKind
	Difficulty llm.Difficulty
	Frontend   bool
	Model      string // optional direct destination chosen by a prompt router
	Assessor   string
}

type routeAssessor func(context.Context, string, bool, []string) (routeAssessment, error)

func newAutoRouter(enabled bool, providers []string, current, routeModel string) *autoRouter {
	model := routeModelFromConfig(routeModel)
	r := &autoRouter{
		enabled:    enabled,
		providers:  providers,
		current:    current,
		cache:      map[string]llm.Provider{},
		routeModel: model,
	}
	if localRouteConfigured(model) {
		r.assessor = r.assessLocalRoute
		r.localRouteAssessor = true
	}
	return r
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

// Route picks a provider+model for a delegated task. Returns (provider, modelID,
// label) or (nil, "", "") to keep the current delegate model. Routing is
// ORCHESTRATOR-DRIVEN: explicitly stated kind/difficulty (the main model's
// delegation decision) always routes, as does a vision subtask capability need.
// Unstated delegated tasks route only when /route is enabled, and then a small
// model assesses the subtask level/capabilities. When a local route model is
// explicitly configured (route_model / EIGEN_ROUTE_MODEL), that assessor is the
// local model; otherwise the legacy small candidate model is used. Routing is
// not keyword-based. The top-level/orchestrator model itself is never changed
// here.
func (r *autoRouter) Route(ctx context.Context, prompt, kind, difficulty string, hasImage bool) (llm.Provider, string, string) {
	r.mu.Lock()
	enabled := r.enabled
	providers := append([]string(nil), r.providers...)
	current := r.current
	assessor := r.assessor
	localAssessor := r.localRouteAssessor
	r.mu.Unlock()

	// Kind/difficulty: orchestrator-stated wins. If /route is on and the
	// delegation did not state both fields, ask the configured prompt router
	// (preferably a small local model) to assess the missing routing fields.
	// Do NOT keyword-classify the prompt for routing: routing should be a model
	// decision that feeds the user's tier chain.
	k, kExplicit := llm.ParseTaskKind(kind)
	d, dExplicit := llm.ParseDifficulty(difficulty)
	explicit := kExplicit || dExplicit
	if !enabled && !hasImage && !explicit {
		return nil, "", ""
	}

	candidates := r.routeCandidates(enabled || explicit || hasImage, current, providers)
	if len(candidates) == 0 {
		return nil, "", "route skipped: no credentialed candidate models"
	}
	assessment := routeAssessment{Kind: k, Difficulty: d, Frontend: false, Assessor: "orchestrator"}
	if hasImage {
		assessment.Kind = llm.TaskVision
	}
	if enabled && (!kExplicit || !dExplicit) {
		var a routeAssessment
		var err error
		if assessor != nil {
			a, err = assessor(ctx, prompt, hasImage, candidates)
		} else {
			a, err = r.assessRoute(ctx, prompt, hasImage, candidates)
		}
		if err != nil {
			if !localAssessor {
				return nil, "", fmt.Sprintf("route skipped: assessor unavailable (%v)", err)
			}
			// A configured local router is an optimization, not a hard dependency.
			// If it is down, slow, or emits bad JSON, degrade to the parser defaults
			// (general/medium unless the orchestrator stated a field) and let the
			// normal policy choose from the allowed candidate set.
			a = routeAssessment{Kind: k, Difficulty: d, Frontend: false, Assessor: "local fallback"}
		}
		assessment = a
		if kExplicit {
			assessment.Kind = k
		}
		if dExplicit {
			assessment.Difficulty = d
		}
	}
	// An attached image always forces vision regardless of stated/assessed kind.
	if hasImage {
		assessment.Kind = llm.TaskVision
	}
	req := llm.RouteRequest{
		Kind:       assessment.Kind,
		Difficulty: assessment.Difficulty,
		Frontend:   assessment.Frontend,
		Candidates: candidates,
	}
	chosen, ok := "", false
	if assessment.Model != "" {
		// A prompt router may choose the concrete destination itself. Validate it
		// through the same capability gate and candidate allowlist before honoring it.
		req.Candidates = []string{assessment.Model}
		chosen, ok = llm.Route(req)
	}
	if !ok {
		// The prompt router either did not choose a concrete model, or chose one
		// that cannot satisfy the assessed/explicit capability. Fall back to the
		// normal policy over all candidates instead of failing the delegation.
		req.Candidates = candidates
		chosen, ok = llm.Route(req)
		if !ok {
			return nil, "", "route skipped: no capable candidate model"
		}
	}

	prov, err := r.providerFor(chosen)
	if err != nil {
		return nil, "", fmt.Sprintf("route skipped: %s unavailable (%v)", chosen, err)
	}
	source := "model-assessed"
	if assessment.Assessor != "" {
		source = "assessed by " + assessment.Assessor
	}
	if explicit && (!enabled || (kExplicit && dExplicit)) {
		source = "orchestrator-stated"
	}
	label := fmt.Sprintf("routed → %s (%s/%s; %s)", chosen, kindName(assessment.Kind), diffName(assessment.Difficulty), source)
	return prov, chosen, label
}

func routeModelFromConfig(configured string) string {
	for _, key := range []string{"EIGEN_ROUTE_MODEL", "EIGEN_ROUTER_MODEL"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return strings.TrimSpace(configured)
}

func localRouteConfigured(routeModel string) bool {
	return strings.TrimSpace(routeModel) != "" ||
		strings.TrimSpace(os.Getenv("EIGEN_ROUTE_MODEL")) != "" ||
		strings.TrimSpace(os.Getenv("EIGEN_ROUTER_MODEL")) != ""
}

func localRouteModelRef(configured string) (provider, model string) {
	model = strings.TrimSpace(configured)
	if model == "" {
		model = strings.TrimSpace(os.Getenv("EIGEN_ROUTE_MODEL"))
	}
	if model == "" {
		model = strings.TrimSpace(os.Getenv("EIGEN_ROUTER_MODEL"))
	}
	if model == "" {
		model = llm.DefaultModel("llama")
	}
	if tag, id := llm.ParseRef(model); tag != "" {
		return tag, id
	}
	// Untagged route_model values are LOCAL model ids, even if the same string is
	// known in the remote catalog. This lets a llama.cpp/Ollama server expose
	// arbitrary names (including names that contain ':') without accidentally
	// routing the router itself to a remote provider.
	return "llama", model
}

func localRouteAssessorName(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "local"
	}
	return "local:" + model
}

func (r *autoRouter) assessLocalRoute(ctx context.Context, prompt string, hasImage bool, candidates []string) (routeAssessment, error) {
	provider, model := localRouteModelRef(r.routeModel)
	if llm.CanonicalProvider(provider) == "llama" {
		base := strings.TrimSpace(os.Getenv("EIGEN_LLAMA_BASE_URL"))
		if base == "" {
			return routeAssessment{}, fmt.Errorf("local router requires EIGEN_LLAMA_BASE_URL (or set route_model to a custom local provider ref)")
		}
		if !localReady(base) {
			return routeAssessment{}, fmt.Errorf("local router is not ready at %s", base)
		}
	}
	prov, err := r.localRouteProvider(provider, model)
	if err != nil {
		return routeAssessment{}, err
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	resp, err := prov.Complete(cctx, llm.Request{
		System: `You are Eigen's LOCAL prompt router. Decide where a delegated subtask should run. Do not solve the task. Return ONLY compact JSON with keys: kind, difficulty, frontend, model.
kind must be one of: general, search, vision, social.
difficulty must be one of: trivial, easy, medium, hard.
frontend must be true only for UI/visual/frontend/design work.
model is required and MUST be exactly one of the candidate model ids. If unsure, still choose the safest capable candidate.`,
		Messages: []llm.Message{{Role: llm.RoleUser, Text: localRouteAssessmentPrompt(prompt, hasImage, candidates)}},
	})
	if err != nil {
		return routeAssessment{}, err
	}
	a, err := parseRouteAssessment(resp.Text)
	if err != nil {
		return routeAssessment{}, err
	}
	if a.Model != "" {
		normalized, ok := normalizeAssessedModel(a.Model, candidates)
		if !ok {
			return routeAssessment{}, fmt.Errorf("local router chose non-candidate model %q", a.Model)
		}
		a.Model = normalized
	}
	a.Assessor = localRouteAssessorName(model)
	if hasImage {
		a.Kind = llm.TaskVision
	}
	return a, nil
}

func (r *autoRouter) localRouteProvider(provider, model string) (llm.Provider, error) {
	ref := provider + ":" + model
	r.mu.Lock()
	if r.routeProvider != nil && r.routeProviderRef == ref {
		p := r.routeProvider
		r.mu.Unlock()
		return p, nil
	}
	r.mu.Unlock()
	p, err := llm.New(provider, model)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.routeProvider = p
	r.routeProviderRef = ref
	r.mu.Unlock()
	return p, nil
}

func (r *autoRouter) assessRoute(ctx context.Context, prompt string, hasImage bool, candidates []string) (routeAssessment, error) {
	assessorModel, ok := llm.Route(llm.RouteRequest{Kind: llm.TaskGeneral, Difficulty: llm.DiffTrivial, Candidates: candidates})
	if !ok || assessorModel == "" {
		return routeAssessment{}, fmt.Errorf("no small model candidate")
	}
	prov, err := r.providerFor(assessorModel)
	if err != nil {
		return routeAssessment{}, err
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	resp, err := prov.Complete(cctx, llm.Request{
		System: `You are Eigen's routing assessor. Classify a delegated subtask for model routing. Do not solve the task. Return ONLY compact JSON with keys: kind, difficulty, frontend.
kind must be one of: general, search, vision, social.
difficulty must be one of: trivial, easy, medium, hard.
frontend must be true only for UI/visual/frontend/design work.`,
		Messages: []llm.Message{{Role: llm.RoleUser, Text: routeAssessmentPrompt(prompt, hasImage)}},
	})
	if err != nil {
		return routeAssessment{}, err
	}
	a, err := parseRouteAssessment(resp.Text)
	if err != nil {
		return routeAssessment{}, err
	}
	a.Assessor = assessorModel
	if hasImage {
		a.Kind = llm.TaskVision
	}
	return a, nil
}

func localRouteAssessmentPrompt(prompt string, hasImage bool, candidates []string) string {
	var b strings.Builder
	b.WriteString(routeAssessmentPrompt(prompt, hasImage))
	b.WriteString("\n\ncandidate_models:\n")
	for _, id := range candidates {
		b.WriteString("- ")
		b.WriteString(id)
		if info, ok := llm.Lookup(id); ok {
			b.WriteString(" (provider=")
			b.WriteString(llm.CanonicalProvider(info.Provider))
			if info.Search {
				b.WriteString(", search")
			}
			if info.Vision {
				b.WriteString(", vision")
			}
			if info.Social {
				b.WriteString(", social")
			}
			b.WriteString(")")
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func routeAssessmentPrompt(prompt string, hasImage bool) string {
	const max = 6000
	if len([]rune(prompt)) > max {
		r := []rune(prompt)
		prompt = string(r[:max/2]) + "\n\n[... middle omitted for routing assessment ...]\n\n" + string(r[len(r)-max/2:])
	}
	img := "false"
	if hasImage {
		img = "true"
	}
	return "has_image: " + img + "\n\nsubtask:\n" + prompt
}

func parseRouteAssessment(text string) (routeAssessment, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return routeAssessment{}, fmt.Errorf("no JSON object in assessor output")
	}
	var raw struct {
		Kind        string `json:"kind"`
		Difficulty  string `json:"difficulty"`
		Level       string `json:"level"`
		Frontend    bool   `json:"frontend"`
		Model       string `json:"model"`
		Route       string `json:"route"`
		Destination string `json:"destination"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &raw); err != nil {
		return routeAssessment{}, err
	}
	model := strings.TrimSpace(firstNonEmpty(raw.Model, raw.Route, raw.Destination))
	kind := strings.TrimSpace(raw.Kind)
	k, ok := llm.ParseTaskKind(kind)
	if !ok {
		if kind == "" && model != "" {
			k = llm.TaskGeneral
		} else {
			return routeAssessment{}, fmt.Errorf("invalid kind %q", raw.Kind)
		}
	}
	diff := strings.TrimSpace(raw.Difficulty)
	if diff == "" {
		diff = strings.TrimSpace(raw.Level)
	}
	d, ok := llm.ParseDifficulty(diff)
	if !ok {
		if diff == "" && model != "" {
			d = llm.DiffMedium
		} else {
			return routeAssessment{}, fmt.Errorf("invalid difficulty %q", diff)
		}
	}
	return routeAssessment{Kind: k, Difficulty: d, Frontend: raw.Frontend, Model: model}, nil
}

func (r *autoRouter) routeCandidates(widen bool, current string, providers []string) []string {
	if widen && len(providers) == 0 {
		// Delegated routing should actually roam to the best credentialed tier by
		// default. The old empty allowlist meant "current provider only", which made
		// route=true a near no-op when the current provider had no cheaper/stronger
		// alternatives. Set route_providers to a concrete list to restrict this.
		return llm.AllCredentialedModels()
	}
	return llm.RouteCandidates(current, providers)
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

func normalizeAssessedModel(model string, candidates []string) (string, bool) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", false
	}
	if containsString(candidates, model) {
		return model, true
	}
	if tag, id := llm.ParseRef(model); tag != "" && containsString(candidates, id) {
		return id, true
	}
	return "", false
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
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

// councilRunner builds the adversarial planning function: the AUTHOR is the
// active model, the ADVERSARY is a model from the other vendor (cross-vendor,
// like review). Returns the hardened plan + a short convergence note. Falls
// back to a solo author plan when no cross-vendor model is credentialed.
func (r *autoRouter) councilRunner(authorModel func() string) func(context.Context, string, string) (string, error) {
	return func(ctx context.Context, task, taskContext string) (string, error) {
		author := authorModel()
		authorProv, err := r.providerFor(author)
		if err != nil {
			return "", err
		}
		cfg := llm.CouncilConfig{Author: authorProv, AuthorID: author, MaxRounds: 3}
		// Pick the cross-vendor adversary over the credentialed candidate set,
		// with fallbacks from OTHER vendors (so a flaky primary, e.g. a down
		// endpoint, degrades to a different vendor — not to a solo plan).
		cands := llm.RouteCandidates(r.current, r.Providers())
		if len(cands) == 0 {
			cands = llm.AllCredentialedModels()
		}
		// EIGEN_PLAN_ADVERSARY pins a specific adversary model (skip auto-pick) —
		// useful to force a fast/known-good cross-vendor model.
		advList := llm.CrossVendorAdversaries(author, cands)
		if pin := strings.TrimSpace(os.Getenv("EIGEN_PLAN_ADVERSARY")); pin != "" {
			advList = append([]string{pin}, advList...)
		}
		for _, adv := range advList {
			if ap, err := r.providerFor(adv); err == nil {
				if cfg.Adversary == nil {
					cfg.Adversary, cfg.AdversaryID = ap, adv
				} else {
					cfg.Fallbacks = append(cfg.Fallbacks, llm.AdversaryOption{Provider: ap, ID: adv})
				}
			}
		}
		res, err := llm.Council(ctx, cfg, task, taskContext)
		if err != nil {
			return "", err
		}
		return llm.FormatCouncil(res), nil
	}
}

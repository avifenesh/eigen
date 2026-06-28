package gui

import (
	"strings"

	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/llm"
)

// Config bridge layer. Surfaces the editable ~/.eigen/config.json as a typed
// form: each field carries its key, description, current value, and (for closed
// or dynamic sets) its options. Set validates through config.Set and persists,
// so the GUI can't write an invalid value. File-only fields (telegram, skills
// dirs) are intentionally not exposed here.

// ConfigFieldDTO is one editable config setting + its current value/options.
type ConfigFieldDTO struct {
	Key        string   `json:"key"`
	Desc       string   `json:"desc"`
	Value      string   `json:"value"`
	Options    []string `json:"options,omitempty"`    // closed/dynamic option set ("" = free text)
	Multi      bool     `json:"multi,omitempty"`      // space-separated multi-select
	AllowEmpty bool     `json:"allowEmpty,omitempty"` // empty is a valid (often meaningful) choice — picker offers it
}

// emptyMeaningful lists option-set fields where "" is a real, reachable value
// (e.g. judge_model: empty = automatic cross-vendor judge). Without an explicit
// empty choice in the picker, once a real value is set the unset state becomes
// unreachable — so the view must offer it.
var emptyMeaningful = map[string]bool{
	"model":       true,
	"judge_model": true,
	"route_model": true,
}

// ConfigDTO is the full editable-config snapshot.
type ConfigDTO struct {
	Fields []ConfigFieldDTO `json:"fields"`
	Path   string           `json:"path"`
}

// dynamicOptions resolves a field's dynamic option set (catalog-dependent).
func dynamicOptions(kind string) []string {
	switch kind {
	case "models":
		ms := llm.Models()
		out := make([]string, 0, len(ms))
		for _, m := range ms {
			out = append(out, m.ID)
		}
		return out
	case "providers":
		return []string{"mantle", "converse", "anthropic", "codex", "grok", "glm", "llama"}
	}
	return nil
}

// Config returns the editable config fields with current values + options.
func (b *Bridge) Config() (*ConfigDTO, error) {
	c := config.Load()
	fields := config.Fields()
	out := make([]ConfigFieldDTO, 0, len(fields))
	for _, f := range fields {
		// Secret fields (e.g. telegram_token) stay file-only: the form never
		// surfaces a credential, even the masked "set" placeholder.
		if f.Secret {
			continue
		}
		opts := f.Options
		if f.Dynamic != "" {
			opts = dynamicOptions(f.Dynamic)
		}
		out = append(out, ConfigFieldDTO{
			Key:        f.Key,
			Desc:       f.Desc,
			Value:      config.Get(c, f.Key),
			Options:    opts,
			Multi:      f.Multi,
			AllowEmpty: emptyMeaningful[f.Key],
		})
	}
	return &ConfigDTO{Fields: out, Path: config.Path()}, nil
}

// SetConfig validates + persists a single config key. Returns the value as
// stored (config.Get may normalize it, e.g. a model ref).
func (b *Bridge) SetConfig(key, value string) (string, error) {
	c := config.Load()
	if err := config.Set(&c, key, value); err != nil {
		return "", err
	}
	if err := config.Save(c); err != nil {
		return "", err
	}
	return config.Get(c, key), nil
}

// ── per-role fallback chains ────────────────────────────────────────────────
// The GUI's per-rule chain editor: one ordered list of model names per role
// (primary, explore, research, general, code, dreamer, judge). Each falls
// through to the next on a quota/billing failure until one answers, or the whole
// chain is exhausted ("we're down"). Custom roles override the built-in default.

// RuleChainDTO is one role's fallback chain for the editor.
type RuleChainDTO struct {
	Role   string   `json:"role"`   // "primary" | "explore" | … | "judge"
	Desc   string   `json:"desc"`   // what this role drives
	Chain  []string `json:"chain"`  // ordered model names (friendly shorthands ok)
	Custom bool     `json:"custom"` // true = user-configured; false = built-in default
}

// RuleChainsDTO is the full per-role chain snapshot for the editor.
type RuleChainsDTO struct {
	Roles  []RuleChainDTO `json:"roles"`
	Models []string       `json:"models"` // model names the picker offers (shorthands + catalog ids)
}

// ruleRoleDesc explains what each role drives (shown under its switcher row).
var ruleRoleDesc = map[string]string{
	"primary":  "the main chat/orchestrator model — your chosen model stays first; the chain is its quota failover",
	"explore":  "locate-code subagents — fast + cheap, read-only",
	"research": "deep-investigation subagents — wants a strong reasoner",
	"general":  "catch-all delegated subagents",
	"code":     "code-writing subagents — correctness matters",
	"dreamer":  "background dreaming/consolidation — kept OFF the cheap GLM quota real tasks want",
	"judge":    "goal/claim judging — a cheap-but-valid, independent assessor",
}

// RuleChains returns every role's current fallback chain (configured or the
// built-in default) plus the model names the picker can offer.
func (b *Bridge) RuleChains() (*RuleChainsDTO, error) {
	c := config.Load()
	roles := make([]RuleChainDTO, 0, len(config.RuleRoles))
	for _, role := range config.RuleRoles {
		_, custom := c.RuleChains[role]
		roles = append(roles, RuleChainDTO{
			Role:   role,
			Desc:   ruleRoleDesc[role],
			Chain:  c.ChainFor(role),
			Custom: custom,
		})
	}
	return &RuleChainsDTO{Roles: roles, Models: chainModelChoices()}, nil
}

// SetRuleChain persists one role's chain (ordered model names). An empty chain
// clears the role back to its built-in default. Returns the stored chain.
func (b *Bridge) SetRuleChain(role string, chain []string) ([]string, error) {
	c := config.Load()
	clean := make([]string, 0, len(chain))
	for _, m := range chain {
		if m = strings.TrimSpace(m); m != "" {
			clean = append(clean, m)
		}
	}
	config.SetRuleChain(&c, role, clean)
	if err := config.Save(c); err != nil {
		return nil, err
	}
	return c.ChainFor(role), nil
}

// chainModelChoices lists the model names the chain editor offers: the friendly
// shorthands the user's chain reads in, plus every catalog id, deduped.
func chainModelChoices() []string {
	out := append([]string{}, config.DefaultRuleChain...)
	seen := map[string]bool{}
	for _, m := range out {
		seen[m] = true
	}
	for _, m := range llm.Models() {
		if !seen[m.ID] {
			seen[m.ID] = true
			out = append(out, m.ID)
		}
	}
	return out
}

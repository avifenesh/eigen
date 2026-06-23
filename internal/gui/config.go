package gui

import (
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

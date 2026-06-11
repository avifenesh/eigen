package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is eigen's optional JSON config (~/.eigen/config.json). Every field is
// optional; flags and environment variables override it. It supplies defaults
// so users don't repeat flags every run.
type Config struct {
	Provider   string   `json:"provider"`
	Model      string   `json:"model"`
	Perm       string   `json:"perm"`
	MaxTokens  int      `json:"max_tokens"`
	TTSCmd     string   `json:"tts_cmd"`
	NotifyCmd  string   `json:"notify_cmd"`
	JudgeModel string   `json:"judge_model"`
	SkillsDirs []string `json:"skills_dirs"`

	// Route enables the opt-in auto-router: per task, pick the cheapest model
	// that can do it well (ties → stronger → faster). RouteProviders is the
	// provider allowlist for CROSS-provider routing (canonical names, e.g.
	// "converse grok glm"); empty = route only within the current provider.
	Route          bool     `json:"route"`
	RouteProviders []string `json:"route_providers"`

	// Observe enables the structured activity log (~/.eigen/observe/events.jsonl,
	// metadata only) for long-term learning + debugging. Default on.
	Observe     *bool `json:"observe,omitempty"`
	DreamOnIdle bool  `json:"dream_on_idle"`
	IdleMinutes int   `json:"idle_minutes"`
}

// Load reads ~/.eigen/config.json. A missing or malformed file yields a zero
// Config (never an error) — config is best-effort and must not block startup.
func Load() Config {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}
	}
	return LoadFrom(filepath.Join(home, ".eigen", "config.json"))
}

// LoadFrom reads a config from an explicit path (used by tests).
func LoadFrom(path string) Config {
	var c Config
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(data, &c)
	return c
}

// Path returns the canonical config file location (~/.eigen/config.json).
func Path() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".eigen", "config.json")
}

// Save writes the config to the canonical path, creating ~/.eigen if needed.
func Save(c Config) error {
	p := Path()
	if p == "" {
		return os.ErrNotExist
	}
	return SaveTo(p, c)
}

// SaveTo writes the config to an explicit path (used by tests).
func SaveTo(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Set applies a named key to the config, parsing the value as that key's type.
// Returns an error naming valid keys when the key is unknown or the value
// malformed. Keys match the JSON field names.
func Set(c *Config, key, value string) error {
	switch key {
	case "provider":
		c.Provider = value
	case "model":
		c.Model = value
	case "perm":
		if value != "gated" && value != "auto" {
			return fmt.Errorf("perm must be gated|auto")
		}
		c.Perm = value
	case "max_tokens":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return fmt.Errorf("max_tokens must be a non-negative integer")
		}
		c.MaxTokens = n
	case "tts_cmd":
		c.TTSCmd = value
	case "notify_cmd":
		c.NotifyCmd = value
	case "judge_model":
		c.JudgeModel = value
	case "dream_on_idle":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("dream_on_idle must be true|false")
		}
		c.DreamOnIdle = b
	case "idle_minutes":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return fmt.Errorf("idle_minutes must be a non-negative integer")
		}
		c.IdleMinutes = n
	case "route":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("route must be true|false")
		}
		c.Route = b
	case "route_providers":
		c.RouteProviders = splitFields(value)
	case "observe":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("observe must be true|false")
		}
		c.Observe = &b
	default:
		return fmt.Errorf("unknown key %q (valid: %s)", key, strings.Join(Keys(), " "))
	}
	return nil
}

// Keys lists the /config-settable keys (skills_dirs stays file-only: a list),
// derived from Fields() so order and membership have one source of truth.
func Keys() []string {
	fs := Fields()
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Key
	}
	return out
}

// splitFields splits a space/comma-separated value into non-empty fields.
func splitFields(s string) []string {
	f := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' })
	if len(f) == 0 {
		return nil
	}
	return f
}

// View renders the config as aligned "key = value" lines, marking zero values.
func View(c Config) string {
	val := func(s string) string {
		if s == "" {
			return "(unset)"
		}
		return s
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-14s = %s\n", "provider", val(c.Provider))
	fmt.Fprintf(&b, "%-14s = %s\n", "model", val(c.Model))
	fmt.Fprintf(&b, "%-14s = %s\n", "perm", val(c.Perm))
	fmt.Fprintf(&b, "%-14s = %d\n", "max_tokens", c.MaxTokens)
	fmt.Fprintf(&b, "%-14s = %s\n", "tts_cmd", val(c.TTSCmd))
	fmt.Fprintf(&b, "%-14s = %s\n", "notify_cmd", val(c.NotifyCmd))
	fmt.Fprintf(&b, "%-14s = %s\n", "judge_model", val(c.JudgeModel))
	fmt.Fprintf(&b, "%-14s = %t\n", "dream_on_idle", c.DreamOnIdle)
	fmt.Fprintf(&b, "%-14s = %d\n", "idle_minutes", c.IdleMinutes)
	fmt.Fprintf(&b, "%-14s = %t\n", "route", c.Route)
	rp := "(current provider only)"
	if len(c.RouteProviders) > 0 {
		rp = strings.Join(c.RouteProviders, " ")
	}
	fmt.Fprintf(&b, "%-14s = %s\n", "route_providers", rp)
	fmt.Fprintf(&b, "%-14s = %t\n", "observe", c.ObserveEnabled())
	if len(c.SkillsDirs) > 0 {
		fmt.Fprintf(&b, "%-14s = %s (file-only)\n", "skills_dirs", strings.Join(c.SkillsDirs, ":"))
	}
	return strings.TrimRight(b.String(), "\n")
}

// ObserveEnabled reports whether the activity log is on (default true when
// unset).
func (c Config) ObserveEnabled() bool {
	return c.Observe == nil || *c.Observe
}

// Get returns the current value of a settable key, formatted as Set accepts.
func Get(c Config, key string) string {
	switch key {
	case "provider":
		return c.Provider
	case "model":
		return c.Model
	case "perm":
		return c.Perm
	case "max_tokens":
		return strconv.Itoa(c.MaxTokens)
	case "tts_cmd":
		return c.TTSCmd
	case "notify_cmd":
		return c.NotifyCmd
	case "judge_model":
		return c.JudgeModel
	case "dream_on_idle":
		return strconv.FormatBool(c.DreamOnIdle)
	case "idle_minutes":
		return strconv.Itoa(c.IdleMinutes)
	case "route":
		return strconv.FormatBool(c.Route)
	case "route_providers":
		return strings.Join(c.RouteProviders, " ")
	case "observe":
		return strconv.FormatBool(c.ObserveEnabled())
	}
	return ""
}

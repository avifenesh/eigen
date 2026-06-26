package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/theme"
)

// knownTheme reports whether name is a registered palette.
func knownTheme(name string) bool {
	for _, n := range theme.PaletteNames() {
		if n == name {
			return true
		}
	}
	return false
}

// Config is eigen's optional JSON config (~/.eigen/config.json). Every field is
// optional; flags and environment variables override it. It supplies defaults
// so users don't repeat flags every run.
type Config struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Perm     string `json:"perm"`
	// InputMode is what Enter does while a turn is running: "steer" injects the
	// message mid-turn (between tool rounds); "queue" holds it until the turn
	// ends, then sends it as the next turn. Default steer. Per-press override
	// in the TUI (alt+q) + /steer //queue.
	InputMode  string `json:"input_mode,omitempty"`
	Effort     string `json:"effort"`    // default reasoning effort for new sessions (per-model levels; e.g. max)
	Theme      string `json:"theme"`     // named color palette (nord|gruvbox); applied at startup via EIGEN_THEME
	NerdFont   string `json:"nerd_font"` // on|off icon tier (Nerd Font glyphs vs Unicode fallback); applied via EIGEN_NERD_FONT
	MaxTokens  int    `json:"max_tokens"`
	TTSCmd     string `json:"tts_cmd"`
	NotifyCmd  string `json:"notify_cmd"`
	JudgeModel string `json:"judge_model"`
	// DreamModel pins the model for background dreaming/consolidation. Default
	// (empty) = the sonnet-first dream ladder, deliberately OFF the cheap GLM
	// quota real tasks want. Env EIGEN_DREAM_MODEL overrides.
	DreamModel string `json:"dream_model,omitempty"`
	// TelegramToken is the bot token (from @BotFather) for the `eigen telegram`
	// phone bridge; TelegramAllow is the chat-id allowlist (only these chats are
	// served — fail-closed). Env EIGEN_TELEGRAM_TOKEN / EIGEN_TELEGRAM_ALLOW.
	TelegramToken string   `json:"telegram_token,omitempty"`
	TelegramAllow []int64  `json:"telegram_allow,omitempty"`
	SkillsDirs    []string `json:"skills_dirs"`

	// Route enables the opt-in model-assessed router for delegated subtasks: a
	// small model assesses missing kind/difficulty, then the user's tier chain
	// picks the cheapest capable model (ties → stronger → faster). The
	// main/orchestrator model stays explicit. RouteProviders restricts cross-
	// provider routing; empty = all credentialed providers.
	Route          bool     `json:"route"`
	RouteProviders []string `json:"route_providers"`

	// BoardGitHubOwners are the GitHub owners (your login + orgs like "agent-sh")
	// whose repos appear as lanes on the cross-project work board. Empty = the
	// board auto-detects (the gh-authenticated user + their orgs). Set to pin or
	// restrict the set. Env EIGEN_BOARD_GH_OWNERS (comma-separated) overrides.
	BoardGitHubOwners []string `json:"board_github_owners,omitempty"`

	// BoardPinned are lanes the user pinned to the board so they always show even
	// when idle (no open work / clean tree). A local lane is pinned by its
	// directory path; a remote lane by its "owner/name". Toggled from the board.
	BoardPinned []string `json:"board_pinned,omitempty"`

	// ObsidianVault pins the Obsidian vault directory eigen reads/writes notes
	// in. Empty = auto-detect (env EIGEN_OBSIDIAN_VAULT → ~/revuto → ~/Obsidian).
	// Set it (from the Connectors Obsidian card) to use ANY vault you like.
	ObsidianVault string `json:"obsidian_vault,omitempty"`

	// Observe enables the structured activity log (~/.eigen/observe/events.jsonl,
	// metadata only) for long-term learning + debugging. Default on.
	Observe     *bool `json:"observe,omitempty"`

	// RuleChains is the per-ROLE model fallback chain: an ordered list of model
	// names (friendly shorthands ok — opus/glm/composer/…) tried in turn, each
	// falling through to the next on a quota/billing failure until one answers.
	// Keys are roles: "primary", "subagent", "explore", "research", "general",
	// "code", "dreamer", "judge". A missing/empty role falls back to "default".
	// Empty map → built-in DefaultRuleChain. Edited in the GUI (per-rule switcher)
	// and persisted here. Env EIGEN_CHAIN_<ROLE> (comma-separated) overrides one.
	RuleChains map[string][]string `json:"rule_chains,omitempty"`

	DreamOnIdle bool `json:"dream_on_idle"`
	// DreamBatch routes dream Stage1 through the provider's async BATCH API
	// (~50% input discount) when the dream model's provider supports it
	// (Anthropic Messages Batches today). Off by default — the batch path is
	// async (results land on a later wake) and only pays on the heavy nightly
	// run, so it's opt-in. Env EIGEN_DREAM_BATCH=1 also enables it.
	DreamBatch  bool `json:"dream_batch,omitempty"`
	IdleMinutes int  `json:"idle_minutes"`

	// FrontWindowMin / StallIdleMin tune the subtask lifecycle: a foreground
	// subtask runs inline for FrontWindowMin minutes before it's promoted to
	// the background (if still active); a (sub)agent with no tool call for
	// StallIdleMin minutes is considered hung and stopped. 0 = built-in default
	// (2 min each).
	FrontWindowMin int `json:"front_window_min,omitempty"`
	StallIdleMin   int `json:"stall_idle_min,omitempty"`

	// LocalBackground routes BACKGROUND chores (session titling, dreaming,
	// compaction summaries, skill scans, feed suggestions) to a LOCAL model
	// (EIGEN_LLAMA_BASE_URL) when it's up AND ready to serve — saving the
	// frontier/Bedrock budget. OPT-IN (default off): a local server's quality
	// varies and it may be busy loading another model, so the user enables it
	// deliberately. When off, or when the local model isn't ready, background
	// work falls back to the usual small model (grok/haiku).
	LocalBackground bool `json:"local_background"`

	// DaemonTimeout overrides the client→daemon per-request timeout, in SECONDS
	// (0 = default 30s). Raise it when slow links or slow provider/model
	// construction make a model switch or other op exceed the default and
	// "fail" with a phantom timeout while the daemon is still working. Heavy ops
	// (/compact, /model) already get longer floors; this raises the base and
	// those floors with it. Exported to the client as EIGEN_DAEMON_TIMEOUT.
	DaemonTimeout int `json:"daemon_timeout,omitempty"`
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
	// A hand-edited ref in model ("mantle:us.openai.gpt-5.5") splits into the
	// shadow provider field — ONE user-facing field, provider is metadata.
	if tag, id := llm.ParseRef(c.Model); tag != "" {
		c.Provider, c.Model = tag, id
	}
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
		// Back-compat: still settable, but the canonical way is a model ref
		// ("mantle:us.openai.gpt-5.5") — provider is derived metadata now.
		c.Provider = value
	case "model":
		// ONE field names both. Keep the shadow provider field honest:
		//  - explicit "provider:" tag → that provider
		//  - untagged id the catalog knows → the catalog's provider
		//  - untagged unknown id → leave provider (the only case it carries info)
		if tag, id := llm.ParseRef(value); tag != "" {
			c.Provider, c.Model = tag, id
		} else {
			c.Model = value
			if info, ok := llm.Lookup(value); ok && info.Provider != "" {
				c.Provider = info.Provider
			}
		}
	case "perm":
		if value != "gated" && value != "auto" {
			return fmt.Errorf("perm must be gated|auto")
		}
		c.Perm = value
	case "input_mode":
		if value != "steer" && value != "queue" {
			return fmt.Errorf("input_mode must be steer|queue")
		}
		c.InputMode = value
	case "effort":
		// Closed set (Fields() declares the options; the GUI renders a <select>).
		// Enforce it backend-side like every other closed field; "" means unset.
		opts := FieldFor("effort").Options
		if value != "" && !slices.Contains(opts, value) {
			return fmt.Errorf("effort must be one of %s", strings.Join(opts, "|"))
		}
		c.Effort = value
	case "theme":
		if value != "" && !knownTheme(value) {
			return fmt.Errorf("theme must be one of %s", strings.Join(theme.PaletteNames(), "|"))
		}
		c.Theme = value
	case "nerd_font":
		if value != "" && value != "on" && value != "off" {
			return fmt.Errorf("nerd_font must be on|off")
		}
		c.NerdFont = value
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
	case "telegram_token":
		c.TelegramToken = value
	case "judge_model":
		c.JudgeModel = value
	case "dream_model":
		c.DreamModel = value
	case "dream_on_idle":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("dream_on_idle must be true|false")
		}
		c.DreamOnIdle = b
	case "dream_batch":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("dream_batch must be true|false")
		}
		c.DreamBatch = b
	case "idle_minutes":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return fmt.Errorf("idle_minutes must be a non-negative integer")
		}
		c.IdleMinutes = n
	case "front_window_min":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return fmt.Errorf("front_window_min must be a non-negative integer (minutes; 0 = default)")
		}
		c.FrontWindowMin = n
	case "stall_idle_min":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return fmt.Errorf("stall_idle_min must be a non-negative integer (minutes; 0 = default)")
		}
		c.StallIdleMin = n
	case "route":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("route must be true|false")
		}
		c.Route = b
	case "route_providers":
		c.RouteProviders = splitFields(value)
	case "local_background":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("local_background must be true|false")
		}
		c.LocalBackground = b
	case "daemon_timeout":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return fmt.Errorf("daemon_timeout must be a non-negative integer (seconds; 0 = default 30s)")
		}
		c.DaemonTimeout = n
	case "observe":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("observe must be true|false")
		}
		c.Observe = &b
	default:
		// rule_chains.<role> — set one role's fallback chain (comma/space list of
		// model names). The map is otherwise edited via the GUI bridge; this is the
		// CLI escape hatch. Empty value clears the role (reverts to its default).
		if role, ok := strings.CutPrefix(key, "rule_chains."); ok {
			role = strings.TrimSpace(role)
			if role == "" {
				return fmt.Errorf("rule_chains key needs a role: rule_chains.<%s>", strings.Join(RuleRoles, "|"))
			}
			SetRuleChain(c, role, splitChain(value))
			return nil
		}
		return fmt.Errorf("unknown key %q (valid: %s)", key, strings.Join(Keys(), " "))
	}
	return nil
}

// SetRuleChain sets (or, with an empty chain, clears) one role's fallback chain.
// Clearing reverts the role to its built-in default (DefaultRoleChains /
// DefaultRuleChain) at resolution time. Lazily allocates the map.
func SetRuleChain(c *Config, role string, chain []string) {
	role = strings.TrimSpace(role)
	if role == "" {
		return
	}
	if len(chain) == 0 {
		delete(c.RuleChains, role)
		if len(c.RuleChains) == 0 {
			c.RuleChains = nil
		}
		return
	}
	if c.RuleChains == nil {
		c.RuleChains = map[string][]string{}
	}
	c.RuleChains[role] = chain
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
	// ONE field names both: model renders as a ref (tagged only when the id
	// doesn't self-tag via the catalog). provider is shadow metadata — shown
	// only when model is unset and it alone carries the default.
	fmt.Fprintf(&b, "%-14s = %s\n", "model", val(Get(c, "model")))
	if c.Model == "" && c.Provider != "" {
		fmt.Fprintf(&b, "%-14s = %s (provider default; set a model to supersede)\n", "provider", c.Provider)
	}
	fmt.Fprintf(&b, "%-14s = %s\n", "perm", val(c.Perm))
	fmt.Fprintf(&b, "%-14s = %s\n", "input_mode", val(Get(c, "input_mode")))
	fmt.Fprintf(&b, "%-14s = %s\n", "effort", val(c.Effort))
	fmt.Fprintf(&b, "%-14s = %s\n", "theme", val(c.Theme))
	fmt.Fprintf(&b, "%-14s = %s\n", "nerd_font", val(c.NerdFont))
	fmt.Fprintf(&b, "%-14s = %d\n", "max_tokens", c.MaxTokens)
	fmt.Fprintf(&b, "%-14s = %s\n", "tts_cmd", val(c.TTSCmd))
	fmt.Fprintf(&b, "%-14s = %s\n", "notify_cmd", val(c.NotifyCmd))
	fmt.Fprintf(&b, "%-14s = %s\n", "telegram_token", val(Get(c, "telegram_token")))
	fmt.Fprintf(&b, "%-14s = %s\n", "judge_model", val(c.JudgeModel))
	fmt.Fprintf(&b, "%-14s = %s\n", "dream_model", val(c.DreamModel))
	fmt.Fprintf(&b, "%-14s = %t\n", "dream_on_idle", c.DreamOnIdle)
	fmt.Fprintf(&b, "%-14s = %t\n", "dream_batch", c.DreamBatch)
	fmt.Fprintf(&b, "%-14s = %d\n", "idle_minutes", c.IdleMinutes)
	fmt.Fprintf(&b, "%-14s = %d\n", "front_window_min", c.FrontWindowMin)
	fmt.Fprintf(&b, "%-14s = %d\n", "stall_idle_min", c.StallIdleMin)
	fmt.Fprintf(&b, "%-14s = %t\n", "route", c.Route)
	rp := "(all credentialed providers for delegated subtasks)"
	if len(c.RouteProviders) > 0 {
		rp = strings.Join(c.RouteProviders, " ")
	}
	fmt.Fprintf(&b, "%-14s = %s\n", "route_providers", rp)
	fmt.Fprintf(&b, "%-14s = %t\n", "observe", c.ObserveEnabled())
	fmt.Fprintf(&b, "%-16s = %t\n", "local_background", c.LocalBackground)
	fmt.Fprintf(&b, "%-16s = %d\n", "daemon_timeout", c.DaemonTimeout)
	if len(c.SkillsDirs) > 0 {
		fmt.Fprintf(&b, "%-14s = %s (file-only)\n", "skills_dirs", strings.Join(c.SkillsDirs, ":"))
	}
	// Per-role fallback chains: only the roles the user customized (the rest use
	// built-in defaults). Set via rule_chains.<role> or the GUI per-rule editor.
	for _, role := range RuleRoles {
		if ch, ok := c.RuleChains[role]; ok && len(ch) > 0 {
			fmt.Fprintf(&b, "%-14s = %s\n", "rule_chains."+role, strings.Join(ch, ","))
		}
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
		// Render the one-field form: bare id when it self-tags, provider:id
		// when the provider carries information the id alone wouldn't.
		return llm.Ref(c.Provider, c.Model)
	case "perm":
		return c.Perm
	case "input_mode":
		if c.InputMode == "" {
			return "steer"
		}
		return c.InputMode
	case "effort":
		return c.Effort
	case "theme":
		return c.Theme
	case "nerd_font":
		return c.NerdFont
	case "max_tokens":
		return strconv.Itoa(c.MaxTokens)
	case "tts_cmd":
		return c.TTSCmd
	case "notify_cmd":
		return c.NotifyCmd
	case "telegram_token":
		if c.TelegramToken != "" {
			return "set"
		}
		return ""
	case "judge_model":
		return c.JudgeModel
	case "dream_model":
		return c.DreamModel
	case "dream_on_idle":
		return strconv.FormatBool(c.DreamOnIdle)
	case "dream_batch":
		return strconv.FormatBool(c.DreamBatch)
	case "idle_minutes":
		return strconv.Itoa(c.IdleMinutes)
	case "front_window_min":
		return strconv.Itoa(c.FrontWindowMin)
	case "stall_idle_min":
		return strconv.Itoa(c.StallIdleMin)
	case "route":
		return strconv.FormatBool(c.Route)
	case "route_providers":
		return strings.Join(c.RouteProviders, " ")
	case "observe":
		return strconv.FormatBool(c.ObserveEnabled())
	case "local_background":
		return strconv.FormatBool(c.LocalBackground)
	case "daemon_timeout":
		return strconv.Itoa(c.DaemonTimeout)
	}
	if role, ok := strings.CutPrefix(key, "rule_chains."); ok {
		return strings.Join(c.ChainFor(strings.TrimSpace(role)), ",")
	}
	return ""
}

// DefaultRuleChain is the built-in model fallback chain used for any role the
// user hasn't customized. Friendly names; resolved + credential-filtered at use
// (llm.NewChain). Ordered strongest→last-resort: if the whole chain is dead,
// the request genuinely fails.
var DefaultRuleChain = []string{
	"opus", "gpt-5.5", "glm", "sonnet", "gpt-5.4", "opus-4.7", "glm-5.1", "composer", "glm-5", "grok",
}

// RuleRoles are the configurable per-role chain keys (the GUI's switcher rows).
var RuleRoles = []string{"primary", "explore", "research", "general", "code", "dreamer", "judge"}

// DefaultRoleChains are the built-in per-role chains, encoding the capability !=
// price intuitions (these mirror the SubagentModel ladders so an unconfigured
// install behaves the same, but now as full fallback chains): explore wants
// fast+cheap, research wants strong, code wants strong+fast, judge wants a
// cheap-but-valid assessor (gpt/glm/haiku — never the top default), dreamer
// stays sonnet-first OFF the GLM quota real tasks want. A role absent here uses
// DefaultRuleChain (opus-first, full failover). Every chain still falls through
// to extra links so it degrades instead of failing on a single drained account.
var DefaultRoleChains = map[string][]string{
	"explore":  {"composer", "glm", "haiku", "sonnet", "opus"},
	"research": {"glm", "opus", "gpt-5.5", "sonnet", "grok"},
	"code":     {"opus", "composer", "glm", "gpt-5.5", "sonnet"},
	"general":  {"opus", "glm", "gpt-5.5", "sonnet", "composer"},
	"judge":    {"gpt-5.4", "glm", "haiku", "gpt-5.5", "sonnet"},
	"dreamer":  {"sonnet", "haiku", "gpt-5.4", "glm"},
}

// ChainFor returns the model chain for a role: env override
// (EIGEN_CHAIN_<ROLE>) → the role's configured chain → the "default" configured
// chain → the role's built-in default → DefaultRuleChain. Never empty.
func (c Config) ChainFor(role string) []string {
	if env := strings.TrimSpace(os.Getenv("EIGEN_CHAIN_" + strings.ToUpper(role))); env != "" {
		return splitChain(env)
	}
	if c.RuleChains != nil {
		if ch, ok := c.RuleChains[role]; ok && len(ch) > 0 {
			return ch
		}
		if ch, ok := c.RuleChains["default"]; ok && len(ch) > 0 {
			return ch
		}
	}
	if ch, ok := DefaultRoleChains[role]; ok {
		return ch
	}
	return DefaultRuleChain
}

// splitChain parses a comma/space separated chain string into model names.
func splitChain(s string) []string {
	f := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	out := make([]string, 0, len(f))
	for _, x := range f {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

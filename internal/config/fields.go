package config

// Field describes one settable config key for UIs: what it means, and what
// values it accepts. Options (static or dynamic) mark a CLOSED set — pickers
// cycle/choose instead of free text; free-text fields have neither.
type Field struct {
	Key  string
	Desc string
	// Options is a static closed set of valid values ("" = not closed).
	Options []string
	// Dynamic names an option set the UI resolves at render time, because it
	// depends on the catalog/credentials: "providers" | "models".
	Dynamic string
	// Multi marks a space-separated multi-select over the option set
	// (route_providers). Pickers toggle membership instead of replacing.
	Multi bool
	// Secret marks a free-text field holding a credential: Get masks it (returns
	// "set"/""), and surfaces that don't want to display secrets (e.g. the GUI
	// config form) skip it. It still exists so /config <key> describe agrees the
	// key is settable rather than reporting "unknown key".
	Secret bool
}

// Fields lists the settable keys with their semantics, in display order.
// Keys() derives from this — one source of truth.
func Fields() []Field {
	return []Field{
		{Key: "model", Desc: "default model for new sessions — catalog ids self-tag their backend; force one with provider:id (e.g. mantle:openai.gpt-5.5, converse:us.anthropic.claude-opus-4-8); live switch: /model", Dynamic: "models"},
		{Key: "perm", Desc: "tool permission: gated asks before mutating tools · auto runs them freely", Options: []string{"gated", "auto"}},
		{Key: "input_mode", Desc: "what Enter does while a turn runs: steer injects mid-turn (between tool rounds) · queue holds it for the next turn", Options: []string{"steer", "queue"}},
		{Key: "effort", Desc: "default reasoning effort for new sessions (per-model levels; e.g. opus max, gpt xhigh); live switch: /effort or ctrl+e", Options: []string{"off", "minimal", "none", "low", "medium", "high", "xhigh", "max"}},
		{Key: "theme", Desc: "color palette (applied at startup): studio (daylight neutral) · deepteal (default dark) · nord (calm blue) · gruvbox (warm); same roles, different hues", Options: []string{"studio", "deepteal", "nord", "gruvbox"}},
		{Key: "nerd_font", Desc: "icon tier (applied at startup): on uses richer Nerd Font glyphs (needs a Nerd Font, e.g. JetBrainsMono NF) · off uses the universal Unicode fallback · empty auto-detects (defaults to off — no tofu)", Options: []string{"on", "off"}},
		{Key: "max_tokens", Desc: "context-budget ceiling in tokens; 0 = auto (85% of the model window)"},
		{Key: "tts_cmd", Desc: "text-to-speech command for /read and voice mode, e.g. espeak-ng or readd — the text is passed as the last argument"},
		{Key: "notify_cmd", Desc: "notifier run on pings (approval needed, long turn done), e.g. notify-send — the message is passed as the last argument; empty = terminal bell only"},
		{Key: "telegram_token", Desc: "bot token (from @BotFather) for the `eigen telegram` phone bridge — pair with the telegram_allow chat-id allowlist in config.json; shown as `set` once stored (never echoed back)", Secret: true},
		{Key: "judge_model", Desc: "pin the goal_achieved judge to a specific model — ANY provider works; empty = automatic cross-vendor judge (GPT judges Claude and vice versa)", Dynamic: "models"},
		{Key: "dream_model", Desc: "pin the background dreaming/consolidation model; empty = sonnet-first dream ladder (kept OFF the cheap GLM quota real tasks want)", Dynamic: "models"},
		{Key: "dream_on_idle", Desc: "reflect recent sessions into memory when idle", Options: []string{"true", "false"}},
		{Key: "dream_batch", Desc: "route dream Stage1 through the provider's async batch API (~50% cheaper; Anthropic only today) — results land on a later wake; off by default", Options: []string{"true", "false"}},
		{Key: "idle_minutes", Desc: "minutes of idle before dreaming kicks in (default 5)"},
		{Key: "front_window_min", Desc: "minutes a subtask runs in the foreground before it's promoted to the background (default 2; 0 = default)"},
		{Key: "stall_idle_min", Desc: "minutes a subagent may go with no tool call before it's considered hung and stopped (default 2; 0 = default)"},
		{Key: "route", Desc: "prompt router for delegated subtasks; route_model/EIGEN_ROUTE_MODEL can use a local model to choose a candidate; the main model stays explicit (/route toggles live)", Options: []string{"true", "false"}},
		{Key: "route_providers", Desc: "providers subtask routing may roam across (space-separated); empty = all credentialed providers", Dynamic: "providers", Multi: true},
		{Key: "route_model", Desc: "small LOCAL prompt-router model. Empty = use EIGEN_ROUTE_MODEL/EIGEN_ROUTER_MODEL when set, else the legacy small candidate model; untagged values use the llama backend (EIGEN_LLAMA_BASE_URL required), provider:model can target a custom local provider"},
		{Key: "observe", Desc: "structured activity log at ~/.eigen/observe (metadata only — no content)", Options: []string{"true", "false"}},
		{Key: "local_background", Desc: "route background chores (titling, dreaming, compaction, scans) to a LOCAL model (EIGEN_LLAMA_BASE_URL) when it's up AND ready — saves frontier budget; opt-in, falls back when the local model is busy/absent", Options: []string{"true", "false"}},
		{Key: "daemon_timeout", Desc: "client→daemon per-request timeout in SECONDS (0 = default 30s). Raise it if a model switch or other op stalls and fails with a phantom timeout on a slow link / slow provider build. Heavy ops (/compact, /model) keep longer floors and rise with this."},
	}
}

// FieldFor returns the field metadata for a key (zero Field when unknown).
func FieldFor(key string) Field {
	for _, f := range Fields() {
		if f.Key == key {
			return f
		}
	}
	return Field{}
}

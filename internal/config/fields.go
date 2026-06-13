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
}

// Fields lists the settable keys with their semantics, in display order.
// Keys() derives from this — one source of truth.
func Fields() []Field {
	return []Field{
		{Key: "model", Desc: "default model for new sessions — catalog ids self-tag their backend; force one with provider:id (e.g. mantle:us.openai.gpt-5.5, ant:claude-fable-5); live switch: /model", Dynamic: "models"},
		{Key: "perm", Desc: "tool permission: gated asks before mutating tools · auto runs them freely", Options: []string{"gated", "auto"}},
		{Key: "effort", Desc: "default reasoning effort for new sessions (per-model levels; e.g. opus max, gpt xhigh); live switch: /effort or ctrl+e", Options: []string{"off", "minimal", "none", "low", "medium", "high", "xhigh", "max"}},
		{Key: "max_tokens", Desc: "context-budget ceiling in tokens; 0 = auto (85% of the model window)"},
		{Key: "tts_cmd", Desc: "text-to-speech command for /read and voice mode, e.g. espeak-ng or readd — the text is passed as the last argument"},
		{Key: "notify_cmd", Desc: "notifier run on pings (approval needed, long turn done), e.g. notify-send — the message is passed as the last argument; empty = terminal bell only"},
		{Key: "judge_model", Desc: "pin the goal_achieved judge to a specific model — ANY provider works; empty = automatic cross-vendor judge (GPT judges Claude and vice versa)", Dynamic: "models"},
		{Key: "dream_on_idle", Desc: "reflect recent sessions into memory when idle", Options: []string{"true", "false"}},
		{Key: "idle_minutes", Desc: "minutes of idle before dreaming kicks in (default 5)"},
		{Key: "route", Desc: "auto-router: per task, pick the cheapest capable model (/route toggles live)", Options: []string{"true", "false"}},
		{Key: "route_providers", Desc: "providers the auto-router may roam across (space-separated); empty = stay on the current provider", Dynamic: "providers", Multi: true},
		{Key: "observe", Desc: "structured activity log at ~/.eigen/observe (metadata only — no content)", Options: []string{"true", "false"}},
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

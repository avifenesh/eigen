package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFromParses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte(`{
		"provider": "converse",
		"model": "claude-x",
		"perm": "auto",
		"max_tokens": 123456,
		"tts_cmd": "espeak-ng",
		"skills_dirs": ["/a", "/b"]
	}`), 0o644)

	c := LoadFrom(path)
	if c.Provider != "converse" || c.Model != "claude-x" || c.Perm != "auto" {
		t.Fatalf("scalars wrong: %+v", c)
	}
	if c.MaxTokens != 123456 || c.TTSCmd != "espeak-ng" {
		t.Fatalf("fields wrong: %+v", c)
	}
	if len(c.SkillsDirs) != 2 || c.SkillsDirs[0] != "/a" {
		t.Fatalf("skills_dirs wrong: %+v", c.SkillsDirs)
	}
}

func TestLoadFromMissingIsZero(t *testing.T) {
	c := LoadFrom(filepath.Join(t.TempDir(), "nope.json"))
	if c.Provider != "" || c.MaxTokens != 0 {
		t.Fatalf("missing config should be zero value, got %+v", c)
	}
}

func TestLoadFromMalformedIsZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte(`{not valid`), 0o644)
	c := LoadFrom(path)
	if c.Provider != "" {
		t.Fatal("malformed config must not crash and yields zero value")
	}
}

func TestSetAndSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")

	var c Config
	for _, kv := range [][2]string{
		{"provider", "converse"},
		{"model", "us.anthropic.claude-opus-4-8"},
		{"perm", "auto"},
		{"max_tokens", "500000"},
		{"notify_cmd", "notify-send"},
		{"judge_model", "claude-fable-5"},
		{"dream_on_idle", "true"},
		{"idle_minutes", "7"},
	} {
		if err := Set(&c, kv[0], kv[1]); err != nil {
			t.Fatalf("Set(%s): %v", kv[0], err)
		}
	}
	if err := SaveTo(p, c); err != nil {
		t.Fatal(err)
	}
	got := LoadFrom(p)
	if got.Provider != "converse" || got.MaxTokens != 500000 || !got.DreamOnIdle || got.IdleMinutes != 7 || got.JudgeModel != "claude-fable-5" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestSetValidation(t *testing.T) {
	var c Config
	if err := Set(&c, "perm", "yolo"); err == nil {
		t.Fatal("bad perm should error")
	}
	if err := Set(&c, "max_tokens", "lots"); err == nil {
		t.Fatal("non-integer max_tokens should error")
	}
	if err := Set(&c, "dream_on_idle", "sometimes"); err == nil {
		t.Fatal("non-bool dream_on_idle should error")
	}
	if err := Set(&c, "no_such_key", "x"); err == nil {
		t.Fatal("unknown key should error")
	}
}

// TestSetEffortClosedSet pins the GUI-101 contract: effort is a CLOSED option
// set (Fields() declares it, the GUI renders a <select>), so Set must reject
// values outside that set like every other closed field — "" stays valid for
// "unset", and every declared option round-trips.
func TestSetEffortClosedSet(t *testing.T) {
	var c Config
	// A value not in the option set is rejected and leaves Effort unchanged.
	if err := Set(&c, "effort", "ultra"); err == nil {
		t.Fatal("out-of-set effort should error")
	}
	if c.Effort != "" {
		t.Fatalf("rejected effort must not be stored, got %q", c.Effort)
	}
	// Empty clears (unset) and is allowed.
	if err := Set(&c, "effort", ""); err != nil {
		t.Fatalf("empty effort (unset) should be allowed: %v", err)
	}
	// Every option the GUI offers must be accepted (no drift between Set and Fields()).
	for _, opt := range FieldFor("effort").Options {
		if err := Set(&c, "effort", opt); err != nil {
			t.Fatalf("Set(effort, %q) should be accepted: %v", opt, err)
		}
		if c.Effort != opt {
			t.Fatalf("effort %q not stored, got %q", opt, c.Effort)
		}
	}
}

func TestViewRendersAllKeys(t *testing.T) {
	c := Config{Provider: "glm", MaxTokens: 1234}
	v := View(c)
	for _, k := range Keys() {
		if !strings.Contains(v, k) {
			t.Fatalf("View missing key %s:\n%s", k, v)
		}
	}
	if !strings.Contains(v, "(unset)") {
		t.Fatal("zero string values should show (unset)")
	}
}

func TestSetRouteKeys(t *testing.T) {
	var c Config
	if err := Set(&c, "route", "true"); err != nil || !c.Route {
		t.Fatalf("route=true: %v %v", err, c.Route)
	}
	if err := Set(&c, "route_providers", "converse grok glm"); err != nil {
		t.Fatal(err)
	}
	if len(c.RouteProviders) != 3 || c.RouteProviders[0] != "converse" {
		t.Fatalf("route_providers wrong: %v", c.RouteProviders)
	}
	// comma-separated also works.
	Set(&c, "route_providers", "converse,glm")
	if len(c.RouteProviders) != 2 {
		t.Fatalf("comma split wrong: %v", c.RouteProviders)
	}
	if err := Set(&c, "route", "maybe"); err == nil {
		t.Fatal("non-bool route should error")
	}
}

func TestSetModelRefSplitsProvider(t *testing.T) {
	var c Config
	if err := Set(&c, "model", "mantle:us.openai.gpt-5.5"); err != nil {
		t.Fatal(err)
	}
	if c.Provider != "mantle" || c.Model != "us.openai.gpt-5.5" {
		t.Fatalf("ref should split: %+v", c)
	}
	// Untagged catalog id: model set, provider derived from the catalog
	// (keeps the shadow field honest).
	if err := Set(&c, "model", "glm-5.1"); err != nil {
		t.Fatal(err)
	}
	if c.Model != "glm-5.1" || c.Provider != "glm" {
		t.Fatalf("untagged catalog id should derive provider: %+v", c)
	}
	// Get renders the one-field form (catalog ids bare).
	if got := Get(c, "model"); got != "glm-5.1" {
		t.Fatalf("Get(model) = %q", got)
	}
}

func TestTelegramTokenSetGetDescribeAgree(t *testing.T) {
	// Regression: Set + Get handled telegram_token but Fields()/Keys() omitted
	// it, so `/config telegram_token` (describe) reported "unknown key" even
	// though setting it worked. Set, Get, describe (FieldFor), and View must
	// all agree the key exists.
	var c Config
	if err := Set(&c, "telegram_token", "123:secret"); err != nil {
		t.Fatalf("Set(telegram_token): %v", err)
	}
	if c.TelegramToken != "123:secret" {
		t.Fatalf("token not stored: %q", c.TelegramToken)
	}

	// Describe path: FieldFor must return a real (non-zero) field.
	f := FieldFor("telegram_token")
	if f.Key != "telegram_token" {
		t.Fatalf("FieldFor(telegram_token) returned zero Field — describe would report unknown key")
	}
	if !f.Secret {
		t.Fatal("telegram_token must be marked Secret so it stays masked/file-only")
	}
	if len(f.Options) != 0 || f.Multi {
		t.Fatalf("telegram_token should be free-text, got %+v", f)
	}

	// Keys() derives from Fields(): the key must be listed.
	found := false
	for _, k := range Keys() {
		if k == "telegram_token" {
			found = true
		}
	}
	if !found {
		t.Fatal("Keys() must include telegram_token")
	}

	// Get masks the secret (never echoes the raw token).
	if got := Get(c, "telegram_token"); got != "set" {
		t.Fatalf("Get should mask a set token as %q, got %q", "set", got)
	}
	if got := Get(Config{}, "telegram_token"); got != "" {
		t.Fatalf("Get should report unset token as empty, got %q", got)
	}

	// View must render the key (so TestViewRendersAllKeys + describe stay aligned)
	// without leaking the raw secret.
	v := View(c)
	if !strings.Contains(v, "telegram_token") {
		t.Fatalf("View missing telegram_token:\n%s", v)
	}
	if strings.Contains(v, "123:secret") {
		t.Fatalf("View leaked the raw token:\n%s", v)
	}
}

func TestLoadFromNormalizesRef(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.json"
	os.WriteFile(path, []byte(`{"model":"ant:claude-opus-4-8"}`), 0o644)
	c := LoadFrom(path)
	if c.Provider != "ant" || c.Model != "claude-opus-4-8" {
		t.Fatalf("hand-edited ref should normalize: %+v", c)
	}
}

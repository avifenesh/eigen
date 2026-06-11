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

func TestLoadFromNormalizesRef(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.json"
	os.WriteFile(path, []byte(`{"model":"ant:claude-opus-4-1-20250805"}`), 0o644)
	c := LoadFrom(path)
	if c.Provider != "ant" || c.Model != "claude-opus-4-1-20250805" {
		t.Fatalf("hand-edited ref should normalize: %+v", c)
	}
}

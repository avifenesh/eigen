package config

import (
	"os"
	"path/filepath"
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

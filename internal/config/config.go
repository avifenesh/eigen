package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is eigen's optional JSON config (~/.eigen/config.json). Every field is
// optional; flags and environment variables override it. It supplies defaults
// so users don't repeat flags every run.
type Config struct {
	Provider    string   `json:"provider"`
	Model       string   `json:"model"`
	Perm        string   `json:"perm"`
	MaxTokens   int      `json:"max_tokens"`
	TTSCmd      string   `json:"tts_cmd"`
	SkillsDirs  []string `json:"skills_dirs"`
	DreamOnIdle bool     `json:"dream_on_idle"`
	IdleMinutes int      `json:"idle_minutes"`
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

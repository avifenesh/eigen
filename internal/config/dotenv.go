// Package config handles eigen's configuration loading. For now that is just
// .env credential files; settings.toml lands here as the settings system grows.
package config

import (
	"bufio"
	"os"
	"strings"
)

// LoadEnvFiles loads KEY=VALUE pairs from .env files into the process
// environment without overriding variables that are already set. Files are read
// in order, so an earlier file wins over a later one and the real environment
// wins over all. Lines may use an optional "export " prefix and quoted values.
func LoadEnvFiles(paths ...string) {
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimPrefix(line, "export ")
			key, val, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			val = strings.TrimSpace(val)
			if len(val) >= 2 {
				if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
					val = val[1 : len(val)-1]
				}
			}
			if key != "" {
				if _, exists := os.LookupEnv(key); !exists {
					os.Setenv(key, val)
				}
			}
		}
		f.Close()
	}
}

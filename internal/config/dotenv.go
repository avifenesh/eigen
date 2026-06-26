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
// wins over all. A missing file is not an error. Lines may use an optional
// "export " prefix and quoted values.
func LoadEnvFiles(paths ...string) error {
	for _, p := range paths {
		if err := loadOne(p); err != nil {
			return err
		}
	}
	return nil
}

func loadOne(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
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
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			// Quoted value: take it verbatim, comments inside the quotes belong to the value.
			val = val[1 : len(val)-1]
		} else {
			// Unquoted value: strip an inline " #..." comment, then trim the
			// trailing whitespace it leaves behind (e.g. KEY=sk-... # rotate monthly).
			val = strings.TrimRight(stripInlineComment(val), " \t")
		}
		if key != "" {
			if _, exists := os.LookupEnv(key); !exists {
				os.Setenv(key, val)
			}
		}
	}
	return scanner.Err()
}

// stripInlineComment returns val truncated at the first unescaped " #"
// (whitespace followed by a hash), dropping the comment. A "#" not preceded by
// whitespace, or one escaped with a preceding backslash, stays part of the
// value so tokens like "color=#fff" or escaped hashes survive.
func stripInlineComment(val string) string {
	for i := 1; i < len(val); i++ {
		if val[i] != '#' {
			continue
		}
		if val[i-1] != ' ' && val[i-1] != '\t' {
			continue
		}
		// Count preceding backslashes; an odd count escapes the hash.
		backslashes := 0
		for j := i - 2; j >= 0 && val[j] == '\\'; j-- {
			backslashes++
		}
		if backslashes%2 == 1 {
			continue
		}
		return val[:i]
	}
	return val
}

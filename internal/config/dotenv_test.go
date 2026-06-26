package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFiles(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, ".env")
	content := "# a comment\n" +
		"export FOO=bar\n" +
		"QUOTED=\"baz qux\"\n" +
		"SINGLE='abc'\n" +
		"EMPTY=\n" +
		"NO_EQUALS_LINE\n" +
		"PRESET=from-file\n" +
		"KEY=sk-secret123 # rotate monthly\n" +
		"TRAILING=value   \n" +
		"HASHVALUE=color#fff\n" +
		"QUOTEDHASH=\"keep # this\"\n" +
		"ESCAPEDHASH=keep \\#me # but drop this\n"
	if err := os.WriteFile(primary, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	managed := []string{"FOO", "QUOTED", "SINGLE", "EMPTY", "PRESET", "KEY", "TRAILING", "HASHVALUE", "QUOTEDHASH", "ESCAPEDHASH"}
	for _, k := range managed {
		os.Unsetenv(k)
	}
	os.Setenv("PRESET", "already-set")
	t.Cleanup(func() {
		for _, k := range managed {
			os.Unsetenv(k)
		}
	})

	if err := LoadEnvFiles(primary); err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"FOO":         "bar",
		"QUOTED":      "baz qux",
		"SINGLE":      "abc",
		"EMPTY":       "",
		"PRESET":      "already-set",  // never override an already-set var
		"KEY":         "sk-secret123", // inline comment + trailing space stripped
		"TRAILING":    "value",        // trailing whitespace stripped
		"HASHVALUE":   "color#fff",    // hash without leading space is kept
		"QUOTEDHASH":  "keep # this",  // quoted value kept verbatim
		"ESCAPEDHASH": "keep \\#me",   // escaped "#" kept, real inline comment dropped
	}
	for k, want := range cases {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

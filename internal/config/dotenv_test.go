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
		"PRESET=from-file\n"
	if err := os.WriteFile(primary, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"FOO", "QUOTED", "SINGLE", "EMPTY", "PRESET"} {
		os.Unsetenv(k)
	}
	os.Setenv("PRESET", "already-set")
	t.Cleanup(func() {
		for _, k := range []string{"FOO", "QUOTED", "SINGLE", "EMPTY", "PRESET"} {
			os.Unsetenv(k)
		}
	})

	LoadEnvFiles(primary)

	cases := map[string]string{
		"FOO":    "bar",
		"QUOTED": "baz qux",
		"SINGLE": "abc",
		"EMPTY":  "",
		"PRESET": "already-set", // never override an already-set var
	}
	for k, want := range cases {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

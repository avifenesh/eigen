package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writePlugin writes a plugins.json and an executable helper script, returning
// the json path.
func writePlugin(t *testing.T, dir, json string) string {
	t.Helper()
	p := filepath.Join(dir, "plugins.json")
	if err := os.WriteFile(p, []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadPluginsRunsCommand(t *testing.T) {
	dir := t.TempDir()
	// A script that uppercases its stdin via tr.
	spec := `[{"name":"shout","description":"uppercase","command":["tr","a-z","A-Z"],"readonly":true}]`
	path := writePlugin(t, dir, spec)

	defs, err := LoadPlugins(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Name != "shout" || !defs[0].ReadOnly {
		t.Fatalf("unexpected defs: %+v", defs)
	}
	out, err := defs[0].Run(context.Background(), json.RawMessage(`hello`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "HELLO" {
		t.Fatalf("plugin should pipe args to stdin and return stdout, got %q", out)
	}
}

func TestLoadPluginsMissingFileSkipped(t *testing.T) {
	defs, err := LoadPlugins(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should be skipped, got %v", err)
	}
	if len(defs) != 0 {
		t.Fatal("expected no defs")
	}
}

func TestLoadPluginsMalformedErrors(t *testing.T) {
	dir := t.TempDir()
	path := writePlugin(t, dir, `{not json`)
	if _, err := LoadPlugins(path); err == nil {
		t.Fatal("malformed plugins.json should error")
	}
}

func TestLoadPluginsRequiresNameAndCommand(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadPlugins(writePlugin(t, dir, `[{"description":"x","command":["true"]}]`)); err == nil {
		t.Fatal("missing name should error")
	}
	dir2 := t.TempDir()
	if _, err := LoadPlugins(writePlugin(t, dir2, `[{"name":"x"}]`)); err == nil {
		t.Fatal("missing command should error")
	}
}

func TestPluginPropagatesFailure(t *testing.T) {
	dir := t.TempDir()
	// `false` exits non-zero with no stdout.
	defs, err := LoadPlugins(writePlugin(t, dir, `[{"name":"boom","command":["false"]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := defs[0].Run(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("a non-zero exit should be an error")
	}
}

func TestPluginRegistersInRegistry(t *testing.T) {
	dir := t.TempDir()
	defs, _ := LoadPlugins(writePlugin(t, dir, `[{"name":"echo","command":["cat"]}]`))
	r, err := NewRegistry(append([]Definition{Read(NewPolicy(dir))}, defs...)...)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Get("echo"); !ok {
		t.Fatal("plugin tool should be registered")
	}
}

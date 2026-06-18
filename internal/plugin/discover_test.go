package plugin

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiscoverHooksPreservesMatcher(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "hooks", "hooks.json"), `{
		"hooks": {
			"PreToolUse": [{
				"matcher": "edit|write",
				"hooks": [{"type": "command", "command": "echo hi"}]
			}]
		}
	}`)

	hooks, err := discoverHooks(root, nil)
	if err != nil {
		t.Fatalf("discoverHooks: %v", err)
	}
	if len(hooks) != 1 {
		t.Fatalf("want 1 hook, got %d: %+v", len(hooks), hooks)
	}
	got := hooks[0]
	if got.Event != "tool_start" {
		t.Fatalf("event = %q, want tool_start", got.Event)
	}
	if got.Matcher != "edit|write" {
		t.Fatalf("matcher = %q, want edit|write", got.Matcher)
	}
	if want := []string{"echo", "hi"}; !reflect.DeepEqual(got.Command, want) {
		t.Fatalf("command = %v, want %v", got.Command, want)
	}
}

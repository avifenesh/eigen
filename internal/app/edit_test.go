package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/feed"
	"github.com/avifenesh/eigen/internal/memory"
	"github.com/avifenesh/eigen/internal/skill"
)

// --- toggleDisabled ----------------------------------------------------

func TestToggleDisabledMCP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	os.WriteFile(path, []byte(`{"servers":[{"name":"ws","command":["bin"],"tools":["a","b"],"env":{"K":"v"}}]}`), 0o644)

	on, err := toggleDisabled(path, "mcp", 0)
	if err != nil || on != true {
		t.Fatalf("toggle: on=%v err=%v", on, err)
	}
	var cfg map[string]any
	data, _ := os.ReadFile(path)
	json.Unmarshal(data, &cfg)
	entry := cfg["servers"].([]any)[0].(map[string]any)
	if entry["disabled"] != true {
		t.Fatalf("disabled not persisted: %v", entry)
	}
	// Every other field preserved verbatim.
	if entry["name"] != "ws" || entry["env"].(map[string]any)["K"] != "v" {
		t.Fatalf("fields lost: %v", entry)
	}
	if len(entry["tools"].([]any)) != 2 {
		t.Fatalf("tools lost: %v", entry)
	}

	// Toggle back: the marker is REMOVED (enabled = absence).
	on, err = toggleDisabled(path, "mcp", 0)
	if err != nil || on != false {
		t.Fatalf("untoggle: on=%v err=%v", on, err)
	}
	data, _ = os.ReadFile(path)
	json.Unmarshal(data, &cfg)
	entry = cfg["servers"].([]any)[0].(map[string]any)
	if _, has := entry["disabled"]; has {
		t.Fatalf("disabled marker should be removed: %v", entry)
	}
}

func TestToggleDisabledPluginArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	os.WriteFile(path, []byte(`[{"name":"p1","command":["x"]},{"name":"p2","command":["y"]}]`), 0o644)
	if _, err := toggleDisabled(path, "plugin", 1); err != nil {
		t.Fatal(err)
	}
	var specs []map[string]any
	data, _ := os.ReadFile(path)
	json.Unmarshal(data, &specs)
	if _, has := specs[0]["disabled"]; has {
		t.Fatal("wrong entry toggled")
	}
	if specs[1]["disabled"] != true {
		t.Fatalf("p2 not disabled: %v", specs[1])
	}
}

func TestToggleDisabledHooksWrapped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hooks.json")
	os.WriteFile(path, []byte(`{"hooks":[{"event":"session_stop","command":["c"]}]}`), 0o644)
	if _, err := toggleDisabled(path, "hook", 0); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"disabled": true`) {
		t.Fatalf("not persisted: %s", data)
	}
}

func TestToggleDisabledErrors(t *testing.T) {
	if _, err := toggleDisabled("/nonexistent.json", "mcp", 0); err == nil {
		t.Fatal("missing file must error")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	os.WriteFile(path, []byte(`{"servers":[]}`), 0o644)
	if _, err := toggleDisabled(path, "mcp", 0); err == nil {
		t.Fatal("out-of-range index must error")
	}
}

// --- config page editing ------------------------------------------------

func configModel(t *testing.T) *Model {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	d := &Data{Config: config.Config{Perm: "gated"}, Skills: skill.Discover()}
	m := New(d)
	m.active = PageConfig
	m.width, m.height = 100, 30
	return m
}

func TestConfigEditSaves(t *testing.T) {
	m := configModel(t)
	// cursor starts at "provider"; move to "perm" (index 2)
	m.Update(key("j"))
	m.Update(key("j"))
	m.Update(key("enter")) // edit perm (prefilled "gated")
	if !m.config.editing {
		t.Fatal("should be editing")
	}
	// clear prefill, type "auto"
	for range "gated" {
		m.Update(key("backspace"))
	}
	for _, r := range "auto" {
		m.Update(key(string(r)))
	}
	m.Update(key("enter"))
	if m.config.editing {
		t.Fatalf("edit should close on save (err=%q)", m.config.err)
	}
	if m.data.Config.Perm != "auto" {
		t.Fatalf("perm = %q", m.data.Config.Perm)
	}
	// persisted to disk
	saved := config.Load()
	if saved.Perm != "auto" {
		t.Fatalf("not persisted: %q", saved.Perm)
	}
}

func TestConfigEditValidates(t *testing.T) {
	m := configModel(t)
	m.Update(key("j"))
	m.Update(key("j"))
	m.Update(key("enter"))
	for range "gated" {
		m.Update(key("backspace"))
	}
	for _, r := range "bogus" {
		m.Update(key(string(r)))
	}
	m.Update(key("enter"))
	if !m.config.editing || m.config.err == "" {
		t.Fatalf("invalid value must keep editing with an error: editing=%v err=%q",
			m.config.editing, m.config.err)
	}
	m.Update(key("esc"))
	if m.config.editing {
		t.Fatal("esc must cancel")
	}
	if m.data.Config.Perm != "gated" {
		t.Fatal("cancel must not mutate config")
	}
}

func TestConfigEditingCapturesJumpKeys(t *testing.T) {
	m := configModel(t)
	m.Update(key("enter")) // edit "provider"
	m.Update(key("q"))     // must TYPE q, not quit
	if m.quitting {
		t.Fatal("q while editing must not quit")
	}
	if !strings.Contains(m.config.input, "q") {
		t.Fatalf("q should be typed: %q", m.config.input)
	}
}

// --- memory page --------------------------------------------------------

func memModel(t *testing.T, notes string) *Model {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	gm, err := memory.OpenGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if notes != "" {
		os.MkdirAll(filepath.Dir(gm.Path()), 0o755)
		os.WriteFile(gm.Path(), []byte(notes), 0o644)
	}
	d := &Data{Config: config.Config{}, GlobalMem: gm, Skills: skill.Discover()}
	m := New(d)
	m.active = PageMemory
	m.width, m.height = 100, 30
	return m
}

func TestMemoryBullets(t *testing.T) {
	bs := memoryBullets("- one\n  cont\n- two\n")
	if len(bs) != 2 || !strings.Contains(bs[0], "cont") {
		t.Fatalf("bullets: %q", bs)
	}
}

func TestMemoryDeleteWithConfirm(t *testing.T) {
	m := memModel(t, "- first note\n- second note\n- third note\n")
	m.Update(key("j")) // select second
	m.Update(key("d"))
	if !m.memory.confirm {
		t.Fatal("d must ask for confirmation")
	}
	m.Update(key("y"))
	content := m.data.GlobalMem.Read()
	if strings.Contains(content, "second") || !strings.Contains(content, "first") || !strings.Contains(content, "third") {
		t.Fatalf("delete wrong: %q", content)
	}
	// A backup snapshot was taken.
	backups := m.data.GlobalMem.Backups()
	if len(backups) == 0 {
		t.Fatal("delete must snapshot first")
	}
}

func TestMemoryDeleteCancel(t *testing.T) {
	m := memModel(t, "- only note\n")
	m.Update(key("d"))
	m.Update(key("n")) // anything but y cancels
	if strings.Contains(m.data.GlobalMem.Read(), "only note") == false {
		t.Fatal("cancel must not delete")
	}
}

func TestMemoryConsolidateNeedsSmall(t *testing.T) {
	m := memModel(t, "- a note\n")
	m.Update(key("C"))
	if m.memory.consoling {
		t.Fatal("no small model: must not start")
	}
	if !strings.Contains(m.memory.status, "small model") {
		t.Fatalf("status: %q", m.memory.status)
	}
}

// --- home feed dismiss ----------------------------------------------------

func TestHomeFeedDismiss(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := &Data{
		Config: config.Config{},
		Skills: skill.Discover(),
		Feed: feed.Feed{Items: []feed.Item{
			{Kind: "git", Title: "p: 2 uncommitted file(s)", Dir: "/p", Task: "commit"},
			{Kind: "memory", Title: "p: old intent", Dir: "/p", Task: "do"},
		}},
		FeedFresh: true,
	}
	m := New(d)
	m.active = PageHome
	m.width, m.height = 100, 30
	if m.home.feedN != 2 {
		t.Fatalf("feedN = %d", m.home.feedN)
	}
	m.Update(key("d")) // dismiss the selected (first) item
	if m.home.feedN != 1 || m.home.feed[0].Kind != "memory" {
		t.Fatalf("dismiss failed: feedN=%d feed=%+v", m.home.feedN, m.home.feed)
	}
	// Persisted: a fresh filter still drops it.
	if got := feed.FilterDismissed(d.Feed.Items); len(got) != 1 {
		t.Fatalf("not persisted: %+v", got)
	}
}

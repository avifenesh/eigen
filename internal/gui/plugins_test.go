package gui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/avifenesh/eigen/internal/plugin"
)

// writeFile creates a file (with parents) holding marker bytes — enough for the
// pluginEnabled stat checks, which only care about presence.
func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPluginEnabledDerivation checks the on-disk derivation the GUI uses to
// show + toggle a plugin's enabled state, mirroring how Registry.SetEnabled
// parks component files aside with a ".disabled" suffix.
func TestPluginEnabledDerivation(t *testing.T) {
	dir := t.TempDir()
	reg := plugin.NewRegistryAt(dir)

	p := plugin.InstalledPlugin{
		Name:     "toolbox",
		Skills:   []string{"toolbox-greet"},
		Agents:   []string{"toolbox-helper"},
		Commands: []string{"toolbox-do-it"},
	}

	// All active files present → enabled.
	writeFile(t, filepath.Join(reg.SkillsDir(), "toolbox-greet", "SKILL.md"))
	writeFile(t, filepath.Join(reg.AgentsDir(), "toolbox-helper.md"))
	writeFile(t, filepath.Join(reg.CommandsDir(), "toolbox-do-it.md"))
	if !pluginEnabled(reg, p) {
		t.Fatal("plugin with all active component files should read as enabled")
	}

	// Park the skill aside (active gone, ".disabled" present) → disabled, because
	// a single parked component is the disable signal.
	skill := filepath.Join(reg.SkillsDir(), "toolbox-greet", "SKILL.md")
	if err := os.Rename(skill, skill+".disabled"); err != nil {
		t.Fatal(err)
	}
	if pluginEnabled(reg, p) {
		t.Fatal("plugin with a parked skill should read as disabled")
	}

	// Restore it → enabled again.
	if err := os.Rename(skill+".disabled", skill); err != nil {
		t.Fatal(err)
	}
	if !pluginEnabled(reg, p) {
		t.Fatal("plugin should read as enabled after the skill is restored")
	}
}

// TestPluginEnabledMCPOnly: a plugin with only MCP servers / hooks has no
// file-backed component to park, so it defaults to enabled (the JSON disabled
// markers are managed by the registry and never read as a uninstall here).
func TestPluginEnabledMCPOnly(t *testing.T) {
	reg := plugin.NewRegistryAt(t.TempDir())
	p := plugin.InstalledPlugin{Name: "mcponly", MCPServers: []string{"mcponly-srv"}, Hooks: 1}
	if !pluginEnabled(reg, p) {
		t.Fatal("MCP/hooks-only plugin should default to enabled")
	}
}

// TestPluginEnabledRemovedComponent: if neither the active nor the parked file
// exists (record references a component that was scrubbed), that is not a
// disable signal — we do not flip the whole plugin off on a missing file.
func TestPluginEnabledRemovedComponent(t *testing.T) {
	reg := plugin.NewRegistryAt(t.TempDir())
	p := plugin.InstalledPlugin{Name: "ghost", Skills: []string{"ghost-skill"}}
	if !pluginEnabled(reg, p) {
		t.Fatal("a missing component file should not read as disabled")
	}
}

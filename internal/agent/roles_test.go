package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/plugin"
)

func TestLookupRoleLoadsInstalledPluginAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	reg := plugin.NewRegistryAt(filepath.Join(home, ".eigen"))
	agentSkill := "demo-agent-reviewer"
	if err := os.MkdirAll(filepath.Join(reg.SkillsDir(), agentSkill), 0o755); err != nil {
		t.Fatal(err)
	}
	prompt := "---\nname: demo-agent-reviewer\ndescription: Review things\n---\nBe a careful plugin agent.\n"
	if err := os.WriteFile(filepath.Join(reg.SkillsDir(), agentSkill, "SKILL.md"), []byte(prompt), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := reg.RecordInstall(plugin.InstalledPlugin{Name: "demo", Root: filepath.Join(reg.PluginsDir(), "demo"), Skills: []string{agentSkill}, Agents: []string{agentSkill}}); err != nil {
		t.Fatal(err)
	}

	role, ok := LookupRole(agentSkill)
	if !ok {
		t.Fatalf("expected plugin agent role %q", agentSkill)
	}
	if role.ReadOnly || !role.InheritTools {
		t.Fatalf("plugin agent roles should inherit normal task tools/gates, got %+v", role)
	}
	if !strings.Contains(role.System, "installed plugin agent role") || !strings.Contains(role.System, "Be a careful plugin agent") {
		t.Fatalf("plugin role system prompt missing wrapper/original prompt:\n%s", role.System)
	}

	if ok, err := reg.SetEnabled("demo", false); err != nil || !ok {
		t.Fatalf("disable plugin: ok=%v err=%v", ok, err)
	}
	if _, ok := LookupRole(agentSkill); ok {
		t.Fatal("disabled plugin agent should not be advertised as a task role")
	}
}

package plugin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/skill"
)

func TestParseMarketplaceSourcePolymorphism(t *testing.T) {
	js := `{
	  "name": "demo-market",
	  "owner": {"name": "Jane"},
	  "plugins": [
	    {"name": "p-local", "source": "./plugins/p-local", "description": "local"},
	    {"name": "p-obj-local", "source": {"source": "./plugins/x"}},
	    {"name": "p-git", "source": {"source": "github", "repo": "acme/p-git", "ref": "v1"}}
	  ]
	}`
	m, err := ParseMarketplace([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "demo-market" || len(m.Plugins) != 3 {
		t.Fatalf("bad parse: %+v", m)
	}
	if !m.Plugins[0].Source.IsLocal() || m.Plugins[0].Source.Path != "./plugins/p-local" {
		t.Fatalf("string source: %+v", m.Plugins[0].Source)
	}
	if !m.Plugins[1].Source.IsLocal() || m.Plugins[1].Source.Path != "./plugins/x" {
		t.Fatalf("obj-local source: %+v", m.Plugins[1].Source)
	}
	g := m.Plugins[2].Source
	if g.IsLocal() || g.Kind != "github" || g.Repo != "acme/p-git" || g.Ref != "v1" {
		t.Fatalf("github source: %+v", g)
	}
	if _, ok := m.Find("p-git"); !ok {
		t.Fatal("Find should be case-insensitive and locate p-git")
	}
}

func TestParseMarketplaceRejectsMissingName(t *testing.T) {
	if _, err := ParseMarketplace([]byte(`{"plugins":[]}`)); err == nil {
		t.Fatal("expected error on missing name")
	}
}

// buildTarGz makes an in-memory .tar.gz with the given files (path→content),
// all nested under a top dir "repo-main/" like a real GitHub tarball.
func buildTarGz(t *testing.T, top string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		full := top + "/" + name
		if err := tw.WriteHeader(&tar.Header{Name: full, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestExtractTarGzRejectsTraversal(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "../evil.sh", Mode: 0o644, Size: 3, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("bad"))
	tw.Close()
	gz.Close()
	if _, err := extractTarGz(&buf, t.TempDir()); err == nil {
		t.Fatal("expected traversal entry to be rejected")
	}
}

// fakeTree returns a TreeFetcher that extracts a prebuilt tarball, ignoring the
// owner/repo/ref (the test controls the bytes).
func fakeTree(tgz []byte) TreeFetcher {
	return func(_ context.Context, _, _, _, destDir string) (string, error) {
		return extractTarGz(bytes.NewReader(tgz), destDir)
	}
}

// okScanner approves everything; riskyScanner flags everything.
type okScanner struct{}

func (okScanner) Scan(_ context.Context, _, _ string) (skill.ScanResult, error) {
	return skill.ScanResult{Safe: true}, nil
}

type riskyScanner struct{}

func (riskyScanner) Scan(_ context.Context, _, _ string) (skill.ScanResult, error) {
	return skill.ScanResult{Safe: false, Reasons: []string{"curl|sh"}}, nil
}

// A full marketplace+plugin tarball with a skill, an MCP server, and a hook.
func demoTarball(t *testing.T) []byte {
	return buildTarGz(t, "repo-main", map[string]string{
		".claude-plugin/marketplace.json": `{
		  "name": "demo", "owner": {"name": "Jane"},
		  "plugins": [{"name": "toolbox", "source": "./plugins/toolbox", "description": "a toolbox"}]
		}`,
		"plugins/toolbox/.claude-plugin/plugin.json": `{"name": "toolbox", "version": "1.0.0"}`,
		"plugins/toolbox/skills/greet/SKILL.md":      "---\nname: greet\ndescription: \"say hi\"\n---\n\nRun ${CLAUDE_PLUGIN_ROOT}/skills/greet/hi.sh\n",
		"plugins/toolbox/skills/greet/hi.sh":         "echo hi\n",
		"plugins/toolbox/.mcp.json":                  `{"mcpServers": {"box": {"command": "node", "args": ["${CLAUDE_PLUGIN_ROOT}/server.js"], "env": {"K": "v"}}}}`,
		"plugins/toolbox/hooks/hooks.json":           `{"hooks": {"PostToolUse": [{"matcher": "Write", "hooks": [{"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/fmt.sh"}]}]}}`,
		"plugins/toolbox/server.js":                  "// mcp server\n",
		"plugins/toolbox/fmt.sh":                     "echo fmt\n",
		"plugins/toolbox/commands/do-it.md":          "---\ndescription: do the thing\nargument-hint: \"[scope]\"\n---\n\nDo it for: $ARGUMENTS\n",
	})
}

func TestInstallPluginWiresComponents(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistryAt(dir)
	tgz := demoTarball(t)

	// Add the marketplace, then install the plugin.
	if _, _, err := r.AddMarketplace(context.Background(), "jane/demo", fakeTree(tgz)); err != nil {
		t.Fatalf("add marketplace: %v", err)
	}
	res, err := r.InstallPlugin(context.Background(), "toolbox", "", InstallOptions{Scanner: okScanner{}, Tree: fakeTree(tgz)})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	// Skill installed + namespaced + ${ROOT} expanded.
	if len(res.Plugin.Skills) != 1 || res.Plugin.Skills[0] != "toolbox-greet" {
		t.Fatalf("skills = %v", res.Plugin.Skills)
	}
	smd := filepath.Join(dir, "skills", "toolbox-greet", "SKILL.md")
	b, err := os.ReadFile(smd)
	if err != nil {
		t.Fatalf("skill not installed: %v", err)
	}
	// Stored SKILL.md uses OUR namespaced ref, not Claude's var nor a literal path.
	if bytes.Contains(b, []byte("${CLAUDE_PLUGIN_ROOT}")) {
		t.Fatal("Claude var should be rewritten to ${EIGEN_PLUGIN_ROOT}")
	}
	if !bytes.Contains(b, []byte("${EIGEN_PLUGIN_ROOT}")) {
		t.Fatalf("SKILL.md should reference ${EIGEN_PLUGIN_ROOT}, got: %s", b)
	}
	bundle := filepath.Join(dir, "plugins", "toolbox")
	// The bundle path lives in the .eigen-root sidecar (skill.Body expands from it).
	rootSidecar, err := os.ReadFile(filepath.Join(dir, "skills", "toolbox-greet", ".eigen-root"))
	if err != nil || strings.TrimSpace(string(rootSidecar)) != bundle {
		t.Fatalf(".eigen-root sidecar should record the bundle path %s, got %q (err %v)", bundle, rootSidecar, err)
	}
	// Bundled skill support file copied.
	if _, err := os.Stat(filepath.Join(dir, "skills", "toolbox-greet", "hi.sh")); err != nil {
		t.Fatalf("bundled skill file not copied: %v", err)
	}

	// MCP server wired into mcp.json, namespaced, root via OUR env param, described.
	mcp, _ := readObj(r.MCPPath())
	servers, _ := mcp["servers"].([]any)
	if len(servers) != 1 {
		t.Fatalf("want 1 mcp server, got %d", len(servers))
	}
	srv := servers[0].(jsonObj)
	if srv["name"] != "toolbox-box" {
		t.Fatalf("mcp name = %v", srv["name"])
	}
	if srv["description"] == nil || srv["description"] == "" {
		t.Fatal("mcp server must be auto-described")
	}
	// command/args reference ${EIGEN_PLUGIN_ROOT}, NOT a literal path.
	cmd := srv["command"].([]any)
	joined := ""
	for _, c := range cmd {
		joined += c.(string) + " "
	}
	if !bytes.Contains([]byte(joined), []byte("${EIGEN_PLUGIN_ROOT}")) {
		t.Fatalf("mcp command should reference ${EIGEN_PLUGIN_ROOT}, got: %v", cmd)
	}
	if bytes.Contains([]byte(joined), []byte(bundle)) {
		t.Fatalf("mcp command should NOT contain the literal bundle path (use the env param): %v", cmd)
	}
	// The path lives in the server env param EIGEN_PLUGIN_ROOT=<bundle>.
	env, _ := srv["env"].(jsonObj)
	if env == nil {
		if m, ok := srv["env"].(map[string]any); ok {
			env = m
		}
	}
	if env["EIGEN_PLUGIN_ROOT"] != bundle {
		t.Fatalf("EIGEN_PLUGIN_ROOT env should be the bundle path %s, got %v", bundle, env["EIGEN_PLUGIN_ROOT"])
	}

	// Hook wired into hooks.json, event-mapped, ${ROOT}-expanded.
	hk, _ := readObj(r.HooksPath())
	hooks, _ := hk["hooks"].([]any)
	if len(hooks) != 1 {
		t.Fatalf("want 1 hook, got %d", len(hooks))
	}
	h0 := hooks[0].(jsonObj)
	if h0["event"] != "tool_result" {
		t.Fatalf("PostToolUse should map to tool_result, got %v", h0["event"])
	}

	// Command wired into ~/.eigen/commands, namespaced, ${ROOT}-expanded.
	if len(res.Plugin.Commands) != 1 || res.Plugin.Commands[0] != "toolbox-do-it" {
		t.Fatalf("commands = %v", res.Plugin.Commands)
	}
	cb, err := os.ReadFile(filepath.Join(dir, "commands", "toolbox-do-it.md"))
	if err != nil {
		t.Fatalf("command not installed: %v", err)
	}
	if !bytes.Contains(cb, []byte("Do it for: $ARGUMENTS")) {
		t.Fatalf("command body wrong: %s", cb)
	}

	// Recorded as installed.
	if _, ok := r.InstalledByName("toolbox"); !ok {
		t.Fatal("plugin should be recorded as installed")
	}
}

func TestInstallPluginBlocksRiskyUnlessForced(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistryAt(dir)
	tgz := demoTarball(t)
	if _, _, err := r.AddMarketplace(context.Background(), "jane/demo", fakeTree(tgz)); err != nil {
		t.Fatal(err)
	}
	// Risky scan, no force → blocked, nothing recorded, nothing wired.
	_, err := r.InstallPlugin(context.Background(), "toolbox", "", InstallOptions{Scanner: riskyScanner{}, Tree: fakeTree(tgz)})
	if err == nil {
		t.Fatal("risky plugin must be blocked without --force")
	}
	if _, ok := r.InstalledByName("toolbox"); ok {
		t.Fatal("blocked install must not record the plugin")
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "toolbox-greet")); err == nil {
		t.Fatal("blocked install must not leave skill files")
	}

	// With force → installed despite the verdict (surfaced in Scans).
	res, err := r.InstallPlugin(context.Background(), "toolbox", "", InstallOptions{Scanner: riskyScanner{}, Force: true, Tree: fakeTree(tgz)})
	if err != nil {
		t.Fatalf("forced install: %v", err)
	}
	if len(res.Scans) == 0 {
		t.Fatal("forced install should still surface the risky verdict")
	}
}

func TestUninstallReversesWiring(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistryAt(dir)
	tgz := demoTarball(t)
	_, _, _ = r.AddMarketplace(context.Background(), "jane/demo", fakeTree(tgz))
	if _, err := r.InstallPlugin(context.Background(), "toolbox", "", InstallOptions{Scanner: okScanner{}, Tree: fakeTree(tgz)}); err != nil {
		t.Fatal(err)
	}
	ok, err := r.Uninstall("toolbox")
	if err != nil || !ok {
		t.Fatalf("uninstall: ok=%v err=%v", ok, err)
	}
	// Skill dir, mcp server, hook, bundle, record all gone.
	if _, err := os.Stat(filepath.Join(dir, "skills", "toolbox-greet")); err == nil {
		t.Fatal("skill dir should be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "commands", "toolbox-do-it.md")); err == nil {
		t.Fatal("command file should be removed")
	}
	mcp, _ := readObj(r.MCPPath())
	if servers, _ := mcp["servers"].([]any); len(servers) != 0 {
		t.Fatalf("mcp servers should be empty, got %d", len(servers))
	}
	hk, _ := readObj(r.HooksPath())
	if hooks, _ := hk["hooks"].([]any); len(hooks) != 0 {
		t.Fatalf("hooks should be empty, got %d", len(hooks))
	}
	if _, ok := r.InstalledByName("toolbox"); ok {
		t.Fatal("record should be gone")
	}
}

func TestSetEnabledTogglesComponents(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistryAt(dir)
	tgz := demoTarball(t)
	_, _, _ = r.AddMarketplace(context.Background(), "jane/demo", fakeTree(tgz))
	if _, err := r.InstallPlugin(context.Background(), "toolbox", "", InstallOptions{Scanner: okScanner{}, Tree: fakeTree(tgz)}); err != nil {
		t.Fatal(err)
	}
	// Disable: SKILL.md parked, mcp server marked disabled.
	if ok, err := r.SetEnabled("toolbox", false); err != nil || !ok {
		t.Fatalf("disable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "toolbox-greet", "SKILL.md")); err == nil {
		t.Fatal("SKILL.md should be parked aside when disabled")
	}
	mcp, _ := readObj(r.MCPPath())
	srv := mcp["servers"].([]any)[0].(jsonObj)
	if srv["disabled"] != true {
		t.Fatal("mcp server should be marked disabled")
	}
	// Re-enable: restored.
	if ok, err := r.SetEnabled("toolbox", true); err != nil || !ok {
		t.Fatalf("enable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "toolbox-greet", "SKILL.md")); err != nil {
		t.Fatal("SKILL.md should be restored when enabled")
	}
	mcp, _ = readObj(r.MCPPath())
	srv = mcp["servers"].([]any)[0].(jsonObj)
	if _, dis := srv["disabled"]; dis {
		t.Fatal("mcp server disabled marker should be removed when enabled")
	}
}

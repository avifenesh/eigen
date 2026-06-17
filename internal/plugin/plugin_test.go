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
	    {"name": "p-git", "source": {"source": "github", "repo": "acme/p-git", "ref": "v1"}},
	    {"name": "p-url", "source": {"source": "url", "url": "https://github.com/acme/p-url.git", "commit": "abc123"}},
	    {"name": "p-subdir", "source": {"source": "git-subdir", "url": "https://github.com/acme/packs.git", "path": "plugins/one", "sha": "def456"}},
	    {"name": "p-url-string", "source": "https://github.com/acme/p-url-string.git"}
	  ]
	}`
	m, err := ParseMarketplace([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "demo-market" || len(m.Plugins) != 6 {
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
	u := m.Plugins[3].Source
	if u.Kind != "url" || u.Repo != "https://github.com/acme/p-url.git" || u.EffectiveRef() != "abc123" {
		t.Fatalf("url source: %+v", u)
	}
	sd := m.Plugins[4].Source
	if sd.Kind != "git-subdir" || sd.Repo != "https://github.com/acme/packs.git" || sd.Path != "plugins/one" || sd.EffectiveRef() != "def456" {
		t.Fatalf("git-subdir source: %+v", sd)
	}
	su := m.Plugins[5].Source
	if su.Kind != "url" || su.Repo != "https://github.com/acme/p-url-string.git" {
		t.Fatalf("url string source: %+v", su)
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

func TestAddMarketplaceRejectsPlainHTTPURL(t *testing.T) {
	r := NewRegistryAt(t.TempDir())
	if _, _, err := r.AddMarketplace(context.Background(), "http://example.com/marketplace.json", nil); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected https-only error, got %v", err)
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

func TestExtractTarGzIgnoresPAXGlobalHeader(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader}); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"demo","plugins":[]}`
	if err := tw.WriteHeader(&tar.Header{Name: "repo-main/.claude-plugin/marketplace.json", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	root, err := extractTarGz(bytes.NewReader(buf.Bytes()), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(root) != "repo-main" {
		t.Fatalf("topDir should ignore pax global header, got %s", root)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude-plugin", "marketplace.json")); err != nil {
		t.Fatalf("marketplace missing after extract: %v", err)
	}
}

func TestExtractTarGzSkipsSymlinkEntries(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "repo-main/link", Linkname: "/etc", Typeflag: tar.TypeSymlink}); err != nil {
		t.Fatal(err)
	}
	body := "safe"
	if err := tw.WriteHeader(&tar.Header{Name: "repo-main/link/file.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	root, err := extractTarGz(bytes.NewReader(buf.Bytes()), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(root, "link"))
	if err != nil {
		t.Fatalf("expected link path to exist as a directory for nested file: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("tar symlink entry should be skipped, not created")
	}
	if !info.IsDir() {
		t.Fatalf("link path should be a directory created by nested file, got %v", info.Mode())
	}
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

func TestSafeJoinUnderRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"../evil", "a/../../evil"} {
		if _, err := safeJoinUnder(root, rel, "test"); err == nil {
			t.Fatalf("expected %q to be rejected", rel)
		}
	}
	if got, err := safeJoinUnder(root, "./plugins/toolbox", "test"); err != nil || got != filepath.Join(root, "plugins", "toolbox") {
		t.Fatalf("safe relative path: got %q err %v", got, err)
	}
}

func TestSafeJoinUnderRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := safeJoinUnder(root, "link/secret.txt", "test")
	if err == nil || !strings.Contains(err.Error(), "resolves outside root") {
		t.Fatalf("expected symlink escape rejection, got %v", err)
	}
}

// fakeTree returns a TreeFetcher that extracts a prebuilt tarball, ignoring the
// owner/repo/ref (the test controls the bytes).
func fakeTree(tgz []byte) TreeFetcher {
	return func(_ context.Context, _, _, _, destDir string) (string, error) {
		return extractTarGz(bytes.NewReader(tgz), destDir)
	}
}

func TestInstallPluginRejectsExternalRepoPathTraversal(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistryAt(dir)
	tgz := buildTarGz(t, "repo-main", map[string]string{
		".claude-plugin/marketplace.json": `{
		  "name": "demo", "owner": {"name": "Jane"},
		  "plugins": [{"name": "escape", "source": {"source": "github", "repo": "evil/repo/../outside"}}]
		}`,
	})
	if _, _, err := r.AddMarketplace(context.Background(), "jane/demo", fakeTree(tgz)); err != nil {
		t.Fatalf("add marketplace: %v", err)
	}
	_, err := r.InstallPlugin(context.Background(), "escape", "", InstallOptions{Scanner: okScanner{}, Tree: fakeTree(tgz)})
	if err == nil || !strings.Contains(err.Error(), "unsafe plugin repo path") {
		t.Fatalf("expected unsafe plugin repo path error, got %v", err)
	}
}

func TestDiscoverRejectsManifestComponentTraversal(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"escape","mcpServers":"../outside.json"}`
	if err := os.WriteFile(filepath.Join(root, ".claude-plugin", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(root, false); err == nil || !strings.Contains(err.Error(), "unsafe mcpServers path") {
		t.Fatalf("expected unsafe mcpServers path error, got %v", err)
	}

	manifest = `{"name":"escape","hooks":"../outside.json"}`
	if err := os.WriteFile(filepath.Join(root, ".claude-plugin", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(root, false); err == nil || !strings.Contains(err.Error(), "unsafe hooks path") {
		t.Fatalf("expected unsafe hooks path error, got %v", err)
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

type commandRiskyScanner struct{}

func (commandRiskyScanner) Scan(_ context.Context, name, _ string) (skill.ScanResult, error) {
	if name == "do-it" {
		return skill.ScanResult{Safe: false, Reasons: []string{"command risky"}}, nil
	}
	return skill.ScanResult{Safe: true}, nil
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
	if res.Plugin.ScanStatus != ScanStatusClean || res.Plugin.ScanCount != 2 {
		t.Fatalf("scan metadata = %q/%d, want clean/2", res.Plugin.ScanStatus, res.Plugin.ScanCount)
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

func TestInstallCodexMarketplaceAndAdaptAgents(t *testing.T) {
	dir := t.TempDir()
	market := filepath.Join(dir, "openai-bundled")
	pluginRoot := filepath.Join(market, "plugins", "browser")
	mustWrite(t, filepath.Join(market, ".agents", "plugins", "marketplace.json"), `{
	  "name": "openai-bundled",
	  "interface": {"displayName": "OpenAI Bundled"},
	  "plugins": [{"name": "browser", "source": {"source": "local", "path": "./plugins/browser"}, "category": "Engineering"}]
	}`)
	mustWrite(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), `{
	  "name": "browser",
	  "version": "1.0.0",
	  "description": "Codex browser plugin",
	  "skills": "./skills/",
	  "agents": "./agents/",
	  "mcp_servers": {"browser": {"command": ["node", "${CODEX_PLUGIN_ROOT}/server.js"], "env": {"ROOT": "${CODEX_PLUGIN_ROOT}"}}}
	}`)
	mustWrite(t, filepath.Join(pluginRoot, "skills", "control", "SKILL.md"), "---\nname: control\ndescription: control browser\n---\nUse ${CODEX_PLUGIN_ROOT}/scripts/browser.js\n")
	mustWrite(t, filepath.Join(pluginRoot, "agents", "tester.md"), "---\nname: qa-tester\ndescription: test local web apps\nkind: vision\ndifficulty: easy\nallowed-tools: Read, Grep, Glob\nread_only: true\n---\nYou are a browser QA tester.\n")
	mustWrite(t, filepath.Join(pluginRoot, "server.js"), "// server\n")

	r := NewRegistryAt(filepath.Join(dir, "eigen"))
	if _, _, err := r.AddMarketplace(context.Background(), market, nil); err != nil {
		t.Fatalf("add codex marketplace: %v", err)
	}
	res, err := r.InstallPlugin(context.Background(), "browser", "", InstallOptions{})
	if err != nil {
		t.Fatalf("install codex plugin: %v", err)
	}
	if len(res.Plugin.Skills) != 2 || len(res.Plugin.Agents) != 1 || res.Plugin.Agents[0] != "browser-agent-qa-tester" {
		t.Fatalf("skills=%v agents=%v", res.Plugin.Skills, res.Plugin.Agents)
	}
	if len(res.Plugin.AgentRoles) != 1 || res.Plugin.AgentRoles[0].Kind != "vision" || res.Plugin.AgentRoles[0].Difficulty != "easy" || !res.Plugin.AgentRoles[0].ReadOnly {
		t.Fatalf("agent role metadata not recorded: %+v", res.Plugin.AgentRoles)
	}
	if got := strings.Join(res.Plugin.AgentRoles[0].Tools, ","); got != "read,grep,glob" {
		t.Fatalf("agent role tools = %q", got)
	}
	agentSkill, err := os.ReadFile(filepath.Join(r.SkillsDir(), "browser-agent-qa-tester", "SKILL.md"))
	if err != nil {
		t.Fatalf("agent skill missing: %v", err)
	}
	if !bytes.Contains(agentSkill, []byte("Original agent prompt")) || !bytes.Contains(agentSkill, []byte("browser QA tester")) {
		t.Fatalf("agent prompt not preserved:\n%s", agentSkill)
	}
	browserSkill, err := os.ReadFile(filepath.Join(r.SkillsDir(), "browser-control", "SKILL.md"))
	if err != nil {
		t.Fatalf("browser skill missing: %v", err)
	}
	if bytes.Contains(browserSkill, []byte("${CODEX_PLUGIN_ROOT}")) || !bytes.Contains(browserSkill, []byte("${EIGEN_PLUGIN_ROOT}")) {
		t.Fatalf("Codex root var should be rewritten, got:\n%s", browserSkill)
	}
	mcp, _ := readObj(r.MCPPath())
	servers, _ := mcp["servers"].([]any)
	if len(servers) != 1 {
		t.Fatalf("want 1 mcp server, got %d", len(servers))
	}
	cmd := servers[0].(jsonObj)["command"].([]any)
	if !strings.Contains(cmd[1].(string), "${EIGEN_PLUGIN_ROOT}") {
		t.Fatalf("mcp command should use eigen root var: %v", cmd)
	}
	ok, err := r.Uninstall("browser")
	if err != nil || !ok {
		t.Fatalf("uninstall browser: ok=%v err=%v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(r.SkillsDir(), "browser-agent-qa-tester")); err == nil {
		t.Fatal("uninstall should remove generated agent skill")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewPluginReportsManifestAndComponents(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistryAt(dir)
	tgz := demoTarball(t)
	if _, _, err := r.AddMarketplace(context.Background(), "jane/demo", fakeTree(tgz)); err != nil {
		t.Fatal(err)
	}
	pv, err := r.PreviewPlugin(context.Background(), "toolbox", "", fakeTree(tgz))
	if err != nil {
		t.Fatal(err)
	}
	if pv.Entry.Name != "toolbox" || pv.Manifest == nil || pv.Manifest.Name != "toolbox" {
		t.Fatalf("bad preview identity: %+v", pv)
	}
	if len(pv.Skills) != 1 || pv.Skills[0] != "greet" || len(pv.Commands) != 1 || pv.Commands[0] != "do-it" || len(pv.MCPServers) != 1 || pv.Hooks != 1 {
		t.Fatalf("preview missing components: %+v", pv)
	}
}

func TestAgentReadOnlyMetadataFailsClosed(t *testing.T) {
	tools, ok := normalizeAgentTools([]string{"Read", "Grep"}, false, false)
	if !ok || strings.Join(tools, ",") != "read,grep" {
		t.Fatalf("read-only allowlist should be accepted, tools=%v ok=%v", tools, ok)
	}
	if tools, ok := normalizeAgentTools([]string{"Read"}, false, true); ok || len(tools) != 0 {
		t.Fatalf("explicit read_only:false should not auto-promote, tools=%v ok=%v", tools, ok)
	}
	if tools, ok := normalizeAgentTools([]string{"Read", "Write"}, true, true); ok || len(tools) != 0 {
		t.Fatalf("mutating tool should fail closed even with read_only:true, tools=%v ok=%v", tools, ok)
	}
	tools, ok = normalizeAgentTools(nil, true, true)
	if !ok || strings.Join(tools, ",") != "read,grep,glob,list,tree,symbols,diff" {
		t.Fatalf("read_only:true with no tools should get safe defaults, tools=%v ok=%v", tools, ok)
	}
	if _, ok := normalizeAgentTools([]string{"Skill"}, true, true); ok {
		t.Fatal("skill tool should not be admitted to plugin read-only task_group roles")
	}
}

func TestDisabledMarketplaceIsNotSearchedForInstalls(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistryAt(dir)
	tgz := demoTarball(t)
	if _, _, err := r.AddMarketplace(context.Background(), "jane/demo", fakeTree(tgz)); err != nil {
		t.Fatal(err)
	}
	if ok, err := r.SetMarketEnabled("demo", false); err != nil || !ok {
		t.Fatalf("disable market: ok=%v err=%v", ok, err)
	}
	if _, err := r.InstallPlugin(context.Background(), "toolbox", "", InstallOptions{Tree: fakeTree(tgz)}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("disabled marketplace should be skipped for implicit installs, got %v", err)
	}
	if _, err := r.InstallPlugin(context.Background(), "toolbox", "demo", InstallOptions{Tree: fakeTree(tgz)}); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("explicit disabled marketplace should explain disabled state, got %v", err)
	}
	if ok, err := r.SetMarketEnabled("demo", true); err != nil || !ok {
		t.Fatalf("enable market: ok=%v err=%v", ok, err)
	}
	if _, err := r.InstallPlugin(context.Background(), "toolbox", "", InstallOptions{Tree: fakeTree(tgz)}); err != nil {
		t.Fatalf("enabled marketplace should install: %v", err)
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
	if res.Plugin.ScanStatus != ScanStatusForced || res.Plugin.ScanCount != 2 {
		t.Fatalf("forced install scan metadata = %q/%d, want forced/2", res.Plugin.ScanStatus, res.Plugin.ScanCount)
	}
	if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "forced install") {
		t.Fatalf("forced install should surface a warning, got %+v", res.Warnings)
	}
	rec, ok := r.InstalledByName("toolbox")
	if !ok || len(rec.Scans) == 0 || rec.ScanStatus != ScanStatusForced || len(rec.Warnings) == 0 {
		t.Fatalf("forced scan findings should be recorded for UI audit: ok=%v rec=%+v", ok, rec)
	}
}

func TestInstallRollbackCleansEarlierComponentsWhenLaterScanFails(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistryAt(dir)
	tgz := demoTarball(t)
	_, _, _ = r.AddMarketplace(context.Background(), "jane/demo", fakeTree(tgz))
	_, err := r.InstallPlugin(context.Background(), "toolbox", "", InstallOptions{Scanner: commandRiskyScanner{}, Tree: fakeTree(tgz)})
	if err == nil {
		t.Fatal("expected command scan failure")
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "toolbox-greet")); err == nil {
		t.Fatal("rollback should remove skills installed before later scan failure")
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins", "toolbox")); err == nil {
		t.Fatal("rollback should remove cached bundle")
	}
	mcp, _ := readObj(r.MCPPath())
	if servers, _ := mcp["servers"].([]any); len(servers) != 0 {
		t.Fatalf("rollback should remove mcp servers, got %d", len(servers))
	}
	if _, ok := r.InstalledByName("toolbox"); ok {
		t.Fatal("failed install must not be recorded")
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

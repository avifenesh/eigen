package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func isolateComputerUse(t *testing.T) { t.Setenv("EIGEN_COMPUTER_USE_BIN", "/nonexistent") }

func TestWithBuiltinServersAddsComputerUse(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "computer-use-linux")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("EIGEN_COMPUTER_USE_BIN", bin)
	t.Setenv("EIGEN_WORKSPACE_BIN", "/nonexistent")
	t.Setenv("EIGEN_CHROME_BRIDGE", "/nonexistent")

	got := withBuiltinServers(nil)
	if len(got) != 1 || got[0].Name != "computer_use" {
		t.Fatalf("computer_use should be auto-added, got %+v", got)
	}
	if got[0].Command[0] != bin || got[0].Command[1] != "mcp" {
		t.Fatalf("command should use detected computer-use binary, got %v", got[0].Command)
	}
	if len(got[0].Tools) != len(computerUseTools) {
		t.Fatalf("computer-use allowlist should apply, got %d", len(got[0].Tools))
	}
}

func TestComputerUseUserWins(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "computer-use-linux")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("EIGEN_COMPUTER_USE_BIN", bin)
	t.Setenv("EIGEN_WORKSPACE_BIN", "/nonexistent")
	t.Setenv("EIGEN_CHROME_BRIDGE", "/nonexistent")
	user := []serverConfig{{Name: "computer_use", Command: []string{"my-computer-use"}}}
	got := withBuiltinServers(user)
	if len(got) != 1 || got[0].Command[0] != "my-computer-use" {
		t.Fatalf("user's computer_use config must win, got %+v", got)
	}
}

func TestWithBuiltinServersAddsWorkspace(t *testing.T) {
	isolateComputerUse(t)
	// Point the locator at a fake executable so detection succeeds.
	dir := t.TempDir()
	bin := filepath.Join(dir, "agent-workspace-linux")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("EIGEN_WORKSPACE_BIN", bin)
	t.Setenv("EIGEN_CHROME_BRIDGE", "/nonexistent") // isolate from the real bridge

	got := withBuiltinServers(nil)
	if len(got) != 1 || got[0].Name != "workspace" {
		t.Fatalf("workspace should be auto-added, got %+v", got)
	}
	if len(got[0].Tools) != len(workspaceTools) {
		t.Fatalf("curated allowlist should be applied, got %d tools", len(got[0].Tools))
	}
	if got[0].Command[0] != bin {
		t.Fatalf("command should use the detected binary, got %v", got[0].Command)
	}
}

func TestWithBuiltinServersUserWins(t *testing.T) {
	isolateComputerUse(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "agent-workspace-linux")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("EIGEN_WORKSPACE_BIN", bin)

	t.Setenv("EIGEN_CHROME_BRIDGE", "/nonexistent")
	user := []serverConfig{{Name: "workspace", Command: []string{"my-own-binary"}}}
	got := withBuiltinServers(user)
	if len(got) != 1 {
		t.Fatalf("user's workspace must not be duplicated, got %d", len(got))
	}
	if got[0].Command[0] != "my-own-binary" {
		t.Fatal("user config must win over the built-in")
	}
}

func TestHarnessBinariesIgnorePATHUnlessExplicit(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"computer-use-linux", "agent-workspace-linux"} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
	}
	t.Setenv("PATH", dir)
	t.Setenv("EIGEN_COMPUTER_USE_BIN", "")
	t.Setenv("EIGEN_WORKSPACE_BIN", "")
	t.Setenv("HOME", t.TempDir())
	if got := ComputerUseBinary(); got != "" {
		t.Fatalf("computer-use PATH binary should not auto-register: %q", got)
	}
	if got := WorkspaceBinary(); got != "" {
		t.Fatalf("workspace PATH binary should not auto-register: %q", got)
	}
}

func TestWithBuiltinServersAbsentBinary(t *testing.T) {
	isolateComputerUse(t)
	t.Setenv("EIGEN_WORKSPACE_BIN", "/nonexistent/binary")
	t.Setenv("EIGEN_CHROME_BRIDGE", "/nonexistent")
	got := withBuiltinServers(nil)
	if len(got) != 0 {
		t.Fatalf("no binary → no built-in server, got %+v", got)
	}
}

func TestIsExecutable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "x")
	os.WriteFile(exe, []byte("x"), 0o755)
	if !isExecutable(exe) {
		t.Error("0755 file should be executable")
	}
	noexe := filepath.Join(dir, "y")
	os.WriteFile(noexe, []byte("y"), 0o644)
	if isExecutable(noexe) {
		t.Error("0644 file should not be executable")
	}
	if isExecutable(dir) {
		t.Error("a directory is not executable")
	}
}

func TestChromeBridgeDoesNotDependOnSiblingCheckout(t *testing.T) {
	isolateComputerUse(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("EIGEN_WORKSPACE_BIN", "/nonexistent")
	t.Setenv("EIGEN_CHROME_BRIDGE", "")
	if script, _ := ChromeBridge(); script != "" {
		t.Fatalf("chrome bridge should require explicit configuration, got %q", script)
	}
}

func TestChromeBridgeFindsEigenInstalledConnector(t *testing.T) {
	isolateComputerUse(t)
	home := t.TempDir()
	dir := filepath.Join(home, ".eigen", "chrome-bridge", "bin")
	os.MkdirAll(dir, 0o755)
	script := filepath.Join(dir, "mcp-server.js")
	os.WriteFile(script, []byte("// fake"), 0o644)
	node := filepath.Join(home, "node")
	os.WriteFile(node, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("HOME", home)
	t.Setenv("EIGEN_CHROME_BRIDGE", "")
	t.Setenv("EIGEN_NODE_BIN", node)
	gotScript, gotNode := ChromeBridge()
	if gotScript != script || gotNode != node {
		t.Fatalf("ChromeBridge installed = %q %q, want %q %q", gotScript, gotNode, script, node)
	}
}

func TestWithBuiltinServersAddsChrome(t *testing.T) {
	isolateComputerUse(t)
	dir := t.TempDir()
	// A fake bridge: <dir>/bin/mcp-server.js + a fake node binary.
	os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	script := filepath.Join(dir, "bin", "mcp-server.js")
	os.WriteFile(script, []byte("// fake"), 0o644)
	node := filepath.Join(dir, "node")
	os.WriteFile(node, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("EIGEN_WORKSPACE_BIN", "/nonexistent") // isolate workspace
	t.Setenv("EIGEN_CHROME_BRIDGE", dir)            // repo dir form
	t.Setenv("EIGEN_NODE_BIN", node)

	got := withBuiltinServers(nil)
	if len(got) != 1 || got[0].Name != "chrome" {
		t.Fatalf("chrome should be auto-added, got %+v", got)
	}
	if got[0].Command[0] != node || got[0].Command[1] != script {
		t.Fatalf("command should be [node script], got %v", got[0].Command)
	}
	if len(got[0].Tools) != len(chromeBridgeTools) {
		t.Fatalf("chrome allowlist should apply, got %d", len(got[0].Tools))
	}
}

func TestChromeBridgeUserWins(t *testing.T) {
	isolateComputerUse(t)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	os.WriteFile(filepath.Join(dir, "bin", "mcp-server.js"), []byte("//"), 0o644)
	node := filepath.Join(dir, "node")
	os.WriteFile(node, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("EIGEN_WORKSPACE_BIN", "/nonexistent")
	t.Setenv("EIGEN_CHROME_BRIDGE", dir)
	t.Setenv("EIGEN_NODE_BIN", node)

	user := []serverConfig{{Name: "chrome", Command: []string{"my-chrome"}}}
	got := withBuiltinServers(user)
	if len(got) != 1 || got[0].Command[0] != "my-chrome" {
		t.Fatalf("user's chrome config must win, got %+v", got)
	}
}

func TestChromeBridgeAbsentNode(t *testing.T) {
	isolateComputerUse(t)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	os.WriteFile(filepath.Join(dir, "bin", "mcp-server.js"), []byte("//"), 0o644)
	t.Setenv("EIGEN_WORKSPACE_BIN", "/nonexistent")
	t.Setenv("EIGEN_CHROME_BRIDGE", dir)
	t.Setenv("EIGEN_NODE_BIN", "/nonexistent/node") // no node → not registered
	if got := withBuiltinServers(nil); len(got) != 0 {
		t.Fatalf("no node → chrome not registered, got %+v", got)
	}
}

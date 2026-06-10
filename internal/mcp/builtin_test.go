package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWithBuiltinServersAddsWorkspace(t *testing.T) {
	// Point the locator at a fake executable so detection succeeds.
	dir := t.TempDir()
	bin := filepath.Join(dir, "agent-workspace-linux")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("EIGEN_WORKSPACE_BIN", bin)

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
	dir := t.TempDir()
	bin := filepath.Join(dir, "agent-workspace-linux")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("EIGEN_WORKSPACE_BIN", bin)

	user := []serverConfig{{Name: "workspace", Command: []string{"my-own-binary"}}}
	got := withBuiltinServers(user)
	if len(got) != 1 {
		t.Fatalf("user's workspace must not be duplicated, got %d", len(got))
	}
	if got[0].Command[0] != "my-own-binary" {
		t.Fatal("user config must win over the built-in")
	}
}

func TestWithBuiltinServersAbsentBinary(t *testing.T) {
	t.Setenv("EIGEN_WORKSPACE_BIN", "/nonexistent/binary")
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

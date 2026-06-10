package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentsGuidanceReadsNearest(t *testing.T) {
	root := t.TempDir()
	// repo root marker + AGENTS.md, plus a nested dir with its own.
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Root rule: use tabs."), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "service")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "AGENTS.md"), []byte("Service rule: no panics."), 0o644); err != nil {
		t.Fatal(err)
	}

	g := agentsGuidance(sub)
	if !strings.Contains(g, "Service rule: no panics.") {
		t.Fatalf("should include the nearest AGENTS.md:\n%s", g)
	}
	if !strings.Contains(g, "Root rule: use tabs.") {
		t.Fatalf("should walk up to the root AGENTS.md:\n%s", g)
	}
	// Nearest first.
	if strings.Index(g, "Service rule") > strings.Index(g, "Root rule") {
		t.Fatal("nearest guidance should come first")
	}
}

func TestAgentsGuidanceNoneFound(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if g := agentsGuidance(dir); g != "" {
		t.Fatalf("no AGENTS.md should yield empty, got:\n%s", g)
	}
}

func TestAgentsGuidanceTruncates(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	big := strings.Repeat("x", 20*1024)
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(big), 0o644)
	g := agentsGuidance(dir)
	if !strings.Contains(g, "[truncated]") {
		t.Fatal("oversized AGENTS.md should be truncated")
	}
}

func TestAgentsGuidanceStopsAtRepoRoot(t *testing.T) {
	// A file ABOVE the repo root must not be picked up.
	outer := t.TempDir()
	os.WriteFile(filepath.Join(outer, "AGENTS.md"), []byte("OUTSIDE rule."), 0o644)
	repo := filepath.Join(outer, "repo")
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("inside rule."), 0o644)

	g := agentsGuidance(repo)
	if strings.Contains(g, "OUTSIDE rule.") {
		t.Fatalf("must not read AGENTS.md above the repo root:\n%s", g)
	}
	if !strings.Contains(g, "inside rule.") {
		t.Fatal("should read the repo-root AGENTS.md")
	}
}

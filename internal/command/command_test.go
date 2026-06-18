package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseClaudeCommandFrontmatter(t *testing.T) {
	// The real agentsys system-prompt-curator command shape.
	src := `---
description: Create or improve production-grade system prompts
argument-hint: "[role description or --improve path]"
allowed-tools: Read, Write
model: claude-opus-4
---

You are the System Prompt Curator. Do the work for: $ARGUMENTS
`
	c := parse("system-prompt-curator", src)
	if c.Description != "Create or improve production-grade system prompts" {
		t.Fatalf("description = %q", c.Description)
	}
	if c.ArgHint != "[role description or --improve path]" {
		t.Fatalf("arg-hint = %q", c.ArgHint)
	}
	if c.Model != "claude-opus-4" {
		t.Fatalf("model = %q, want claude-opus-4", c.Model)
	}
	if strings.Contains(c.Body, "---") || strings.Contains(c.Body, "allowed-tools") {
		t.Fatalf("frontmatter leaked into body: %q", c.Body)
	}
	if !strings.HasPrefix(c.Body, "You are the System Prompt Curator") {
		t.Fatalf("body = %q", c.Body)
	}
}

func TestExpandArguments(t *testing.T) {
	body := "Review scope: $ARGUMENTS\nFirst: $1 Second: $2"
	got := Expand(body, `src/ "two words"`)
	if !strings.Contains(got, "Review scope: src/ \"two words\"") {
		t.Fatalf("$ARGUMENTS not expanded: %q", got)
	}
	if !strings.Contains(got, "First: src/ Second: two words") {
		t.Fatalf("positional args wrong: %q", got)
	}
}

func TestExpandNoPlaceholderAppendsArgs(t *testing.T) {
	got := Expand("Do the thing.", "with extra context")
	if !strings.Contains(got, "Do the thing.") || !strings.HasSuffix(strings.TrimSpace(got), "with extra context") {
		t.Fatalf("args should be appended when no placeholder: %q", got)
	}
	// No args, no placeholder → unchanged.
	if got := Expand("Just do it.", ""); strings.TrimSpace(got) != "Just do it." {
		t.Fatalf("unexpected change: %q", got)
	}
}

func TestLoadProjectShadowsUser(t *testing.T) {
	user := t.TempDir()
	proj := t.TempDir()
	write := func(dir, name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(user, "greet.md", "---\ndescription: user greet\n---\nUSER body")
	write(user, "only-user.md", "---\ndescription: only user\n---\nbody")
	write(proj, "greet.md", "---\ndescription: project greet\n---\nPROJECT body")

	s := Load(proj, user) // project first
	if s.Len() != 2 {
		t.Fatalf("want 2 commands, got %d (%v)", s.Len(), s.Names())
	}
	g, ok := s.Get("greet")
	if !ok || g.Scope != "project" || !strings.Contains(g.Body, "PROJECT") {
		t.Fatalf("project should shadow user: %+v", g)
	}
	if _, ok := s.Get("only-user"); !ok {
		t.Fatal("user-only command should still load")
	}
}

func TestLoadAgentsysStyleCommandFixture(t *testing.T) {
	dir := t.TempDir()
	body := `---
description: Create or improve production-grade system prompts
argument-hint: "[role description or --improve path]"
allowed-tools: Read, Write
model: claude-opus-4
---

You are the System Prompt Curator. Do the work for: $ARGUMENTS
`
	if err := os.WriteFile(filepath.Join(dir, "system-prompt-curator.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Load(dir)
	if s.Len() != 1 {
		t.Fatalf("want one fixture command, got %d", s.Len())
	}
	c := s.All()[0]
	if c.Description != "Create or improve production-grade system prompts" || c.ArgHint != "[role description or --improve path]" || c.Body == "" {
		t.Fatalf("fixture command parsed wrong: %+v", c)
	}
	if strings.Contains(c.Body, "\n---\n") || strings.Contains(c.Body, "allowed-tools") {
		t.Fatalf("frontmatter leaked into command body: %q", c.Body)
	}
}

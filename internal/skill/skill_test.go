package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSkill creates dir/<name>/SKILL.md with the given frontmatter + body.
func writeSkill(t *testing.T, dir, name, desc, body string) {
	t.Helper()
	sd := filepath.Join(dir, name)
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: \"" + desc + "\"\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverAndCatalog(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "alpha", "Use when doing alpha things. More detail here.", "# Alpha\ndo alpha")
	writeSkill(t, dir, "beta", "Use for beta tasks.", "# Beta\ndo beta")

	set := Discover(dir)
	if set.Len() != 2 {
		t.Fatalf("expected 2 skills, got %d", set.Len())
	}
	cat := set.Catalog()
	if !strings.Contains(cat, "alpha:") || !strings.Contains(cat, "beta:") {
		t.Fatalf("catalog missing skills:\n%s", cat)
	}
	// First sentence only in the catalog.
	if strings.Contains(cat, "More detail here") {
		t.Fatalf("catalog should trim to first sentence:\n%s", cat)
	}
}

func TestBodyStripsFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "alpha", "desc", "# Alpha\nthe instructions")

	set := Discover(dir)
	body, err := set.Body("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "name:") || strings.Contains(body, "---") {
		t.Fatalf("body should not contain frontmatter:\n%s", body)
	}
	if !strings.Contains(body, "the instructions") {
		t.Fatalf("body missing instructions:\n%s", body)
	}
}

func TestBodyUnknownSkill(t *testing.T) {
	set := Discover(t.TempDir())
	if _, err := set.Body("nope"); err == nil {
		t.Fatal("unknown skill should error")
	}
}

func TestResolveHints(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "skill-curator", "Curate SKILL.md files.", "# body")
	writeSkill(t, dir, "system-prompt-curator", "Curate system prompts.", "# body")
	writeSkill(t, dir, "pdf", "Work with PDFs.", "# body")
	set := Discover(dir)

	// Exact and loose hints that must land on skill-curator.
	for _, hint := range []string{
		"skill-curator", // exact
		"skill curator", // space separator (the real miss this session)
		"Skill Curator", // case + space
		"skill_curator", // underscore
	} {
		got, ok := set.Resolve(hint)
		if !ok || got != "skill-curator" {
			t.Fatalf("Resolve(%q) = %q,%v; want skill-curator", hint, got, ok)
		}
	}

	// "curator" alone is AMBIGUOUS (two *-curator skills) -> must not guess.
	if got, ok := set.Resolve("curator"); ok {
		t.Fatalf("ambiguous hint \"curator\" should not resolve, got %q", got)
	}

	// A hint that names one curator unambiguously still resolves.
	if got, ok := set.Resolve("system prompt curator"); !ok || got != "system-prompt-curator" {
		t.Fatalf("Resolve(system prompt curator) = %q,%v", got, ok)
	}

	// Body() goes through Resolve, so a hint loads the right skill.
	if _, err := set.Body("skill curator"); err != nil {
		t.Fatalf("Body via hint should load: %v", err)
	}

	// Pure nonsense still fails closed.
	if _, ok := set.Resolve("zzzqqq"); ok {
		t.Fatal("nonsense hint should not resolve")
	}
}

func TestGetAcceptsHint(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "frontend-skill", "Build UIs.", "# body")
	set := Discover(dir)
	if _, ok := set.Get("frontend skill"); !ok {
		t.Fatal("Get should accept a loose hint")
	}
}

func TestResolveRescansForMidSessionInstall(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "alpha", "Alpha.", "# a")
	set := Discover(dir) // snapshot taken with only alpha

	if _, ok := set.Resolve("skill-curator"); ok {
		t.Fatal("skill-curator should not exist yet")
	}
	// Install a skill AFTER the set was discovered (mimics `eigen skill add`
	// or a hand-dropped SKILL.md mid-session).
	writeSkill(t, dir, "skill-curator", "Curate skills.", "# the curator body")

	// Resolve/Body must pick it up without an explicit Rescan (rescan-on-miss).
	if got, ok := set.Resolve("skill curator"); !ok || got != "skill-curator" {
		t.Fatalf("mid-session install not resolved: %q,%v", got, ok)
	}
	body, err := set.Body("skill-curator")
	if err != nil || !strings.Contains(body, "the curator body") {
		t.Fatalf("Body after install: %q err=%v", body, err)
	}
	// And it now shows in the catalog/names.
	if !strings.Contains(strings.Join(set.Names(), ","), "skill-curator") {
		t.Fatalf("names missing the new skill: %v", set.Names())
	}
}

func TestNameFallsBackToDir(t *testing.T) {
	dir := t.TempDir()
	sd := filepath.Join(dir, "no-name-skill")
	os.MkdirAll(sd, 0o755)
	// frontmatter without a name field
	os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte("---\ndescription: \"x\"\n---\nbody"), 0o644)

	set := Discover(dir)
	if _, ok := set.Get("no-name-skill"); !ok {
		t.Fatalf("name should fall back to directory, got %v", set.Names())
	}
}

func TestEmptyCatalog(t *testing.T) {
	set := Discover(t.TempDir())
	if set.Catalog() != "" {
		t.Fatal("no skills should yield an empty catalog")
	}
}

func TestDiscoverDedupesByName(t *testing.T) {
	d1, d2 := t.TempDir(), t.TempDir()
	writeSkill(t, d1, "dup", "first", "body one")
	writeSkill(t, d2, "dup", "second", "body two")
	set := Discover(d1, d2)
	if set.Len() != 1 {
		t.Fatalf("duplicate names should dedupe (first wins), got %d", set.Len())
	}
	body, _ := set.Body("dup")
	if !strings.Contains(body, "body one") {
		t.Fatalf("first directory should win, got %q", body)
	}
}

func TestSaveAndRediscover(t *testing.T) {
	dir := t.TempDir()
	path, err := Save(dir, "my-skill", "Use for testing save.\nSecond line.", "# My Skill\nbody here")
	if err != nil {
		t.Fatal(err)
	}
	set := Discover(dir)
	sk, ok := set.Get("my-skill")
	if !ok {
		t.Fatal("saved skill should be discoverable")
	}
	if strings.Contains(sk.Description, "\n") {
		t.Fatal("description newlines should be collapsed")
	}
	body, _ := set.Body("my-skill")
	if !strings.Contains(body, "body here") {
		t.Fatalf("body not saved:\n%s", body)
	}
	if path == "" {
		t.Fatal("Save should return the path")
	}
}

func TestSaveRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	if _, err := Save(dir, "dup", "a", "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(dir, "dup", "b", "two"); err == nil {
		t.Fatal("Save must refuse to overwrite an existing skill")
	}
}

func TestSaveRejectsBadName(t *testing.T) {
	dir := t.TempDir()
	if _, err := Save(dir, "bad/name", "d", "b"); err == nil {
		t.Fatal("name with a slash should be rejected")
	}
	if _, err := Save(dir, "", "d", "b"); err == nil {
		t.Fatal("empty name should be rejected")
	}
}

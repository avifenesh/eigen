package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPeekCodexSkipsInjectedTitle(t *testing.T) {
	dir := t.TempDir()
	content := `{"timestamp":"t","type":"session_meta","payload":{"cwd":"/home/u/proj","id":"x"}}
{"type":"response_item","payload":{"role":"user","content":[{"type":"text","text":"# AGENTS.md instructions for /home/u/proj\n<INSTRUCTIONS>do things</INSTRUCTIONS>"}]}}
{"type":"response_item","payload":{"role":"user","content":[{"type":"text","text":"actually fix the parser bug"}]}}
`
	p := writeFile(t, dir, "rollout-x.jsonl", content)
	pv := Peek(SourceCodex, p)
	if pv.Cwd != "/home/u/proj" {
		t.Errorf("cwd = %q", pv.Cwd)
	}
	if pv.Title != "actually fix the parser bug" {
		t.Errorf("title should skip the AGENTS.md injection, got %q", pv.Title)
	}
}

func TestPeekClaudeCwdAndTitle(t *testing.T) {
	dir := t.TempDir()
	content := `{"type":"user","cwd":"/home/u/myproj","message":{"role":"user","content":"help me refactor"}}
{"type":"assistant","message":{"role":"assistant","content":"sure"}}
`
	p := writeFile(t, dir, "s.jsonl", content)
	pv := Peek(SourceClaude, p)
	if pv.Cwd != "/home/u/myproj" {
		t.Errorf("cwd = %q", pv.Cwd)
	}
	if pv.Title != "help me refactor" {
		t.Errorf("title = %q", pv.Title)
	}
	if pv.Messages != 2 {
		t.Errorf("messages = %d", pv.Messages)
	}
}

func TestClaudeDirFromPath(t *testing.T) {
	// Claude encodes the cwd by mapping '/', '.', '_', and literal '-' all to a
	// single '-'. The decode must resolve against the real filesystem: build a
	// tree whose names exercise each lossy case, then check round-trips.
	root := t.TempDir()
	cases := []struct {
		name, rel string // rel is the cwd relative to root, exercising one lossy case
	}{
		{"plain dirs", "proj/sub"},
		{"dot dir (--)", "dot/.claude/action-graph"},
		{"underscore", "us/my_proj"},
		{"literal hyphen", "lit/agent-sh/ada-spark"},
	}
	for _, c := range cases {
		base := filepath.Join(root, c.rel)
		if err := os.MkdirAll(base, 0o755); err != nil {
			t.Fatal(err)
		}
		encoded := encodeClaudeDir(base)
		jsonl := filepath.Join(root, ".claude", "projects", encoded, "abc.jsonl")
		if got := claudeDirFromPath(jsonl); got != base {
			t.Errorf("%s: decoded %q\n  encoded=%q\n  want    %q", c.name, got, encoded, base)
		}
	}

	if claudeDirFromPath("/somewhere/else/abc.jsonl") != "" {
		t.Error("non-claude folder should decode to empty")
	}
	// A name that resolves to no real directory yields "" (no phantom project).
	if got := claudeDirFromPath("/x/.claude/projects/-nonexistent-ghost-proj/a.jsonl"); got != "" {
		t.Errorf("unresolvable name should decode to empty, got %q", got)
	}
}

// encodeClaudeDir mirrors Claude's folder encoder: every non-alphanumeric byte
// of the absolute cwd becomes a single '-'. Used only by the test to build
// inputs the decoder must round-trip.
func encodeClaudeDir(abs string) string {
	var b strings.Builder
	for _, r := range abs {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func TestTitleFromRejectsBoilerplate(t *testing.T) {
	reject := []string{
		"<user_instructions>x</user_instructions>",
		"# AGENTS.md instructions for /x",
		`{"json":"blob"}`,
		"caveat: the messages below",
		"",
	}
	for _, s := range reject {
		if got := titleFrom(s); got != "" {
			t.Errorf("titleFrom(%q) should be empty, got %q", s, got)
		}
	}
	if got := titleFrom("fix the   bug\nin parser"); got != "fix the bug in parser" {
		t.Errorf("whitespace collapse wrong: %q", got)
	}
	long := titleFrom(string(make([]rune, 0)) + "this is a very long user message that certainly exceeds the seventy two character title cap for sure")
	if len([]rune(long)) > 73 {
		t.Errorf("title not truncated: %d runes", len([]rune(long)))
	}
}

func TestPeekEigenUsesMetaDir(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "s.eigen.jsonl", `{"role":"user","text":"do the thing"}`+"\n")
	_ = SaveMeta(p, SessionMeta{Dir: "/home/u/eigenproj"})
	pv := Peek(SourceEigen, p)
	if pv.Cwd != "/home/u/eigenproj" {
		t.Errorf("eigen cwd from meta = %q", pv.Cwd)
	}
	if pv.Title != "do the thing" {
		t.Errorf("eigen title = %q", pv.Title)
	}
}

func TestPeekEigenUsesMetaTitle(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "s.eigen.jsonl", `{"role":"user","text":"do the thing"}`+"\n")
	_ = SaveMeta(p, SessionMeta{Dir: "/home/u/p", Title: "Renamed Session"})
	pv := Peek(SourceEigen, p)
	if pv.Title != "Renamed Session" {
		t.Errorf("user-set title should win over the derived one, got %q", pv.Title)
	}
}

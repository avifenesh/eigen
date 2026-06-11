package transcript

import (
	"os"
	"path/filepath"
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
	got := claudeDirFromPath("/home/u/.claude/projects/-home-u-projects-x/abc.jsonl")
	if got != "/home/u/projects/x" {
		t.Errorf("decoded dir = %q", got)
	}
	if claudeDirFromPath("/somewhere/else/abc.jsonl") != "" {
		t.Error("non-claude folder should decode to empty")
	}
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

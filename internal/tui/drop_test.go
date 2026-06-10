package tui

import "testing"

func TestNormalizeDroppedFileURI(t *testing.T) {
	got := normalizeDropped("file:///home/me/notes.md")
	if got != "/home/me/notes.md" {
		t.Fatalf("file:// URI: got %q", got)
	}
	// Percent-encoded space.
	got = normalizeDropped("file:///home/me/my%20file.txt")
	if got != "'/home/me/my file.txt'" {
		t.Fatalf("percent-decode + requote: got %q", got)
	}
	// file://host/path — host stripped.
	got = normalizeDropped("file://localhost/etc/hosts")
	if got != "/etc/hosts" {
		t.Fatalf("host strip: got %q", got)
	}
}

func TestNormalizeDroppedQuotedAndEscaped(t *testing.T) {
	if got := normalizeDropped("'/home/me/my file.txt'"); got != "'/home/me/my file.txt'" {
		t.Fatalf("quoted path: got %q", got)
	}
	if got := normalizeDropped(`/home/me/my\ file.txt`); got != "'/home/me/my file.txt'" {
		t.Fatalf("backslash-escaped: got %q", got)
	}
	if got := normalizeDropped("/usr/bin/eigen"); got != "/usr/bin/eigen" {
		t.Fatalf("plain abs path: got %q", got)
	}
}

func TestNormalizeDroppedMultiple(t *testing.T) {
	got := normalizeDropped("/a/one.go\n/a/two.go")
	if got != "/a/one.go /a/two.go" {
		t.Fatalf("newline-separated: got %q", got)
	}
	got = normalizeDropped("file:///a/one file:///b/two")
	if got != "/a/one /b/two" {
		t.Fatalf("space-separated file://: got %q", got)
	}
}

func TestNormalizeDroppedLeavesProse(t *testing.T) {
	// Ordinary pasted text must pass through untouched.
	for _, s := range []string{
		"please read the config and fix it",
		"a sentence with /a/path in the middle",
		"relative/path.go",
		"",
	} {
		if got := normalizeDropped(s); got != s {
			t.Fatalf("prose changed: %q → %q", s, got)
		}
	}
}

func TestLooksLikeDrop(t *testing.T) {
	yes := []string{"/abs/path", "~/home/path", "file:///x", "/a\n/b"}
	no := []string{"hello world", "fix /etc/hosts please", "relative/x", ""}
	for _, s := range yes {
		if !looksLikeDrop(s) {
			t.Errorf("should be a drop: %q", s)
		}
	}
	for _, s := range no {
		if looksLikeDrop(s) {
			t.Errorf("should NOT be a drop: %q", s)
		}
	}
}

package agent

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// APP-029: the fan-out report, BgTask note/error fields, and mutating-fanout
// answer excerpt all cap long text. A raw byte slice on a multibyte string
// splits a rune and emits invalid UTF-8; these guard the rune-safe truncation.

func TestFormatGroupReportKeepsValidUTF8(t *testing.T) {
	// "世" is 3 bytes; maxGroupResultBytes is not a multiple of 3, so a byte
	// slice would split the rune straddling the cap.
	big := strings.Repeat("世", maxGroupResultBytes) // 3× over the byte cap
	report := formatGroupReport([]childResult{{idx: 0, role: "researcher", result: big}})
	if !utf8.ValidString(report) {
		t.Fatalf("formatGroupReport emitted invalid UTF-8")
	}
	if !strings.Contains(report, "…[truncated]") {
		t.Fatalf("oversized result should be truncated:\n%s", report)
	}
}

func TestOneScreenKeepsValidUTF8(t *testing.T) {
	long := strings.Repeat("世", 800) // past the 600-rune cap
	got := oneScreen(long)
	if !utf8.ValidString(got) {
		t.Fatalf("oneScreen emitted invalid UTF-8")
	}
	if n := utf8.RuneCountInString(strings.TrimSuffix(got, "…")); n != 600 {
		t.Fatalf("oneScreen rune count = %d, want 600", n)
	}
	if got := oneScreen("héllo"); got != "héllo" {
		t.Fatalf("oneScreen mangled a short answer: %q", got)
	}
}

func TestNoteTruncationKeepsValidUTF8(t *testing.T) {
	long := strings.Repeat("世", 500)
	if got := sanitizeNote(long); !utf8.ValidString(got) {
		t.Fatalf("sanitizeNote emitted invalid UTF-8")
	} else if n := utf8.RuneCountInString(strings.TrimSuffix(got, "…")); n != 200 {
		t.Fatalf("sanitizeNote rune count = %d, want 200", n)
	}
	if got := truncateForNote(long); !utf8.ValidString(got) {
		t.Fatalf("truncateForNote emitted invalid UTF-8")
	} else if n := utf8.RuneCountInString(strings.TrimSuffix(got, "…")); n != 160 {
		t.Fatalf("truncateForNote rune count = %d, want 160", n)
	}
	// Short notes pass through unchanged (no spurious ellipsis), and Fields
	// flattening still applies.
	if got := sanitizeNote("héllo   wörld"); got != "héllo wörld" {
		t.Fatalf("sanitizeNote mangled a short note: %q", got)
	}
}

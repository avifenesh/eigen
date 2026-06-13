package retrieve

import (
	"strings"
	"testing"
)

func TestChunkFileWindows(t *testing.T) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line")
	}
	chunks := chunkFile("f.go", strings.Join(lines, "\n"))
	if len(chunks) < 2 {
		t.Fatalf("100 lines should make multiple windows, got %d", len(chunks))
	}
	// overlap: chunk 2 starts before chunk 1 ends.
	if chunks[1].Start > chunks[0].End {
		t.Fatalf("windows should overlap: c0 ends %d, c1 starts %d", chunks[0].End, chunks[1].Start)
	}
	// 1-based lines, monotonic.
	if chunks[0].Start != 1 {
		t.Fatalf("first chunk should start at line 1, got %d", chunks[0].Start)
	}
}

func TestChunkSkipsBlank(t *testing.T) {
	if c := chunkFile("e", "\n\n\n"); len(c) != 0 {
		t.Fatalf("all-blank file should yield no chunks, got %d", len(c))
	}
}

func TestDeniedPaths(t *testing.T) {
	for _, p := range []string{".env", ".env.local", "a/.git/config", "node_modules/x.js", "img.png", "b/.aws/creds"} {
		if !denied(p) {
			t.Errorf("%q should be denied from the index", p)
		}
	}
	for _, p := range []string{"main.go", "internal/llm/embed.go", "README.md", "src/app.ts"} {
		if denied(p) {
			t.Errorf("%q should be indexable", p)
		}
	}
}

func TestLooksTextual(t *testing.T) {
	if !looksTextual([]byte("package main\n")) {
		t.Error("source should be textual")
	}
	if looksTextual([]byte{0x7f, 0x45, 0x4c, 0x46, 0x00, 0x01}) {
		t.Error("binary (NUL byte) should be rejected")
	}
}

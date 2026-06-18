package orientation

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIngestSourceBuildsEpisodesAndProvenance(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	if err := EnsureHome(); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(home, ".eigen", "daemon", "sessions", "s1.jsonl")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, source,
		map[string]any{"Role": "user", "Text": "add custom orientation tests"},
		map[string]any{"Role": "assistant", "Text": "I will update the tests.", "ToolCalls": []map[string]any{{"ID": "c1", "Name": "write", "Arguments": map[string]any{"path": filepath.Join(cwd, "internal", "orientation", "orientation_test.go")}}, {"ID": "c2", "Name": "bash", "Arguments": map[string]any{"command": "go test ./internal/orientation"}}}},
	)
	if err := IngestSource(source, cwd, "eigen", "session", "main", "test", true); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Provenance(&out, cwd, "internal/orientation/orientation_test.go"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "PROVENANCE") || !strings.Contains(got, "add custom orientation tests") {
		t.Fatalf("unexpected provenance:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(projectDir(cwd), "graph.json")); err != nil {
		t.Fatalf("graph not written: %v", err)
	}
}

func TestHookFindsEigenSessionAndIngests(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	if err := EnsureHome(); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(home, ".eigen", "daemon", "sessions", "s42.jsonl")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, source,
		map[string]any{"Role": "user", "Text": "wire hook ingestion"},
		map[string]any{"Role": "assistant", "ToolCalls": []map[string]any{{"ID": "c1", "Name": "edit", "Arguments": map[string]any{"path": "internal/orientation/orientation.go"}}}},
	)
	meta := filepath.Join(home, ".eigen", "daemon", "sessions", "s42.meta.json")
	b, _ := json.Marshal(map[string]any{"id": "s42", "dir": cwd})
	if err := os.WriteFile(meta, b, 0o600); err != nil {
		t.Fatal(err)
	}
	payload := `{"event":"turn_done","session":"s42"}`
	if err := Hook(strings.NewReader(payload), ioDiscard{}, []string{"--runtime", "eigen"}); err != nil {
		t.Fatal(err)
	}
	eps, _ := loadEpisodesForCWD(cwd)
	if len(eps) != 1 || eps[0].Intent != "wire hook ingestion" || !containsString(eps[0].FilesTouched, "internal/orientation/orientation.go") {
		t.Fatalf("hook did not ingest expected episode: %+v", eps)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func writeJSONL(t *testing.T, path string, rows ...any) {
	t.Helper()
	var b bytes.Buffer
	for _, r := range rows {
		if err := json.NewEncoder(&b).Encode(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

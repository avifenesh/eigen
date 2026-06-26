package memory

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAppendAndRead(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := Open("/some/project")
	if err != nil {
		t.Fatal(err)
	}
	if s.Read() != "" {
		t.Fatal("fresh memory should be empty")
	}
	if err := s.Append("use go test ./... to run tests"); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("the build entrypoint is main.go"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(s.AdHocNotes(0), "\n")
	if !strings.Contains(got, "go test ./...") || !strings.Contains(got, "main.go") {
		t.Fatalf("notes not persisted:\n%s", got)
	}
	if s.Read() != "" {
		t.Fatalf("manual saves should wait in ad-hoc notes until Phase2, MEMORY.md got %q", s.Read())
	}
}

func TestAppendEnqueuesMemoryMaintenance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := Open("/some/project")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append("use go test ./... to run tests"); err != nil {
		t.Fatal(err)
	}
	idx, err := OpenIndex()
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	kinds := map[string]bool{}
	for {
		j, ok, err := idx.ClaimScope(baseName(s.Dir()), 60)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		kinds[j.Kind] = true
		if err := idx.Finish(j, nil); err != nil {
			t.Fatal(err)
		}
	}
	if !kinds[JobConsolidate] || !kinds[JobSummary] {
		t.Fatalf("manual memory append should enqueue downstream jobs, got %v", kinds)
	}
	if err := s.Append("use make build"); err != nil {
		t.Fatal(err)
	}
	j, ok, err := idx.ClaimScope(baseName(s.Dir()), 60)
	if err != nil || !ok || (j.Kind != JobConsolidate && j.Kind != JobSummary) {
		t.Fatalf("a later manual save should requeue maintenance after done jobs, got %+v ok=%v err=%v", j, ok, err)
	}
	if err := idx.Finish(j, nil); err != nil {
		t.Fatal(err)
	}
}

func TestSectionEmptyWhenNoNotes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")
	if s.Section() != "" {
		t.Fatal("no notes should yield an empty section")
	}
	_ = s.Append("a note")
	if s.Section() != "" {
		t.Fatal("ad-hoc notes should not inject before Phase2 summary")
	}
	if notes := s.AdHocNotes(0); len(notes) != 1 || !strings.Contains(notes[0], "a note") {
		t.Fatalf("ad-hoc note should be saved, got %v", notes)
	}
}

func TestSeparateProjectsSeparateFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, _ := Open("/project/a")
	b, _ := Open("/project/b")
	if a.Path() == b.Path() {
		t.Fatal("different projects must use different memory files")
	}
	_ = a.Append("only in a")
	if strings.Contains(strings.Join(b.AdHocNotes(0), "\n"), "only in a") {
		t.Fatal("project b should not see project a's notes")
	}
}

func TestAppendCollapsesNewlines(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")
	_ = s.Append("line one\nline two")
	got := strings.Join(s.AdHocNotes(0), "\n")
	if !strings.Contains(got, "line one line two") {
		t.Fatalf("a multiline note should collapse to one bullet:\n%s", got)
	}
}

func TestEmptyNoteRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")
	if err := s.Append("   "); err == nil {
		t.Fatal("blank note should error")
	}
}

func TestNilStoreSafe(t *testing.T) {
	var s *Store
	if s.Read() != "" || s.Section() != "" {
		t.Fatal("nil store reads should be empty")
	}
	if err := s.Append("x"); err == nil {
		t.Fatal("nil store append should error, not panic")
	}
}

func TestSnapshotAndRewrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")

	// Snapshot of a missing file is a no-op.
	if bak, err := s.Snapshot(); err != nil || bak != "" {
		t.Fatalf("snapshot of missing file: bak=%q err=%v", bak, err)
	}

	if err := s.Rewrite("- first note\n"); err != nil {
		t.Fatal(err)
	}
	before := s.Read()

	// Rewrite snapshots the old content, then replaces it.
	if err := s.Rewrite("- 2026-01-01 — consolidated\n"); err != nil {
		t.Fatal(err)
	}
	if got := s.Read(); !strings.Contains(got, "consolidated") {
		t.Fatalf("rewrite did not apply: %q", got)
	}
	baks := s.Backups()
	if len(baks) != 1 {
		t.Fatalf("want 1 backup, got %d", len(baks))
	}
	bak, _ := os.ReadFile(baks[0])
	if string(bak) != before {
		t.Fatalf("backup should hold the pre-rewrite content")
	}
}

func TestBackupPruning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")
	_ = s.Rewrite("- note\n")
	// Create more than maxBackups snapshots with distinct names.
	for i := 0; i < maxBackups+3; i++ {
		bak := fmt.Sprintf("%s.2026010%d-00000%d.bak", s.Path(), i%9, i)
		if err := os.WriteFile(bak, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s.pruneBackups()
	if got := len(s.Backups()); got > maxBackups {
		t.Fatalf("pruning should cap backups at %d, got %d", maxBackups, got)
	}
}

func TestAppendRedactsSecrets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")
	awsExample := "AKIA" + "IOSFODNN7EXAMPLE"
	secretValue := "abcdef" + "123456789012"
	if err := s.Append("the key is " + awsExample + " and api_key=" + secretValue + " works"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(s.AdHocNotes(0), "\n")
	if strings.Contains(got, awsExample) || strings.Contains(got, secretValue) {
		t.Fatalf("secrets must be redacted, got %q", got)
	}
	if !strings.Contains(got, Redacted) {
		t.Fatalf("redaction placeholder missing: %q", got)
	}
	if !strings.Contains(got, "api_key=") {
		t.Fatalf("key name should be preserved: %q", got)
	}
}

func TestSectionStalenessFraming(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")
	_ = s.writeSummary("a fact")
	sec := s.Section()
	if !strings.Contains(sec, "may be stale") || !strings.Contains(sec, "not instructions") {
		t.Fatalf("section should frame notes as possibly stale data: %q", sec)
	}
}

func TestGlobalStoreSeparateFromProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	proj, _ := Open("/some/project")
	glob, _ := OpenGlobal()
	if !glob.IsGlobal() || proj.IsGlobal() {
		t.Fatal("IsGlobal should distinguish the stores")
	}
	if proj.Path() == glob.Path() {
		t.Fatal("global and project stores must be different files")
	}
	_ = proj.Append("project fact")
	_ = glob.Append("global rule")
	if strings.Contains(strings.Join(proj.AdHocNotes(0), "\n"), "global rule") || strings.Contains(strings.Join(glob.AdHocNotes(0), "\n"), "project fact") {
		t.Fatal("global and project notes must not bleed into each other")
	}
}

func TestGlobalSectionLabel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	glob, _ := OpenGlobal()
	_ = glob.writeSummary("user commits often")
	sec := glob.Section()
	if !strings.Contains(sec, "Global memory") || !strings.Contains(sec, "cross-project") {
		t.Fatalf("global section should be labeled as cross-project: %q", sec)
	}
}

func TestGlobalUserProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	glob, _ := OpenGlobal()
	if err := glob.WriteUserProfile("I prefer concise summaries"); err != nil {
		t.Fatal(err)
	}
	if got := glob.UserProfile(); !strings.Contains(got, "I prefer concise summaries") {
		t.Fatalf("profile not persisted: %q", got)
	}
	sec := glob.Section()
	if !strings.Contains(sec, "User profile") || !strings.Contains(sec, "I prefer concise summaries") {
		t.Fatalf("global section should inject user profile: %q", sec)
	}
	if err := glob.WriteUserProfile(" "); err != nil {
		t.Fatal(err)
	}
	if got := glob.UserProfile(); got != "" {
		t.Fatalf("empty profile write should clear USER.md, got %q", got)
	}
}

func TestSectionsCombinesGlobalThenProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	proj, _ := Open("/p")
	glob, _ := OpenGlobal()
	_ = proj.writeSummary("PROJECTNOTE")
	_ = glob.writeSummary("GLOBALNOTE")
	combined := Sections(glob, proj)
	gi := strings.Index(combined, "GLOBALNOTE")
	pi := strings.Index(combined, "PROJECTNOTE")
	if gi < 0 || pi < 0 || gi > pi {
		t.Fatalf("Sections should place global before project: %q", combined)
	}
	// Empty stores contribute nothing.
	if Sections(nil, nil) != "" {
		t.Fatal("Sections of nil stores should be empty")
	}
}

func TestWorkspaceListReadSearch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")
	if err := s.writeSummary("summary mentions vector index"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAdHocNote("manual note mentions playwright", time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	files, err := s.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(files, "\n")
	if !strings.Contains(joined, "memory_summary.md") || !strings.Contains(joined, "extensions/ad_hoc/notes/") {
		t.Fatalf("workspace files should include summary and ad-hoc note, got %v", files)
	}
	content, err := s.ReadRelative("memory_summary.md")
	if err != nil || !strings.Contains(content, "vector index") {
		t.Fatalf("read summary: content=%q err=%v", content, err)
	}
	hits, err := s.Search("playwright", 10)
	if err != nil || len(hits) != 1 || !strings.Contains(hits[0].Path, "extensions/ad_hoc/notes/") {
		t.Fatalf("search should find ad-hoc note, hits=%+v err=%v", hits, err)
	}
}

// TestUserProfileSectionsRoundTrip verifies USER.md's two sections — the
// eigen-maintained learned block + the user's free-form area — are edited
// independently without clobbering each other (APP-054).
func TestUserProfileSectionsRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	glob, _ := OpenGlobal()

	// User writes their section; no learned block yet → no markers.
	if err := glob.WriteUserProfile("I prefer terse answers."); err != nil {
		t.Fatal(err)
	}
	if got := glob.UserProfile(); strings.Contains(got, learnedProfileBegin) {
		t.Fatalf("user-only profile should carry no markers: %q", got)
	}

	// Eigen auto-maintains the learned block — user section preserved.
	if err := glob.SetLearnedProfile("Works in Go and Svelte."); err != nil {
		t.Fatal(err)
	}
	if got := glob.UserProfileLearned(); got != "Works in Go and Svelte." {
		t.Fatalf("learned = %q", got)
	}
	if got := glob.UserProfileUser(); got != "I prefer terse answers." {
		t.Fatalf("user section clobbered by learned write: %q", got)
	}
	if full := glob.UserProfile(); !strings.Contains(full, "Works in Go and Svelte.") || !strings.Contains(full, "I prefer terse answers.") {
		t.Fatalf("full profile must inject BOTH: %q", full)
	}

	// User edits their section again — learned block preserved.
	if err := glob.WriteUserProfile("I prefer terse answers. And tests."); err != nil {
		t.Fatal(err)
	}
	if got := glob.UserProfileLearned(); got != "Works in Go and Svelte." {
		t.Fatalf("learned clobbered by user write: %q", got)
	}
}

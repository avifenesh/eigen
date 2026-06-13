package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
)

func gitInit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	run("commit", "--allow-empty", "-q", "-m", "init")
	// realpath (macOS /var → /private/var etc.) so repo-root checks match.
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	return dir
}

func TestPrecheckRefusesNonGit(t *testing.T) {
	_, err := precheckMutatingFanout(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "git repo") {
		t.Fatalf("non-git should be refused, got %v", err)
	}
}

func TestPrecheckRefusesDirty(t *testing.T) {
	dir := gitInit(t)
	os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("x"), 0o644)
	_, err := precheckMutatingFanout(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "clean") {
		t.Fatalf("dirty tree should be refused, got %v", err)
	}
}

func TestPrecheckAcceptsCleanRepo(t *testing.T) {
	dir := gitInit(t)
	st, err := precheckMutatingFanout(context.Background(), dir)
	if err != nil {
		t.Fatalf("clean repo should pass: %v", err)
	}
	if st.root != dir {
		t.Fatalf("root = %s, want %s", st.root, dir)
	}
}

// writeProvider emits one write tool call (creating fileName with body) then a
// final answer. Concurrency-safe for parallel children.
type writeProvider struct {
	fileName string
	body     string
}

func (w *writeProvider) Name() string    { return "wp" }
func (w *writeProvider) ModelID() string { return "wp" }
func (w *writeProvider) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	// First call (no prior tool result) → emit the write; otherwise finish.
	hasToolResult := false
	for _, m := range req.Messages {
		if m.Role == llm.RoleTool {
			hasToolResult = true
		}
	}
	if !hasToolResult {
		args, _ := json.Marshal(map[string]string{"path": w.fileName, "content": w.body})
		return &llm.Response{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "write", Arguments: args}}}, nil
	}
	return &llm.Response{Text: "wrote " + w.fileName}, nil
}

func mutTestAgent(t *testing.T, dir string, wp llm.Provider) *Agent {
	return &Agent{
		Provider:   wp,
		Tools:      mustReg(t),
		Perm:       PermAuto,
		SessionDir: dir,
		WorktreeTools: func(wt string) *tool.Registry {
			policy := tool.NewPolicy(wt)
			r, err := tool.NewRegistry(tool.Read(policy), tool.Write(policy), tool.Edit(policy), tool.List(policy))
			if err != nil {
				t.Fatal(err)
			}
			return r
		},
	}
}

func TestTaskGroupMutatingAppliesPatch(t *testing.T) {
	dir := gitInit(t)
	a := mutTestAgent(t, dir, &writeProvider{fileName: "new.txt", body: "hello from child\n"})

	approved := false
	approve := func(_ context.Context, summary string, diff []byte) (bool, error) {
		approved = true
		if !strings.Contains(string(diff), "hello from child") {
			t.Fatalf("approval diff should contain the child's content:\n%s", diff)
		}
		return true, nil
	}

	report, err := a.TaskGroupMutating(context.Background(), []GroupSubtask{
		{Task: "create new.txt with a greeting"},
	}, 1, approve)
	if err != nil {
		t.Fatalf("mutating fan-out errored: %v", err)
	}
	if !approved {
		t.Fatal("apply should have requested approval")
	}
	// The file should now exist in the MAIN repo.
	got, rerr := os.ReadFile(filepath.Join(dir, "new.txt"))
	if rerr != nil {
		t.Fatalf("applied file missing from main tree: %v\nreport:\n%s", rerr, report)
	}
	if string(got) != "hello from child\n" {
		t.Fatalf("applied content = %q", got)
	}
	if !strings.Contains(report, "Applied 1 patch") {
		t.Fatalf("report should confirm apply:\n%s", report)
	}
}

func TestTaskGroupMutatingDenyLeavesTreeClean(t *testing.T) {
	dir := gitInit(t)
	a := mutTestAgent(t, dir, &writeProvider{fileName: "no.txt", body: "x"})
	deny := func(context.Context, string, []byte) (bool, error) { return false, nil }
	report, err := a.TaskGroupMutating(context.Background(), []GroupSubtask{{Task: "make a file"}}, 1, deny)
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "no.txt")); !os.IsNotExist(statErr) {
		t.Fatal("denied apply must not touch the main tree")
	}
	if !strings.Contains(report, "denied") {
		t.Fatalf("report should note denial:\n%s", report)
	}
	// Main tree still clean (no leftover worktrees / files).
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, _ := cmd.Output()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("tree should be clean after denial, got:\n%s", out)
	}
}

func TestTaskGroupMutatingNoWorktreeToolsErrors(t *testing.T) {
	a := &Agent{Provider: &writeProvider{}, Tools: mustReg(t), Perm: PermAuto, SessionDir: gitInit(t)}
	if _, err := a.TaskGroupMutating(context.Background(), []GroupSubtask{{Task: "x"}}, 1, nil); err == nil {
		t.Fatal("nil WorktreeTools should disable mutating fan-out")
	}
}

package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func bashCall(t *testing.T, def Definition, args map[string]any) string {
	t.Helper()
	b, _ := json.Marshal(args)
	out, err := def.Run(context.Background(), b)
	if err != nil {
		t.Fatalf("bash run: %v", err)
	}
	return out
}

func TestBashBackgroundReturnsHandleAndKeepsRunning(t *testing.T) {
	shells := NewShellRegistry()
	bash := BashWithShells(nil, shells, nil)
	out := bashCall(t, bash, map[string]any{"command": "echo hello; sleep 2; echo bye", "background": true})
	if !strings.Contains(out, "started background shell shell-1") {
		t.Fatalf("background should return a handle, got %q", out)
	}
	// It returned immediately (didn't wait 2s).
	sh := shells.Get("shell-1")
	if sh == nil || !sh.running() {
		t.Fatal("shell should be registered + running")
	}
	// Poll output (bash_output).
	bo := BashOutput(shells)
	deadline := time.Now().Add(3 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		got += bashCall(t, bo, map[string]any{"id": "shell-1"})
		if strings.Contains(got, "hello") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(got, "hello") {
		t.Fatalf("bash_output should show the shell's output, got %q", got)
	}
	// Wait for exit.
	for i := 0; i < 40 && shells.Get("shell-1").running(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	full := bashCall(t, bo, map[string]any{"id": "shell-1", "full": true})
	if !strings.Contains(full, "bye") || !strings.Contains(full, "exited") {
		t.Fatalf("after exit, full output should have bye + exited, got %q", full)
	}
}

func TestKillShellStops(t *testing.T) {
	shells := NewShellRegistry()
	bash := BashWithShells(nil, shells, nil)
	bashCall(t, bash, map[string]any{"command": "sleep 30", "background": true})
	if !shells.Get("shell-1").running() {
		t.Fatal("should be running")
	}
	ks := KillShell(shells)
	out := bashCall(t, ks, map[string]any{"id": "shell-1"})
	if !strings.Contains(out, "killing shell-1") {
		t.Fatalf("kill_shell: %q", out)
	}
	// It dies promptly.
	for i := 0; i < 40 && shells.Get("shell-1").running(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if shells.Get("shell-1").running() {
		t.Fatal("shell should be killed, still running")
	}
}

func TestBashDetachMidRun(t *testing.T) {
	shells := NewShellRegistry()
	ch := make(chan struct{}, 1)
	bash := BashWithShells(nil, shells, func() <-chan struct{} { return ch })
	// Fire the detach signal shortly after start.
	go func() { time.Sleep(300 * time.Millisecond); ch <- struct{}{} }()
	out := bashCall(t, bash, map[string]any{"command": "echo start; sleep 5; echo end"})
	if !strings.Contains(out, "backgrounded as shell shell-1") {
		t.Fatalf("mid-run detach should background + return a handle, got %q", out)
	}
	// The early output transferred; it's still running.
	if !shells.Get("shell-1").running() {
		t.Fatal("detached shell should still be running")
	}
	shells.Get("shell-1").kill()
}

func TestPlainBashStillWorks(t *testing.T) {
	def := Bash(nil) // no registry: historical synchronous behavior
	out := bashCall(t, def, map[string]any{"command": "echo ok"})
	if !strings.Contains(out, "ok") {
		t.Fatalf("plain bash: %q", out)
	}
}

func TestStatusBlockSurfacesRunningShells(t *testing.T) {
	shells := NewShellRegistry()
	if shells.StatusBlock() != "" {
		t.Fatal("no shells → empty status block")
	}
	bash := BashWithShells(nil, shells, nil)
	bashCall(t, bash, map[string]any{"command": "sleep 5", "background": true})
	sb := shells.StatusBlock()
	if !strings.Contains(sb, "shell-1 (running)") || !strings.Contains(sb, "bash_output") {
		t.Fatalf("status block should surface the running shell + how to poll, got %q", sb)
	}
	shells.Get("shell-1").kill()
}

func TestDetachChannelOnlyLiveDuringBash(t *testing.T) {
	shells := NewShellRegistry()
	// Pre-make the channel so the test goroutine and the bash call share it
	// without a race (mirrors the agent storing one channel per call).
	ch := make(chan struct{})
	detach := func() <-chan struct{} { return ch }
	bash := BashWithShells(nil, shells, detach)

	// Fire detach shortly after the command starts (while runBash selects).
	go func() {
		time.Sleep(300 * time.Millisecond)
		select {
		case ch <- struct{}{}: // succeeds: runBash is selecting
		case <-time.After(2 * time.Second):
			t.Error("detach send found no receiver — runBash not selecting")
		}
	}()
	out := bashCall(t, bash, map[string]any{"command": "echo go; sleep 5; echo done"})
	if !strings.Contains(out, "backgrounded as shell") {
		t.Fatalf("on-demand detach should background the running command, got %q", out)
	}
	if !shells.Get("shell-1").running() {
		t.Fatal("detached command should still be running")
	}
	shells.Get("shell-1").kill()
}

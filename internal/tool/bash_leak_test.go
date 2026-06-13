package tool

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestBashTimeoutKillsChildProcessGroup pins the process-leak fix: when a
// bash command times out, children it spawned (background jobs, subshells)
// must be killed too, not orphaned. The command writes its grandchild's PID to
// a file, then the grandchild sleeps long; after the timeout we assert the
// grandchild is gone.
func TestBashTimeoutKillsChildProcessGroup(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	pidFile := t.TempDir() + "/child.pid"
	// Background a long sleep, record its pid, then the foreground bash also
	// waits — so the whole group is alive when the timeout fires.
	cmd := "sleep 60 & echo $! > " + pidFile + "; wait"
	b := Bash(nil)
	args, _ := json.Marshal(map[string]any{"command": cmd, "timeout_seconds": 1})

	start := time.Now()
	if _, err := b.Run(context.Background(), args); err != nil {
		t.Fatalf("run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout should fire ~1s, took %v", elapsed)
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Skipf("child pid not recorded: %v", err)
	}
	pid := strings.TrimSpace(string(data))
	if pid == "" {
		t.Skip("no child pid")
	}
	// Give the kill a moment to propagate, then check the child is dead.
	time.Sleep(300 * time.Millisecond)
	// kill -0 returns success only if the process still exists.
	if err := exec.Command("kill", "-0", pid).Run(); err == nil {
		// Clean up the leak we just detected, then fail.
		_ = exec.Command("kill", "-9", pid).Run()
		t.Fatalf("backgrounded child %s survived the timeout — process group not killed", pid)
	}
}

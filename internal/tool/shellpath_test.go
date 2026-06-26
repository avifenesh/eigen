package tool

import (
	"os/exec"
	"testing"
)

// shellPath must resolve to SOMETHING that exists + runs `-c`, so the bash tool
// works even where bash isn't on PATH at the historical location.
func TestShellPathResolvesRunnable(t *testing.T) {
	p := shellPath()
	if p == "" {
		t.Fatal("shellPath returned empty")
	}
	// It should actually execute a trivial command (PATH name or absolute path).
	out, err := exec.Command(p, "-c", "echo ok").Output()
	if err != nil {
		t.Fatalf("resolved shell %q failed to run: %v", p, err)
	}
	if string(out) != "ok\n" {
		t.Fatalf("resolved shell %q produced %q", p, out)
	}
}

func TestShellCommandUsesResolved(t *testing.T) {
	cmd := shellCommand("true")
	if cmd.Path == "" || len(cmd.Args) < 2 || cmd.Args[1] != "-c" {
		t.Fatalf("shellCommand built unexpected cmd: path=%q args=%v", cmd.Path, cmd.Args)
	}
}

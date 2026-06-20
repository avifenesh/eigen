package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseBinaryDoesNotExposeSmokeCommands(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "eigen-release")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("release binary must build: %v\n%s", err, out)
	}
	for _, arg := range []string{"app-smoke", "tui-smoke"} {
		t.Run(arg, func(t *testing.T) {
			cmd := exec.Command(bin, arg)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("release binary %s should fail explicitly, got success:\n%s", arg, out)
			}
			s := string(out)
			if !strings.Contains(s, "smoke commands require a smoke-tagged test helper") || strings.Contains(s, arg+" action=") || strings.Contains(s, arg+" openApp=") || strings.Contains(s, "smoke answer") {
				t.Fatalf("release binary did not fail safely for %s:\n%s", arg, s)
			}
		})
	}
}

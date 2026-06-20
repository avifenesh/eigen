package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLISmokeVersionAndTheme(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{name: "version flag", args: []string{"--version"}, want: []string{"eigen"}},
		{name: "version command", args: []string{"version"}, want: []string{"eigen"}},
		{name: "theme swatch", args: []string{"theme"}, want: []string{"base", "surface", "Accent", "Sel"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], append([]string{"-test.run=TestCLIHelperProcess", "--"}, tc.args...)...)
			cmd.Env = append(os.Environ(), "GO_WANT_CLI_HELPER_PROCESS=1", "HOME="+t.TempDir())
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v failed: %v\n%s", tc.args, err, out)
			}
			s := string(out)
			for _, want := range tc.want {
				if !strings.Contains(s, want) {
					t.Fatalf("%v output missing %q:\n%s", tc.args, want, s)
				}
			}
		})
	}
}

func TestProductionSmokeCommandsFailExplicitly(t *testing.T) {
	if smokeBuild {
		t.Skip("smoke-tagged helper intentionally enables smoke commands")
	}
	for _, arg := range []string{"app-smoke", "tui-smoke"} {
		t.Run(arg, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=TestCLIHelperProcess", "--", arg)
			cmd.Env = append(os.Environ(), "GO_WANT_CLI_HELPER_PROCESS=1", "HOME="+t.TempDir())
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("%s should fail explicitly in the normal test binary, got success:\n%s", arg, out)
			}
			if s := string(out); !strings.Contains(s, "smoke commands require a smoke-tagged test helper") || strings.Contains(s, "eigen ·") || strings.Contains(s, "session saved") {
				t.Fatalf("%s did not fail explicitly without launching agent/app:\n%s", arg, out)
			}
		})
	}
}

func TestSmokeTaggedBinaryBuilds(t *testing.T) {
	out := filepath.Join(t.TempDir(), "eigen-smoke")
	cmd := exec.Command("go", "build", "-buildvcs=false", "-tags", "smoke", "-o", out, ".")
	cmd.Env = os.Environ()
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("smoke-tagged binary must build: %v\n%s", err, b)
	}
}

func TestCLIHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_CLI_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			os.Args = append([]string{args[0]}, args[i+1:]...)
			main()
			return
		}
	}
	t.Fatal("missing -- separator")
}

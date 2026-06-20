package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func smokeTestCommand(t *testing.T, arg string) *exec.Cmd {
	t.Helper()
	bin := os.Args[0]
	if os.Getenv("EIGEN_SMOKE_HELPER") != "1" {
		out := t.TempDir() + "/eigen-smoke.test"
		build := exec.Command("go", "test", "-tags", "smoke", "-c", "-o", out, ".")
		if b, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build smoke helper: %v\n%s", err, b)
		}
		bin = out
	}
	return exec.Command(bin, "-test.run=TestCLIHelperProcess", "--", arg)
}

type ptyHarness struct {
	t          *testing.T
	ptmx       *os.File
	cmd        *exec.Cmd
	done       chan error
	readDone   chan struct{}
	buf        bytes.Buffer
	repliedBG  bool
	repliedCPR bool
}

func startPTYHarness(t *testing.T, cmd *exec.Cmd, cols, rows uint16) *ptyHarness {
	t.Helper()
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		t.Skipf("pty unavailable: %v", err)
	}
	h := &ptyHarness{t: t, ptmx: ptmx, cmd: cmd, done: make(chan error, 1), readDone: make(chan struct{})}
	go func() { h.done <- cmd.Wait() }()
	go func() {
		defer close(h.readDone)
		_, _ = h.buf.ReadFrom(ptmx)
	}()
	return h
}

func (h *ptyHarness) close() {
	_ = h.ptmx.Close()
}

func (h *ptyHarness) answerProbesOnce() string {
	probe := h.buf.String()
	if !h.repliedBG && strings.Contains(probe, "\x1b]11;?") {
		_, _ = h.ptmx.Write([]byte("\x1b]11;rgb:0000/0000/0000\x1b\\"))
		h.repliedBG = true
	}
	if !h.repliedCPR && strings.Contains(probe, "\x1b[6n") {
		_, _ = h.ptmx.Write([]byte("\x1b[1;1R"))
		h.repliedCPR = true
	}
	return probe
}

func (h *ptyHarness) outputAfterExit(err error) string {
	_ = h.ptmx.Close()
	<-h.readDone
	if err != nil {
		h.t.Fatalf("pty command failed: %v\n%s", err, h.buf.String())
	}
	return h.buf.String()
}

func TestPTYSmokeAppShellKeyboardNavigation(t *testing.T) {
	cmd := smokeTestCommand(t, "app-smoke")
	cmd.Env = append(os.Environ(), "GO_WANT_CLI_HELPER_PROCESS=1", "EIGEN_APP_SMOKE=1", "HOME="+t.TempDir(), "TERM=xterm-256color")
	h := startPTYHarness(t, cmd, 120, 30)
	defer h.close()
	deadline := time.After(5 * time.Second)
	sentNav := false
	for {
		select {
		case err := <-h.done:
			out := h.outputAfterExit(err)
			for _, want := range []string{"models", "plugins", "sessions", "app-smoke action=0"} {
				if !strings.Contains(out, want) {
					t.Fatalf("app shell keyboard smoke missing %q:\n%s", want, out)
				}
			}
			return
		case <-time.After(100 * time.Millisecond):
			probe := h.answerProbesOnce()
			if !sentNav && (strings.Contains(probe, "eigen") || strings.Contains(probe, "home") || len(probe) > 128) {
				_, _ = h.ptmx.Write([]byte(":mod\r"))
				time.Sleep(80 * time.Millisecond)
				_, _ = h.ptmx.Write([]byte("gx"))
				time.Sleep(80 * time.Millisecond)
				_, _ = h.ptmx.Write([]byte("gs"))
				time.Sleep(80 * time.Millisecond)
				_, _ = h.ptmx.Write([]byte("q"))
				sentNav = true
			}
		case <-deadline:
			_ = cmd.Process.Kill()
			t.Fatalf("app shell under pty timed out; output so far:\n%s", h.buf.String())
		}
	}
}

func TestPTYChatTUISmokeQuit(t *testing.T) {
	beforeG := settledRootGoroutines(t)
	cmd := smokeTestCommand(t, "tui-smoke")
	cmd.Env = append(os.Environ(), "GO_WANT_CLI_HELPER_PROCESS=1", "EIGEN_TUI_SMOKE=1", "HOME="+t.TempDir(), "TERM=xterm-256color")
	h := startPTYHarness(t, cmd, 120, 30)
	defer h.close()
	deadline := time.After(10 * time.Second)
	sent := false
	for {
		select {
		case err := <-h.done:
			out := h.outputAfterExit(err)
			for _, want := range []string{"eigen", "tui-smoke openApp=false"} {
				if !strings.Contains(out, want) {
					t.Fatalf("chat TUI PTY smoke missing %q:\n%s", want, out)
				}
			}
			assertRootGoroutineBound(t, beforeG, 4, "chat TUI PTY smoke")
			return
		case <-time.After(100 * time.Millisecond):
			probe := h.answerProbesOnce()
			if !sent && h.repliedBG && h.repliedCPR && (strings.Contains(probe, "eigen") || len(probe) > 128) {
				_, _ = h.ptmx.Write([]byte{0x03})
				sent = true
			}
		case <-deadline:
			_ = cmd.Process.Kill()
			t.Fatalf("chat TUI under PTY timed out; output so far:\n%s", h.buf.String())
		}
	}
}

func TestPTYReleaseAppShellLongerSoak(t *testing.T) {
	beforeG := settledRootGoroutines(t)
	bin := filepath.Join(t.TempDir(), "eigen-release")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build release binary: %v\n%s", err, out)
	}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir(), "TERM=xterm-256color")
	h := startPTYHarness(t, cmd, 120, 30)
	defer h.close()
	deadline := time.After(10 * time.Second)
	sent := false
	for {
		select {
		case err := <-h.done:
			out := h.outputAfterExit(err)
			for _, want := range []string{"home", "live", "projects", "machines", "sessions", "config", "skills", "models", "providers", "memory", "crons", "plugins"} {
				if !strings.Contains(out, want) {
					t.Fatalf("release app PTY soak missing %q:\n%s", want, out)
				}
			}
			assertRootGoroutineBound(t, beforeG, 4, "release app PTY soak")
			return
		case <-time.After(100 * time.Millisecond):
			probe := h.answerProbesOnce()
			if !sent && (strings.Contains(probe, "eigen") || strings.Contains(probe, "home") || len(probe) > 128) {
				for i := 0; i < 4; i++ {
					for _, k := range []string{"gh", "gl", "gp", "ge", "gs", "gc", "gk", "gm", "gv", "gy", "gr", "gx"} {
						_, _ = h.ptmx.Write([]byte(k))
						time.Sleep(20 * time.Millisecond)
					}
					_, _ = h.ptmx.Write([]byte(":mod\r"))
					time.Sleep(20 * time.Millisecond)
					_, _ = h.ptmx.Write([]byte("gx"))
					time.Sleep(20 * time.Millisecond)
				}
				_, _ = h.ptmx.Write([]byte("q"))
				sent = true
			}
		case <-deadline:
			_ = cmd.Process.Kill()
			t.Fatalf("release app PTY soak timed out; output so far:\n%s", h.buf.String())
		}
	}
}

func TestPTYAppShellNavigationSoak(t *testing.T) {
	beforeG := settledRootGoroutines(t)
	cmd := smokeTestCommand(t, "app-smoke")
	cmd.Env = append(os.Environ(), "GO_WANT_CLI_HELPER_PROCESS=1", "EIGEN_APP_SMOKE=1", "HOME="+t.TempDir(), "TERM=xterm-256color")
	h := startPTYHarness(t, cmd, 120, 30)
	defer h.close()
	deadline := time.After(6 * time.Second)
	sent := false
	for {
		select {
		case err := <-h.done:
			out := h.outputAfterExit(err)
			for _, want := range []string{"home", "live", "projects", "machines", "sessions", "config", "skills", "models", "providers", "memory", "crons", "plugins", "app-smoke action=0"} {
				if !strings.Contains(out, want) {
					t.Fatalf("app PTY navigation soak missing %q:\n%s", want, out)
				}
			}
			assertRootGoroutineBound(t, beforeG, 4, "app PTY navigation soak")
			return
		case <-time.After(100 * time.Millisecond):
			probe := h.answerProbesOnce()
			if !sent && (strings.Contains(probe, "eigen") || strings.Contains(probe, "home") || len(probe) > 128) {
				for _, k := range []string{"gh", "gl", "gp", "ge", "gs", "gc", "gk", "gm", "gv", "gy", "gr", "gx"} {
					_, _ = h.ptmx.Write([]byte(k))
					time.Sleep(25 * time.Millisecond)
				}
				for i := 0; i < 3; i++ {
					_, _ = h.ptmx.Write([]byte(fmt.Sprintf(":mod\r")))
					time.Sleep(25 * time.Millisecond)
					_, _ = h.ptmx.Write([]byte("gx"))
					time.Sleep(25 * time.Millisecond)
				}
				_, _ = h.ptmx.Write([]byte("q"))
				sent = true
			}
		case <-deadline:
			_ = cmd.Process.Kill()
			t.Fatalf("app PTY navigation soak timed out; output so far:\n%s", h.buf.String())
		}
	}
}

func TestPTYSmokeVersionCommand(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestCLIHelperProcess", "--", "version")
	cmd.Env = append(os.Environ(), "GO_WANT_CLI_HELPER_PROCESS=1", "HOME="+t.TempDir(), "TERM=xterm-256color")
	h := startPTYHarness(t, cmd, 100, 30)
	defer h.close()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case err := <-h.done:
			out := h.outputAfterExit(err)
			if !strings.Contains(out, "eigen") {
				t.Fatalf("pty version output missing product name:\n%s", out)
			}
			return
		case <-time.After(100 * time.Millisecond):
			h.answerProbesOnce()
		case <-deadline:
			_ = cmd.Process.Kill()
			t.Fatalf("version under pty timed out; output so far:\n%s", h.buf.String())
		}
	}
}

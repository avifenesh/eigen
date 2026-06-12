// Package speech provides optional text-to-speech for eigen: it pipes text to
// a configurable command's stdin. Detection prefers the local Kokoro ONNX
// stack (high quality) over espeak-ng/piper/say. It is best effort — a
// missing or failing TTS command never affects the session.
package speech

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Speaker speaks text by piping it to an external command's stdin. The zero
// value is a no-op; use Detect to construct one.
type Speaker struct {
	argv []string

	mu  sync.Mutex
	cur *exec.Cmd
}

// Detect resolves a TTS command: EIGEN_TTS_CMD (run via `sh -c`, text on stdin)
// wins; otherwise the local Kokoro stack when present (kokoro_stdin.py — high
// quality, reads stdin), then espeak-ng/piper/say. NOTE: `readd speak` is NOT
// a candidate — it reads the latest Claude transcript, not stdin.
func Detect() *Speaker {
	if c := strings.TrimSpace(os.Getenv("EIGEN_TTS_CMD")); c != "" {
		return &Speaker{argv: []string{"sh", "-c", c}}
	}
	if argv := detectKokoro(); argv != nil {
		return &Speaker{argv: argv}
	}
	for _, bin := range []string{"espeak-ng", "piper", "say"} {
		if p, err := exec.LookPath(bin); err == nil {
			return &Speaker{argv: []string{p}}
		}
	}
	return &Speaker{}
}

// Available reports whether a TTS command was resolved.
func (s *Speaker) Available() bool { return s != nil && len(s.argv) > 0 }

// detectKokoro finds the local Kokoro ONNX stack: the kokoro_stdin.py script
// (codex-desktop-linux read-aloud backend — reads stdin, streams to aplay), a
// python that can import kokoro_onnx (the readd venv has it), and the model +
// voices files. Returns the full argv with env baked in, or nil.
func detectKokoro() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	script := ""
	for _, p := range []string{
		filepath.Join(home, "projects", "codex-desktop-linux", "codex-app", "resources", "read-aloud", "kokoro_stdin.py"),
		filepath.Join(home, "projects", "codex-desktop-linux", "linux-features", "read-aloud", "bin", "kokoro_stdin.py"),
	} {
		if _, err := os.Stat(p); err == nil {
			script = p
			break
		}
	}
	if script == "" {
		return nil
	}
	model := filepath.Join(home, ".local", "share", "kokoro", "kokoro-v1.0.onnx")
	voices := filepath.Join(home, ".local", "share", "kokoro", "voices-v1.0.bin")
	if _, err := os.Stat(model); err != nil {
		return nil
	}
	if _, err := os.Stat(voices); err != nil {
		return nil
	}
	// Python with kokoro_onnx: the readd venv is the known-good one; fall
	// back to system python3 (may or may not have the package — Speak fails
	// soft either way).
	python := filepath.Join(home, "projects", "tfqol", "readd", ".venv", "bin", "python3")
	if _, err := os.Stat(python); err != nil {
		p, err := exec.LookPath("python3")
		if err != nil {
			return nil
		}
		python = p
	}
	// sh -c keeps env assignment simple and Stop() still kills the group via
	// the speaker's process handle.
	return []string{"sh", "-c",
		"CODEX_LINUX_READ_ALOUD_KOKORO_MODEL=" + model +
			" CODEX_LINUX_READ_ALOUD_KOKORO_VOICES=" + voices +
			" exec " + python + " " + script}
}

// Speak speaks text asynchronously, interrupting any in-progress speech. It
// returns immediately; speech continues in the background.
func (s *Speaker) Speak(text string) {
	if !s.Available() || strings.TrimSpace(text) == "" {
		return
	}
	go func() { _ = s.speak(text) }()
}

// Stop interrupts any in-progress speech.
func (s *Speaker) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cur := s.cur
	s.cur = nil
	s.mu.Unlock()
	if cur != nil && cur.Process != nil {
		_ = cur.Process.Kill()
	}
}

// speak runs the TTS command synchronously, replacing any current speech. It is
// the unit-testable core of Speak.
func (s *Speaker) speak(text string) error {
	if !s.Available() {
		return nil
	}
	s.Stop()
	cmd := exec.Command(s.argv[0], s.argv[1:]...)
	cmd.Stdin = strings.NewReader(text)
	s.mu.Lock()
	s.cur = cmd
	s.mu.Unlock()
	err := cmd.Run()
	s.mu.Lock()
	if s.cur == cmd {
		s.cur = nil
	}
	s.mu.Unlock()
	return err
}

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

// Argv exposes the resolved TTS command (empty when unavailable) so other
// speech paths (voice mode's cancelable TTS) use the SAME detection — one
// voice everywhere, not Kokoro for read-aloud and espeak for conversation.
func (s *Speaker) Argv() []string {
	if s == nil {
		return nil
	}
	return s.argv
}

// detectKokoro assembles eigen's own Kokoro ONNX stack: the EMBEDDED
// kokoro_stdin.py (vendored — no external checkout needed), the model +
// voices files, and a python3 that can import kokoro_onnx. Everything is
// overridable: EIGEN_KOKORO_MODEL/VOICES point at the files,
// EIGEN_KOKORO_PYTHON picks the interpreter. Returns the full argv with env
// baked in, or nil when a piece is missing.
func detectKokoro() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	model := firstExisting(
		os.Getenv("EIGEN_KOKORO_MODEL"),
		filepath.Join(home, ".eigen", "kokoro", "kokoro-v1.0.onnx"),
		filepath.Join(home, ".local", "share", "kokoro", "kokoro-v1.0.onnx"),
	)
	voices := firstExisting(
		os.Getenv("EIGEN_KOKORO_VOICES"),
		filepath.Join(home, ".eigen", "kokoro", "voices-v1.0.bin"),
		filepath.Join(home, ".local", "share", "kokoro", "voices-v1.0.bin"),
	)
	if model == "" || voices == "" {
		return nil
	}
	python := kokoroPython(home)
	if python == "" {
		return nil
	}
	script, err := materializeKokoroScript(home)
	if err != nil {
		return nil
	}
	// sh -c keeps env assignment simple and Stop() still kills the group via
	// the speaker's process handle.
	return []string{"sh", "-c",
		"EIGEN_KOKORO_MODEL=" + model +
			" EIGEN_KOKORO_VOICES=" + voices +
			" exec " + python + " " + script}
}

// kokoroPython finds a python3 that can import kokoro_onnx:
// EIGEN_KOKORO_PYTHON wins; then a dedicated eigen venv (~/.eigen/kokoro/venv);
// then any python3 on PATH that actually has the package (verified by a quick
// import probe — a python WITHOUT kokoro_onnx would fail on every Speak).
func kokoroPython(home string) string {
	if p := strings.TrimSpace(os.Getenv("EIGEN_KOKORO_PYTHON")); p != "" {
		return p
	}
	candidates := []string{
		filepath.Join(home, ".eigen", "kokoro", "venv", "bin", "python3"),
	}
	if p, err := exec.LookPath("python3"); err == nil {
		candidates = append(candidates, p)
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if exec.Command(p, "-c", "import kokoro_onnx").Run() == nil {
			return p
		}
	}
	return ""
}

// materializeKokoroScript writes the embedded kokoro_stdin.py under
// ~/.eigen/kokoro/ (refreshed when the embedded copy changes) so the speaker
// runs eigen's own script — no external project checkout involved.
func materializeKokoroScript(home string) (string, error) {
	dir := filepath.Join(home, ".eigen", "kokoro")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "kokoro_stdin.py")
	if cur, err := os.ReadFile(path); err == nil && string(cur) == kokoroScript {
		return path, nil
	}
	if err := os.WriteFile(path, []byte(kokoroScript), 0o755); err != nil {
		return "", err
	}
	return path, nil
}

// firstExisting returns the first non-empty path that exists on disk.
func firstExisting(paths ...string) string {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
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

// Package speech provides optional text-to-speech for eigen: it pipes text to
// a configurable command's stdin (default: espeak-ng / piper / say). It is best
// effort — a missing or failing TTS command never affects the session.
package speech

import (
	"os"
	"os/exec"
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
// wins; otherwise the first of espeak-ng, piper, say found on PATH. If none is
// available the Speaker is a no-op.
func Detect() *Speaker {
	if c := strings.TrimSpace(os.Getenv("EIGEN_TTS_CMD")); c != "" {
		return &Speaker{argv: []string{"sh", "-c", c}}
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

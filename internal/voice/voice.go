// Package voice provides speech-to-text (record + transcribe) and
// text-to-speech for eigen's conversation mode. It shells out to local tools —
// arecord/whisper.cpp for STT, readd/espeak-ng for TTS — all overridable by
// env, so no model or cloud dependency is baked in. Detection is cheap and the
// whole package is a no-op when nothing is installed.
package voice

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// STT records a short utterance and returns its transcript.
type STT interface {
	// Listen records until trailing silence (or ctx is canceled) and returns
	// the transcribed text. An empty string means nothing was heard.
	Listen(ctx context.Context) (string, error)
	Available() bool
}

// TTS speaks text aloud and can be interrupted.
type TTS interface {
	Speak(ctx context.Context, text string) error
	Available() bool
}

// InterruptMonitor is an optional STT capability: watch the mic for sustained
// speech (a HIGHER bar than normal listening) while the assistant's reply is
// being spoken, so the user talking over it interrupts. Returns true when
// speech was detected; false when ctx ends first.
type InterruptMonitor interface {
	MonitorInterrupt(ctx context.Context) bool
}

// firstField returns the first whitespace-separated token of s (a command may
// be configured as "cmd arg1 arg2").
func firstField(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// have reports whether a command (possibly "cmd args…") resolves on PATH or as
// an absolute path.
func have(cmd string) bool {
	c := firstField(cmd)
	if c == "" {
		return false
	}
	if strings.ContainsRune(c, os.PathSeparator) {
		info, err := os.Stat(c)
		return err == nil && info.Mode()&0o111 != 0
	}
	_, err := exec.LookPath(c)
	return err == nil
}

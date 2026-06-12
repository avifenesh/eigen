package voice

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// cmdTTS speaks text by piping it to a TTS command's stdin. The command is
// configurable; defaults to espeak-ng/espeak/say. NOTE: callers that already
// resolved a better stack (Kokoro via speech.Detect) should use TTSFromArgv
// so conversation mode speaks with the SAME voice as read-aloud.
type cmdTTS struct {
	command string // EIGEN_VOICE_TTS_CMD, e.g. "espeak-ng"
	argv    []string
}

// TTSFromArgv wraps an already-resolved TTS argv (text on stdin) — the
// speech.Detect Kokoro stack — as a cancelable voice.TTS. Returns nil for an
// empty argv so callers can fall back to DetectTTS.
func TTSFromArgv(argv []string) TTS {
	if len(argv) == 0 {
		return nil
	}
	return &cmdTTS{argv: argv}
}

// DetectTTS builds the text-to-speech backend from the environment, falling
// back to espeak-ng.
func DetectTTS() TTS {
	cmd := os.Getenv("EIGEN_VOICE_TTS_CMD")
	if cmd == "" {
		switch {
		case have("espeak-ng"):
			cmd = "espeak-ng"
		case have("espeak"):
			cmd = "espeak"
		case have("say"): // macOS
			cmd = "say"
		}
	}
	return &cmdTTS{command: cmd}
}

func (t *cmdTTS) Available() bool {
	if len(t.argv) > 0 {
		return true
	}
	return t.command != "" && have(t.command)
}

// Speak pipes text to the TTS command's stdin and waits (ctx cancel stops it,
// which is how an interrupt cuts off speech mid-sentence).
func (t *cmdTTS) Speak(ctx context.Context, text string) error {
	if !t.Available() || strings.TrimSpace(text) == "" {
		return nil
	}
	argv := t.argv
	if len(argv) == 0 {
		argv = strings.Fields(t.command)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

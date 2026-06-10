package voice

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// cmdTTS speaks text by piping it to a TTS command's stdin. The command is
// configurable; defaults to readd (the user's reader) then espeak-ng.
type cmdTTS struct {
	command string // EIGEN_VOICE_TTS_CMD, e.g. "readd speak" or "espeak-ng"
}

// DetectTTS builds the text-to-speech backend from the environment, falling
// back to readd then espeak-ng.
func DetectTTS() TTS {
	cmd := os.Getenv("EIGEN_VOICE_TTS_CMD")
	if cmd == "" {
		switch {
		case have("readd"):
			cmd = "readd speak" // reads stdin
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

func (t *cmdTTS) Available() bool { return t.command != "" && have(t.command) }

// Speak pipes text to the TTS command's stdin and waits (ctx cancel stops it,
// which is how an interrupt cuts off speech mid-sentence).
func (t *cmdTTS) Speak(ctx context.Context, text string) error {
	if !t.Available() || strings.TrimSpace(text) == "" {
		return nil
	}
	argv := strings.Fields(t.command)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

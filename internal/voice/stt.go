package voice

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// whisperSTT records mic audio with arecord and transcribes it with a
// whisper.cpp CLI. Both are configurable via env.
type whisperSTT struct {
	recordCmd  string // EIGEN_VOICE_RECORD_CMD; writes WAV to the path in {out}
	whisperBin string // EIGEN_WHISPER_BIN (whisper-cli/main)
	model      string // EIGEN_WHISPER_MODEL
	maxSeconds int
}

// DetectSTT builds the speech-to-text backend from the environment, or returns
// an unavailable backend when the tools are missing.
func DetectSTT() STT {
	s := &whisperSTT{
		recordCmd:  os.Getenv("EIGEN_VOICE_RECORD_CMD"),
		whisperBin: firstNonEmpty(os.Getenv("EIGEN_WHISPER_BIN"), lookWhisper()),
		model:      firstNonEmpty(os.Getenv("EIGEN_WHISPER_MODEL"), lookWhisperModel()),
		maxSeconds: 30,
	}
	if s.recordCmd == "" {
		// Default recorder: arecord capturing 16kHz mono WAV (whisper's format).
		// {out} is substituted with the temp file path; {secs} with the cap.
		if have("arecord") {
			s.recordCmd = "arecord -q -f S16_LE -r 16000 -c 1 -d {secs} {out}"
		} else if have("parecord") {
			s.recordCmd = "parecord --channels=1 --rate=16000 --format=s16le {out}"
		}
	}
	return s
}

func (s *whisperSTT) Available() bool {
	return s.recordCmd != "" && have(s.recordCmd) && s.whisperBin != "" && have(s.whisperBin) && s.model != ""
}

// Listen records up to maxSeconds (or until ctx cancel) then transcribes.
func (s *whisperSTT) Listen(ctx context.Context) (string, error) {
	if !s.Available() {
		return "", fmt.Errorf("voice input unavailable (need a recorder + whisper + model)")
	}
	tmp, err := os.CreateTemp("", "eigen-voice-*.wav")
	if err != nil {
		return "", err
	}
	path := tmp.Name()
	tmp.Close()
	defer os.Remove(path)

	// Record.
	rec := expandCmd(s.recordCmd, map[string]string{"out": path, "secs": fmt.Sprintf("%d", s.maxSeconds)})
	rctx, cancel := context.WithTimeout(ctx, time.Duration(s.maxSeconds+5)*time.Second)
	defer cancel()
	rcmd := exec.CommandContext(rctx, rec[0], rec[1:]...)
	// arecord with -d stops itself; if the recorder runs unbounded, ctx cancel
	// (the user pressing the key again) ends it.
	_ = rcmd.Run() // a SIGINT/timeout end is expected; transcribe whatever we got
	if fi, err := os.Stat(path); err != nil || fi.Size() < 1024 {
		return "", nil // nothing recorded
	}

	// Transcribe.
	out := path + ".txt"
	defer os.Remove(out)
	args := []string{"-m", s.model, "-f", path, "-otxt", "-of", strings.TrimSuffix(out, ".txt"), "-nt"}
	tctx, tcancel := context.WithTimeout(ctx, 60*time.Second)
	defer tcancel()
	wcmd := exec.CommandContext(tctx, s.whisperBin, args...)
	if err := wcmd.Run(); err != nil {
		return "", fmt.Errorf("transcribe: %w", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// lookWhisper finds a whisper.cpp CLI: PATH, then the conventional checkout.
func lookWhisper() string {
	for _, name := range []string{"whisper-cli", "whisper"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, "projects", "whisper.cpp", "build", "bin", "whisper-cli")
		if info, err := os.Stat(p); err == nil && info.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// lookWhisperModel finds a usable model file in the conventional location.
func lookWhisperModel() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, "projects", "whisper.cpp", "models")
	// Prefer a real english base/small model over the bundled test fixtures.
	for _, name := range []string{"ggml-base.en.bin", "ggml-small.en.bin", "ggml-base.bin", "ggml-medium.en.bin"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// expandCmd splits a command template into argv, substituting {key} tokens.
func expandCmd(tmpl string, vars map[string]string) []string {
	for k, v := range vars {
		tmpl = strings.ReplaceAll(tmpl, "{"+k+"}", v)
	}
	return strings.Fields(tmpl)
}

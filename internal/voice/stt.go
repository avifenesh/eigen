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

// whisperSTT records mic audio and transcribes it with a whisper.cpp CLI.
// Recording is VAD-endpointed (vad.go): the recorder streams raw PCM and we
// stop on trailing quiet — push-to-talk without a fixed window. Both commands
// are configurable via env; EIGEN_VOICE_RECORD_CMD forces the legacy
// fixed-window file recorder instead.
type whisperSTT struct {
	recordCmd  string   // EIGEN_VOICE_RECORD_CMD; writes WAV to the path in {out}
	streamArgv []string // VAD path: raw S16_LE 16k mono PCM on stdout
	whisperBin string   // EIGEN_WHISPER_BIN (whisper-cli/main)
	model      string   // EIGEN_WHISPER_MODEL
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
		// Default: stream raw PCM for VAD endpointing (record until the user
		// stops talking). arecord/parecord both support stdout streaming.
		if have("arecord") {
			s.streamArgv = []string{"arecord", "-q", "-f", "S16_LE", "-r", "16000", "-c", "1", "-t", "raw"}
		} else if have("parecord") {
			s.streamArgv = []string{"parecord", "--channels=1", "--rate=16000", "--format=s16le", "--raw"}
		}
	}
	return s
}

func (s *whisperSTT) Available() bool {
	rec := s.recordCmd != "" && have(s.recordCmd) || len(s.streamArgv) > 0 && have(s.streamArgv[0])
	return rec && s.whisperBin != "" && have(s.whisperBin) && s.model != ""
}

// Listen records (VAD-endpointed when streaming; fixed window for a custom
// recordCmd) then transcribes. ctx cancel stops the recording and transcribes
// what was captured.
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

	if s.recordCmd == "" && len(s.streamArgv) > 0 {
		// VAD path: stream PCM, stop on trailing quiet, wrap as WAV.
		pcm, err := recordVAD(ctx, s.streamArgv, defaultVAD())
		if err != nil && len(pcm) == 0 {
			return "", nil // canceled before speech — not an error
		}
		if len(pcm) < 16000 { // < ~0.5s of audio: nothing real
			return "", nil
		}
		if err := writeWAV(path, pcm); err != nil {
			return "", err
		}
	} else {
		// Legacy fixed-window recorder (custom EIGEN_VOICE_RECORD_CMD).
		rec := expandCmd(s.recordCmd, map[string]string{"out": path, "secs": fmt.Sprintf("%d", s.maxSeconds)})
		rctx, cancel := context.WithTimeout(ctx, time.Duration(s.maxSeconds+5)*time.Second)
		defer cancel()
		rcmd := exec.CommandContext(rctx, rec[0], rec[1:]...)
		_ = rcmd.Run() // a SIGINT/timeout end is expected; transcribe whatever we got
		if fi, err := os.Stat(path); err != nil || fi.Size() < 1024 {
			return "", nil // nothing recorded
		}
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

// lookWhisper finds a whisper.cpp CLI: PATH, then the conventional checkout
// (whisper-cli, or the legacy `main` binary older builds produce).
func lookWhisper() string {
	for _, name := range []string{"whisper-cli", "whisper"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, name := range []string{"whisper-cli", "main"} {
			p := filepath.Join(home, "projects", "whisper.cpp", "build", "bin", name)
			if info, err := os.Stat(p); err == nil && info.Mode()&0o111 != 0 {
				return p
			}
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
	// Fallback: any real ggml model in the dir — but never the bundled
	// for-tests-* fixtures (tiny stubs that produce garbage transcripts).
	if matches, err := filepath.Glob(filepath.Join(dir, "ggml-*.bin")); err == nil {
		for _, p := range matches {
			if !strings.HasPrefix(filepath.Base(p), "for-tests-") {
				return p
			}
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

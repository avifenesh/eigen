package voice

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Check is one diagnosed component of the voice stack.
type Check struct {
	Name   string // "recorder", "whisper", "model", "kokoro python", …
	OK     bool
	Detail string // resolved path / value, or what's missing
	Fix    string // a concrete next step when not OK
}

// Diagnose probes every piece of the voice stack and returns per-component
// results plus an overall summary. Read-only: it resolves paths and runs
// cheap probes (e.g. "does this python import kokoro_onnx?") but records
// nothing and changes nothing. Powers `/voice setup`.
func Diagnose() []Check {
	var out []Check

	// --- STT: recorder -------------------------------------------------
	if rec := os.Getenv("EIGEN_VOICE_RECORD_CMD"); rec != "" {
		out = append(out, probeBin("recorder", rec,
			"EIGEN_VOICE_RECORD_CMD is set but the program isn't on PATH"))
	} else {
		switch {
		case have("arecord"):
			out = append(out, Check{Name: "recorder", OK: true, Detail: "arecord (alsa-utils), VAD streaming"})
		case have("parecord"):
			out = append(out, Check{Name: "recorder", OK: true, Detail: "parecord (pulseaudio)"})
		default:
			out = append(out, Check{Name: "recorder", OK: false,
				Detail: "no arecord or parecord found",
				Fix:    "install alsa-utils (arecord) or pulseaudio-utils (parecord)"})
		}
	}

	// --- STT: whisper binary -------------------------------------------
	whisper := firstNonEmpty(os.Getenv("EIGEN_WHISPER_BIN"), lookWhisper())
	if whisper != "" {
		out = append(out, Check{Name: "whisper", OK: true, Detail: whisper})
	} else {
		out = append(out, Check{Name: "whisper", OK: false,
			Detail: "whisper-cli/whisper not found",
			Fix:    "build whisper.cpp (whisper-cli) or set EIGEN_WHISPER_BIN"})
	}

	// --- STT: model ----------------------------------------------------
	model := firstNonEmpty(os.Getenv("EIGEN_WHISPER_MODEL"), lookWhisperModel())
	if model != "" {
		out = append(out, Check{Name: "whisper model", OK: true, Detail: model})
	} else {
		out = append(out, Check{Name: "whisper model", OK: false,
			Detail: "no ggml-*.bin model (test fixtures don't count)",
			Fix:    "download e.g. ggml-base.en.bin into ~/projects/whisper.cpp/models or set EIGEN_WHISPER_MODEL"})
	}

	// --- TTS: backend --------------------------------------------------
	out = append(out, diagnoseTTS()...)

	// --- playback ------------------------------------------------------
	switch {
	case have("aplay"):
		out = append(out, Check{Name: "playback", OK: true, Detail: "aplay"})
	case have("paplay"):
		out = append(out, Check{Name: "playback", OK: true, Detail: "paplay"})
	default:
		out = append(out, Check{Name: "playback", OK: false,
			Detail: "no aplay/paplay (needed by the Kokoro backend)",
			Fix:    "install alsa-utils (aplay)"})
	}

	return out
}

// diagnoseTTS resolves the TTS backend the way speech.Detect does, but
// reports WHICH stack won and why the Kokoro pieces are or aren't present.
func diagnoseTTS() []Check {
	var out []Check
	if c := strings.TrimSpace(os.Getenv("EIGEN_TTS_CMD")); c != "" {
		out = append(out, Check{Name: "tts", OK: true, Detail: "EIGEN_TTS_CMD override: " + c})
		return out
	}

	home, _ := os.UserHomeDir()
	model := firstExistingFile(
		os.Getenv("EIGEN_KOKORO_MODEL"),
		filepath.Join(home, ".eigen", "kokoro", "kokoro-v1.0.onnx"),
		filepath.Join(home, ".local", "share", "kokoro", "kokoro-v1.0.onnx"),
	)
	voices := firstExistingFile(
		os.Getenv("EIGEN_KOKORO_VOICES"),
		filepath.Join(home, ".eigen", "kokoro", "voices-v1.0.bin"),
		filepath.Join(home, ".local", "share", "kokoro", "voices-v1.0.bin"),
	)
	python := kokoroPythonProbe(home)

	kokoroOK := model != "" && voices != "" && python != ""
	if kokoroOK {
		out = append(out, Check{Name: "tts", OK: true, Detail: "Kokoro (high quality) via " + python})
	} else {
		// Kokoro missing → does an espeak-style fallback exist?
		fb := ""
		for _, bin := range []string{"espeak-ng", "piper", "say"} {
			if have(bin) {
				fb = bin
				break
			}
		}
		if fb != "" {
			out = append(out, Check{Name: "tts", OK: true,
				Detail: "fallback: " + fb + " (robotic — set up Kokoro for natural speech)"})
		} else {
			out = append(out, Check{Name: "tts", OK: false,
				Detail: "no TTS backend at all",
				Fix:    "set up Kokoro (below) or install espeak-ng"})
		}
	}

	// Always report the Kokoro pieces so the user knows what to fix.
	out = append(out, fileCheck("kokoro model", model,
		"download kokoro-v1.0.onnx into ~/.eigen/kokoro/ or ~/.local/share/kokoro/"))
	out = append(out, fileCheck("kokoro voices", voices,
		"download voices-v1.0.bin into ~/.eigen/kokoro/ or ~/.local/share/kokoro/"))
	if python != "" {
		out = append(out, Check{Name: "kokoro python", OK: true, Detail: python + " (imports kokoro_onnx)"})
	} else {
		out = append(out, Check{Name: "kokoro python", OK: false,
			Detail: "no python3 with kokoro_onnx",
			Fix:    "python3 -m venv ~/.eigen/kokoro/venv && ~/.eigen/kokoro/venv/bin/pip install kokoro-onnx numpy"})
	}
	return out
}

// kokoroPythonProbe mirrors speech.kokoroPython without importing it (avoids a
// cycle): EIGEN_KOKORO_PYTHON, then the eigen venv, then any python3 that can
// import kokoro_onnx.
func kokoroPythonProbe(home string) string {
	if p := strings.TrimSpace(os.Getenv("EIGEN_KOKORO_PYTHON")); p != "" {
		if canImportKokoro(p) {
			return p
		}
		return ""
	}
	cands := []string{filepath.Join(home, ".eigen", "kokoro", "venv", "bin", "python3")}
	if p, err := exec.LookPath("python3"); err == nil {
		cands = append(cands, p)
	}
	for _, p := range cands {
		if _, err := os.Stat(p); err == nil && canImportKokoro(p) {
			return p
		}
	}
	return ""
}

func canImportKokoro(python string) bool {
	return exec.Command(python, "-c", "import kokoro_onnx").Run() == nil
}

func probeBin(name, bin, missing string) Check {
	field := strings.Fields(bin)
	if len(field) > 0 && have(field[0]) {
		return Check{Name: name, OK: true, Detail: bin}
	}
	return Check{Name: name, OK: false, Detail: missing}
}

func fileCheck(name, path, fix string) Check {
	if path != "" {
		return Check{Name: name, OK: true, Detail: path}
	}
	return Check{Name: name, OK: false, Detail: "not found", Fix: fix}
}

func firstExistingFile(paths ...string) string {
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

// Report renders Diagnose() as a readable block for the chat note area.
func Report() string {
	checks := Diagnose()
	var b strings.Builder
	allOK := true
	for _, c := range checks {
		glyph := "✓"
		if !c.OK {
			glyph = "✗"
			allOK = false
		}
		b.WriteString(fmt.Sprintf("%s %-15s %s\n", glyph, c.Name, c.Detail))
		if !c.OK && c.Fix != "" {
			b.WriteString(fmt.Sprintf("   → %s\n", c.Fix))
		}
	}
	if allOK {
		b.WriteString("\nvoice ready: ⏺ speak · ▶ read · ◉ voice — all set.")
	} else {
		b.WriteString("\nfix the ✗ items above, then /voice setup again. (" + runtime.GOOS + ")")
	}
	return strings.TrimRight(b.String(), "\n")
}

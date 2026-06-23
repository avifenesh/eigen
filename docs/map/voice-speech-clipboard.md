# voice/, speech/, clipboard/

> The local-IO peripherals slice of eigen's terminal chat: turning microphone audio
> into text (STT), text into spoken audio (TTS), and shuttling text/images to and
> from the system clipboard. Every piece is **best-effort and self-disabling** — it
> shells out to external tools (arecord/whisper.cpp, Kokoro-ONNX/espeak-ng,
> wl-copy/xclip/pbcopy), discovered at startup via env-overridable detection, and a
> missing tool is silently "unavailable" rather than an error. All three packages are
> consumed exclusively by the TUI (`internal/tui`), not the Wails GUI or the daemon.
> `speech` provides the read-aloud "one voice everywhere" Kokoro stack; `voice`
> provides cancelable conversation-mode STT/TTS (with VAD endpointing and
> talk-over interrupt detection) and reuses `speech`'s resolved argv so both speak
> with the same voice; `clipboard` handles copy/paste of text and image paste for
> vision models.

## Files

### internal/clipboard/clipboard.go
- **Role:** Text copy/paste to the system clipboard by piping through an external command; missing command = unavailable, never an error.
- **Key symbols:**
  - `type Copier` — holds resolved `argv` (copy) and `paste` argv slices; the zero value is a safe no-op.
  - `Detect() *Copier` — resolves commands: `EIGEN_CLIPBOARD_CMD`/`EIGEN_CLIPBOARD_PASTE_CMD` (via `sh -c`) win, else first available of wl-copy/wl-paste, xclip, xsel, pbcopy/pbpaste.
  - `(*Copier).Available() bool` — whether a copy command resolved.
  - `(*Copier).Copy(text string) error` — writes text to clipboard via stdin; no-op (nil err) when unavailable.
  - `(*Copier).CanPaste() bool` — whether a paste command resolved.
  - `(*Copier).Paste() (string, error)` — reads clipboard contents; empty string when unavailable.
- **Depends on:** stdlib only (`os`, `os/exec`, `strings`).
- **Used by / entrypoint:** `internal/tui/tui.go` calls `clipboard.Detect()` to build the model's `clip` field (typed as the `clipIface` interface in tui.go); `Copy` from `internal/tui/nav.go`, `commands.go`, `tui.go` (drag-to-copy); `CanPaste`/`Paste`/`Available` from `internal/tui/input.go`, `nav.go`, `commands.go`.

### internal/clipboard/image.go
- **Role:** Best-effort read of an image off the clipboard (for vision-model paste), using the same wl-paste/xclip/pngpaste backend family.
- **Key symbols:**
  - `type ImageData` — raw clipboard image `Bytes` plus `MediaType`; **consumed** in `internal/tui/attach.go` via the returned pointer's `.Bytes`/`.MediaType` fields.
  - `var imageMIMEs` — preference-ordered MIME list (png, jpeg, webp, gif).
  - `PasteImage() (*ImageData, error)` — returns the first available clipboard image; `(nil, nil)` when none/unsupported on a known tool, error only when no image tool exists at all.
  - `containsLine(data []byte, s string) bool` — unexported helper: exact-line match against `wl-paste --list-types` output.
- **Depends on:** stdlib only (`fmt`, `os/exec`).
- **Used by / entrypoint:** `internal/tui/attach.go:156` (`pasteImage` handler) calls `clipboard.PasteImage()`, enforces vision support + size cap, then stages an `llm.Image` for the next message.

### internal/speech/embed.go
- **Role:** Embeds eigen's vendored Kokoro stdin TTS Python script into the binary so the app is self-contained.
- **Key symbols:**
  - `var kokoroScript string` (`//go:embed embedded/kokoro_stdin.py`) — the embedded script source; materialized to `~/.eigen/kokoro/kokoro_stdin.py` at detection time.
- **Depends on:** `embed` (blank import).
- **Used by / entrypoint:** read by `materializeKokoroScript` in `speech.go`. (The embedded `embedded/kokoro_stdin.py` is a Python asset, not a Go source file, so it is outside the mapped `.go` set.)

### internal/speech/speech.go
- **Role:** Optional read-aloud TTS — pipes text to a configurable command's stdin, preferring the local Kokoro ONNX stack over espeak-ng/piper/say; the canonical voice-detection used everywhere.
- **Key symbols:**
  - `type Speaker` — holds the resolved `argv` plus a mutex-guarded current `*exec.Cmd` for interruption; zero value is a no-op.
  - `Detect() *Speaker` — resolves TTS: `EIGEN_TTS_CMD` (`sh -c`) wins, else Kokoro stack, else espeak-ng/piper/say. (Note: `readd speak` is deliberately NOT a candidate.)
  - `(*Speaker).Available() bool` — whether a command resolved.
  - `(*Speaker).Argv() []string` — exposes the resolved argv so `voice` mode reuses the SAME voice (one voice for read-aloud and conversation).
  - `(*Speaker).Speak(text string)` — speaks async, interrupting in-progress speech; returns immediately.
  - `(*Speaker).Stop()` — kills any in-progress speech process.
  - `(*Speaker).speak(text string) error` — unexported synchronous core (unit-testable) behind `Speak`.
  - `detectKokoro() []string` — assembles the Kokoro argv (embedded script + model/voices files + python) with env baked in, or nil if any piece is missing.
  - `kokoroPython(home string) string` — finds a python3 that can `import kokoro_onnx` (env override → eigen venv → PATH probe).
  - `materializeKokoroScript(home string) (string, error)` — writes `embed.kokoroScript` under `~/.eigen/kokoro/`, refreshing only when changed.
  - `firstExisting(paths ...string) string` — first existing path on disk.
- **Depends on:** stdlib only (`os`, `os/exec`, `path/filepath`, `strings`, `sync`) plus the package-local `embed.go`.
- **Used by / entrypoint:** `internal/tui/tui.go:2078` calls `speech.Detect()` into the model's `speaker` field (typed as `speakerIface`). `Argv()` feeds `voiceTTS` in `internal/tui/voice.go`; `Speak`/`Stop`/`Available` drive read-aloud from `tui.go`, `voice.go`, `commands.go`.

### internal/voice/voice.go
- **Role:** Package doc + the STT/TTS interface contracts and shared command-resolution helpers for conversation mode.
- **Key symbols:**
  - `type STT interface` — `Listen(ctx) (string, error)` (record until trailing silence/cancel) + `Available() bool`.
  - `type TTS interface` — `Speak(ctx, text) error` + `Available() bool`.
  - `type InterruptMonitor interface` — optional STT capability: `MonitorInterrupt(ctx) bool`, watch mic for talk-over while the reply is spoken.
  - `firstField(s string) string` — first whitespace token of a command string.
  - `have(cmd string) bool` — whether a command resolves on PATH or as an executable absolute path.
- **Depends on:** stdlib only (`context`, `os`, `os/exec`, `strings`).
- **Used by / entrypoint:** the interfaces are implemented by `whisperSTT` (stt.go) and `cmdTTS` (tts.go) and held as `voice.STT`/`voice.TTS` fields on the TUI model; `internal/tui/voice.go` type-asserts `m.stt.(voice.InterruptMonitor)`. `have`/`firstField` are used across stt.go, tts.go, doctor.go.

### internal/voice/stt.go
- **Role:** Speech-to-text: VAD-endpointed mic recording (or a legacy fixed-window recorder) transcribed by a whisper.cpp CLI.
- **Key symbols:**
  - `type whisperSTT` — recorder/stream argv, whisper bin, model path, max seconds; implements `STT` and `InterruptMonitor`.
  - `DetectSTT() STT` — builds the backend from env (`EIGEN_VOICE_RECORD_CMD`, `EIGEN_WHISPER_BIN`, `EIGEN_WHISPER_MODEL`), defaulting to arecord/parecord raw-PCM streaming for VAD.
  - `(*whisperSTT).Available() bool` — recorder + whisper bin + model all resolve.
  - `(*whisperSTT).MonitorInterrupt(ctx) bool` — delegates to `monitorInterrupt` when streaming (the legacy recorder can't monitor).
  - `(*whisperSTT).Listen(ctx) (string, error)` — records (VAD or fixed window), writes WAV, then transcribes via whisper in a **detached** 60s context so a "stop" cancel still transcribes captured audio.
  - `lookWhisper() string` / `lookWhisperModel() string` — locate the whisper CLI and a real (non-fixture) ggml model in the conventional `~/projects/whisper.cpp` checkout.
  - `firstNonEmpty(vals ...string) string` — first non-empty string.
  - `expandCmd(tmpl string, vars map[string]string) []string` — substitute `{key}` tokens then split into argv (for the legacy recorder template).
- **Depends on:** package-local `voice.go` (`have`), `vad.go` (`recordVAD`, `writeWAV`, `defaultVAD`, `monitorInterrupt`); stdlib otherwise.
- **Used by / entrypoint:** `internal/tui/tui.go:2091` calls `voice.DetectSTT()` into the model's `stt` field; `Listen`/`Available` and the `InterruptMonitor` assertion are exercised in `internal/tui/voice.go`.

### internal/voice/tts.go
- **Role:** Cancelable conversation-mode TTS — pipes text to stdin of either an already-resolved argv (the Kokoro stack from `speech.Detect`) or an espeak-style fallback.
- **Key symbols:**
  - `type cmdTTS` — a TTS `command` string and/or pre-resolved `argv`; implements `TTS`.
  - `TTSFromArgv(argv []string) TTS` — wraps a resolved argv (Kokoro) as a cancelable `voice.TTS`; nil for empty argv so callers fall back to `DetectTTS`.
  - `DetectTTS() TTS` — builds from `EIGEN_VOICE_TTS_CMD` else espeak-ng/espeak/say.
  - `(*cmdTTS).Available() bool` — argv present, or command resolves on PATH.
  - `(*cmdTTS).Speak(ctx, text) error` — pipes text to stdin and waits; ctx cancel cuts speech mid-sentence (the interrupt mechanism).
- **Depends on:** package-local `voice.go` (`have`); stdlib otherwise.
- **Used by / entrypoint:** `internal/tui/voice.go:43` `voiceTTS(spk *speech.Speaker)` prefers `voice.TTSFromArgv(spk.Argv())` and falls back to `voice.DetectTTS()`; the resulting `TTS.Speak` is driven from `speechqueue.go` and `voice.go`.

### internal/voice/vad.go
- **Role:** Voice-activity-detection engine — stream raw PCM, endpoint utterances on trailing quiet, detect talk-over interrupts, and wrap PCM as WAV. (Endpointing ported from the author's codex-desktop-linux patch.js.)
- **Key symbols:**
  - `type vadParams` + `defaultVAD()` — tunable thresholds/durations (env: `EIGEN_VOICE_VAD_THRESHOLD`, `EIGEN_VOICE_SILENCE_MS`); speech starts ~220ms above threshold, ends after ~1.8s quiet.
  - `recordVAD(ctx, argv, p) ([]byte, error)` — runs the recorder, reads frames over a channel (so deadlines fire even with no mic data), returns captured PCM when the utterance ends; always kills the recorder.
  - `monitorInterrupt(ctx, argv) bool` — watches the mic during playback for ~420ms sustained voice above a HIGHER threshold (env `EIGEN_VOICE_INTERRUPT_THRESHOLD`); frame-based timing so live mics and recorded test input behave identically.
  - `rmsLevel(pcm []byte) float64` — normalized RMS (0..1) of S16_LE PCM.
  - `writeWAV(path string, pcm []byte) error` — minimal 44-byte WAV header + S16_LE mono 16kHz PCM.
  - `envFloat`/`envDuration` — bounded env parsing helpers.
- **Depends on:** stdlib only (`context`, `encoding/binary`, `io`, `math`, `os`, `os/exec`, `time`).
- **Used by / entrypoint:** internal to `voice` — `stt.go` calls `recordVAD`, `defaultVAD`, `writeWAV` (in `Listen`) and `monitorInterrupt` (in `MonitorInterrupt`). Also exercised directly by `vad_test.go`.

### internal/voice/doctor.go
- **Role:** Read-only diagnostics for the whole voice stack (recorder, whisper, model, TTS backend incl. Kokoro pieces, playback) — powers `/voice setup`.
- **Key symbols:**
  - `type Check` — one diagnosed component (Name/OK/Detail/Fix).
  - `Diagnose() []Check` — probes every piece and returns per-component results; reachable via `Report` (exported API + called from TUI) and tested directly.
  - `Report() string` — renders `Diagnose()` as a glyph block for the chat note area.
  - `diagnoseTTS() []Check` — mirrors `speech.Detect`'s resolution but reports which stack won and the state of each Kokoro piece.
  - `kokoroPythonProbe(home string) string` / `canImportKokoro(python string) bool` — duplicate of `speech.kokoroPython` (intentional, to avoid an import cycle).
  - `probeBin`, `fileCheck`, `firstExistingFile` — `Check`-building helpers.
- **Depends on:** package-local `voice.go` (`have`), `stt.go` (`firstNonEmpty`, `lookWhisper`, `lookWhisperModel`); stdlib otherwise. Intentionally does NOT import `internal/speech` (avoids a cycle).
- **Used by / entrypoint:** `internal/tui/commands.go:315` calls `voice.Report()` for the `/voice setup` command output; `Diagnose` is also covered by `doctor_test.go`.

## Cross-links
- **internal/tui** — the sole consumer of all three packages. `tui.go` wires `clipboard.Detect`, `speech.Detect`, `voice.DetectSTT` into the model; `voice.go`/`speechqueue.go` drive conversation mode, read-aloud, and interrupt monitoring; `commands.go` exposes `/voice setup` (→ `voice.Report`) and copy commands; `attach.go` consumes `clipboard.PasteImage`; `input.go`/`nav.go` use copy/paste.
- **internal/llm** — indirect: `tui/attach.go` converts a `clipboard.ImageData` into an `llm.Image` and consults `llm.Vision` before staging a pasted image.
- **speech ↔ voice** — `voice.TTSFromArgv` wraps the argv returned by `speech.Speaker.Argv()` so conversation mode and read-aloud share one voice; `voice/doctor.go` re-implements (does not import) `speech`'s Kokoro detection to keep the dependency one-directional and cycle-free.
- **External tools (not Go packages)** — arecord/parecord, whisper.cpp (whisper-cli), Kokoro-ONNX via python3, espeak-ng/piper/say, aplay/paplay, wl-copy/wl-paste, xclip, xsel, pbcopy/pbpaste, pngpaste. All discovered at runtime and overridable via `EIGEN_*` env vars.

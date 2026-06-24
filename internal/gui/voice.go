package gui

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/speech"
	"github.com/avifenesh/eigen/internal/voice"
)

// errVoiceUnavailable is returned when the requested voice capability (STT or
// TTS) isn't installed in this environment — the frontend gates on VoiceStatus
// so this is a defensive guard, surfaced as a clear message via Wails.
var errVoiceUnavailable = errors.New("voice unavailable: install a recorder + whisper (STT) and a TTS command")

// Voice bridge layer. eigen's voice stack (internal/voice) is server-side — it
// shells out to a local recorder + whisper for STT and Kokoro/espeak for TTS —
// so the GUI process (which runs on the host, not in the sandboxed webview)
// drives the SAME stack the TUI uses and streams state to the frontend rather
// than capturing audio in the browser. Three features mirror the TUI taxonomy:
//
//	1. dictate    — VoiceListen: record one VAD-endpointed utterance → transcript.
//	2. read aloud — VoiceSpeak/VoiceStopSpeak: speak a given text once, cancelable.
//	3. voice mode — VoiceModeStart/Stop: a hands-free loop (listen → submit the
//	   turn → speak the reply → listen again), emitting eigen:voice state so the
//	   composer can show what the mic is doing.
//
// Detection is cheap and the whole surface degrades to "unavailable" when no
// recorder/whisper/TTS is installed, so VoiceStatus gates the UI.

const eventVoice = "eigen:voice"

// VoiceStatusDTO reports which voice capabilities are usable in this
// environment, so the frontend can show/hide the affordances.
type VoiceStatusDTO struct {
	STT bool `json:"stt"` // a recorder + whisper + model are present
	TTS bool `json:"tts"` // a TTS command (Kokoro/espeak/…) is present
}

// VoiceEventDTO is pushed on eigen:voice as the mic/speaker change state during
// voice mode (and one-shot listen), so the composer reflects what's happening.
type VoiceEventDTO struct {
	// Phase is one of: idle | listening | transcribing | thinking | speaking |
	// error | off. The frontend maps it to the composer's mic affordance.
	Phase string `json:"phase"`
	Text  string `json:"text,omitempty"`  // transcript (listening done) or error message
	Mode  bool   `json:"mode,omitempty"`  // true while voice MODE (hands-free loop) is active
}

// voiceCtl is the bridge's voice controller: lazily-detected STT/TTS plus the
// single in-flight operation's cancel func (one mic op at a time) and the
// conversation-mode loop's lifecycle. Guarded by mu.
type voiceCtl struct {
	mu       sync.Mutex
	stt      voice.STT
	tts      voice.TTS
	detected bool
	// cancel stops the current one-shot Listen/Speak; modeStop stops the loop.
	cancel   context.CancelFunc
	modeStop context.CancelFunc
	speaking bool
}

func (b *Bridge) voice() *voiceCtl {
	b.voiceOnce.Do(func() { b.voiceCtl = &voiceCtl{} })
	return b.voiceCtl
}

// ensureDetected resolves the STT/TTS backends once (cheap PATH probes).
func (v *voiceCtl) ensureDetected() {
	if v.detected {
		return
	}
	v.stt = voice.DetectSTT()
	if spk := speech.Detect(); spk != nil && spk.Available() {
		if t := voice.TTSFromArgv(spk.Argv()); t != nil {
			v.tts = t
		}
	}
	if v.tts == nil {
		v.tts = voice.DetectTTS()
	}
	v.detected = true
}

// VoiceStatus reports voice availability for capability-gating the UI.
func (b *Bridge) VoiceStatus() (*VoiceStatusDTO, error) {
	v := b.voice()
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ensureDetected()
	return &VoiceStatusDTO{
		STT: v.stt != nil && v.stt.Available(),
		TTS: v.tts != nil && v.tts.Available(),
	}, nil
}

// VoiceListen records ONE VAD-endpointed utterance and returns its transcript
// (empty when nothing was heard). It emits listening→transcribing→idle so the
// composer can show the mic state. A second call cancels the first.
func (b *Bridge) VoiceListen() (string, error) {
	v := b.voice()
	v.mu.Lock()
	v.ensureDetected()
	if v.stt == nil || !v.stt.Available() {
		v.mu.Unlock()
		return "", errVoiceUnavailable
	}
	if v.cancel != nil {
		v.cancel() // supersede any in-flight listen
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	v.cancel = cancel
	stt := v.stt
	v.mu.Unlock()

	b.emit(eventVoice, VoiceEventDTO{Phase: "listening"})
	text, err := stt.Listen(ctx)
	cancel()
	v.mu.Lock()
	if v.cancel != nil {
		v.cancel = nil
	}
	v.mu.Unlock()
	if err != nil {
		b.emit(eventVoice, VoiceEventDTO{Phase: "error", Text: err.Error()})
		return "", err
	}
	b.emit(eventVoice, VoiceEventDTO{Phase: "idle", Text: text})
	return text, nil
}

// VoiceCancelListen cancels an in-flight one-shot listen (the user pressed the
// mic again to stop). The Listen call returns what it captured so far.
func (b *Bridge) VoiceCancelListen() error {
	v := b.voice()
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
	return nil
}

// VoiceSpeak speaks text aloud once (read-aloud of an assistant reply). A new
// call cancels the previous utterance. No-op when no TTS is present.
func (b *Bridge) VoiceSpeak(text string) error {
	v := b.voice()
	v.mu.Lock()
	v.ensureDetected()
	if v.tts == nil || !v.tts.Available() {
		v.mu.Unlock()
		return errVoiceUnavailable
	}
	if v.cancel != nil {
		v.cancel() // stop any prior speak/listen
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.cancel = cancel
	v.speaking = true
	tts := v.tts
	v.mu.Unlock()

	b.emit(eventVoice, VoiceEventDTO{Phase: "speaking"})
	err := tts.Speak(ctx, text)
	cancel()
	v.mu.Lock()
	v.speaking = false
	if v.cancel != nil {
		v.cancel = nil
	}
	v.mu.Unlock()
	b.emit(eventVoice, VoiceEventDTO{Phase: "idle"})
	return err
}

// VoiceStopSpeak cancels the current read-aloud.
func (b *Bridge) VoiceStopSpeak() error {
	v := b.voice()
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.speaking && v.cancel != nil {
		v.cancel()
		v.cancel = nil
		v.speaking = false
	}
	return nil
}

// VoiceModeStart begins the hands-free conversation loop for a session:
// listen → submit the transcript as a turn → wait for the turn to finish →
// speak the reply → listen again, until VoiceModeStop. Each phase is pushed on
// eigen:voice (mode=true) so the composer shows the live state. Needs both STT
// and TTS; returns unavailable otherwise. A second Start restarts the loop.
func (b *Bridge) VoiceModeStart(sessionID string) error {
	if sessionID == "" {
		return errVoiceUnavailable
	}
	v := b.voice()
	v.mu.Lock()
	v.ensureDetected()
	if v.stt == nil || !v.stt.Available() || v.tts == nil || !v.tts.Available() {
		v.mu.Unlock()
		return errVoiceUnavailable
	}
	if v.modeStop != nil {
		v.modeStop() // restart cleanly
	}
	ctx, stop := context.WithCancel(context.Background())
	v.modeStop = stop
	stt, tts := v.stt, v.tts
	v.mu.Unlock()
	go b.voiceModeLoop(ctx, sessionID, stt, tts)
	return nil
}

// VoiceModeStop ends the conversation loop (and any in-flight listen/speak).
func (b *Bridge) VoiceModeStop() error {
	v := b.voice()
	v.mu.Lock()
	if v.modeStop != nil {
		v.modeStop()
		v.modeStop = nil
	}
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
	v.mu.Unlock()
	b.emit(eventVoice, VoiceEventDTO{Phase: "off"})
	return nil
}

// voiceModeLoop drives one hands-free conversation. It runs off the request
// path (its own goroutine) and exits on ctx cancel (VoiceModeStop) or a fatal
// listen/speak error. Each transcript is submitted via SendInput; the loop then
// polls State until the turn goes idle, reads the latest assistant text, and
// speaks it before listening again.
func (b *Bridge) voiceModeLoop(ctx context.Context, sessionID string, stt voice.STT, tts voice.TTS) {
	for ctx.Err() == nil {
		b.emit(eventVoice, VoiceEventDTO{Phase: "listening", Mode: true})
		text, err := stt.Listen(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			b.emit(eventVoice, VoiceEventDTO{Phase: "error", Text: err.Error(), Mode: true})
			return
		}
		if text == "" {
			continue // heard nothing; listen again
		}
		b.emit(eventVoice, VoiceEventDTO{Phase: "thinking", Text: text, Mode: true})
		if err := b.SendInput(sessionID, text, nil, nil); err != nil {
			b.emit(eventVoice, VoiceEventDTO{Phase: "error", Text: err.Error(), Mode: true})
			return
		}
		reply := b.waitForReply(ctx, sessionID)
		if ctx.Err() != nil {
			return
		}
		if reply != "" {
			b.emit(eventVoice, VoiceEventDTO{Phase: "speaking", Mode: true})
			_ = tts.Speak(ctx, reply)
		}
	}
}

// waitForReply polls the session State until the turn finishes (not running),
// then returns the latest assistant message text. Bounded so a stuck turn can't
// pin the loop forever; ctx cancel (VoiceModeStop) returns immediately.
func (b *Bridge) waitForReply(ctx context.Context, sessionID string) string {
	deadline := time.NewTimer(10 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(700 * time.Millisecond)
	defer tick.Stop()
	// Let the turn actually start before treating "not running" as done.
	startGrace := time.After(1500 * time.Millisecond)
	started := false
	for {
		select {
		case <-ctx.Done():
			return ""
		case <-deadline.C:
			if st, err := b.State(sessionID); err == nil && st != nil {
				return latestAssistant(st)
			}
			return ""
		case <-startGrace:
			started = true
		case <-tick.C:
			st, err := b.State(sessionID)
			if err != nil || st == nil {
				continue
			}
			if st.Running {
				started = true
				continue
			}
			if started {
				return latestAssistant(st)
			}
		}
	}
}

// latestAssistant returns the text of the last assistant message in a State.
func latestAssistant(st *SessionStateDTO) string {
	for i := len(st.Messages) - 1; i >= 0; i-- {
		m := st.Messages[i]
		if m.Role == "assistant" && m.Text != "" {
			return m.Text
		}
	}
	return ""
}

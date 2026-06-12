package tui

// Voice: three DISTINCT features, three affordances (the user's taxonomy):
//
//  1. dictate — "one speak": record one utterance, transcribe, SUBMIT as a
//     normal turn. The answer comes back as TEXT — eigen does not speak.
//  2. read aloud — the user reads/writes normally; clicking ▶ speaks the
//     LAST answer once. (The persistent /read toggle that speaks every
//     answer still exists; the button is the one-shot.)
//  3. voice mode — full conversation: dictate → spoken reply → listen again,
//     hands-free, until toggled off. (Ported semantics from the user's
//     codex-desktop-linux conversation-mode.)
//
// All three are BUTTONS in the sidebar (ctrl+t is zellij's tab chord — dead
// in the user's stack; keybinds are secondary). Recording is VAD-endpointed
// (internal/voice/vad.go): it stops on trailing quiet, no fixed window.

import (
	"context"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avifenesh/eigen/internal/speech"
	"github.com/avifenesh/eigen/internal/voice"
)

// voiceSpokenMsg carries a transcribed utterance back to the UI loop.
// conv distinguishes voice-mode turns (spoken reply + relisten) from one-shot
// dictation (text reply, no speech).
type voiceSpokenMsg struct {
	text string
	err  error
	conv bool
	gen  int
}

// voiceTTS picks conversation mode's TTS: the SAME stack the read-aloud
// speaker resolved (Kokoro when present — one voice everywhere), wrapped as a
// cancelable voice.TTS; espeak-style fallback only when the speaker found
// nothing. EIGEN_VOICE_TTS_CMD still overrides via DetectTTS.
func voiceTTS(spk *speech.Speaker) voice.TTS {
	if os.Getenv("EIGEN_VOICE_TTS_CMD") == "" && spk != nil {
		if t := voice.TTSFromArgv(spk.Argv()); t != nil {
			return t
		}
	}
	return voice.DetectTTS()
}

// voiceState: what the mic is doing right now (composer glyph + routing).
type voiceState int

const (
	voiceIdle voiceState = iota
	voiceListening
	voiceTranscribing
	voiceSpeaking // voice mode: the reply is being read aloud
)

// voiceSpeechDoneMsg signals the spoken reply finished (or was cut off);
// conversation mode then returns to listening.
type voiceSpeechDoneMsg struct{ gen int }

// dictateOnce records one utterance and submits it as a normal turn. The
// reply stays text. Clicking again WHILE listening stops the recording
// (transcribing what was heard so far). No-op while a turn is running.
func (m *model) dictateOnce() tea.Cmd {
	if m.voiceMic == voiceListening {
		m.stopListening("⏺ stopped — transcribing what was heard")
		return nil
	}
	if m.state != stInput {
		m.note("dictation: wait for the current turn to finish")
		return nil
	}
	return m.startListening(false)
}

// toggleVoice turns conversation mode on/off, reporting why if unavailable.
// On: starts listening immediately (that IS the mode). While the reply is
// being SPOKEN, a click skips the speech and returns to the mic ("got it,
// let me talk") — only a click in the listening/idle states exits the mode.
func (m *model) toggleVoice() tea.Cmd {
	if m.voiceOn {
		if m.voiceMic == voiceSpeaking {
			m.stopSpeaking() // Speak returns → voiceSpeechDoneMsg → relisten
			return nil
		}
		m.exitVoiceMode("conversation mode off")
		return nil
	}
	if m.stt == nil || !m.stt.Available() {
		m.note("voice input unavailable — need a recorder (arecord) + whisper.cpp + a model (see EIGEN_WHISPER_BIN/MODEL)")
		return nil
	}
	m.voiceOn = true
	spoken := "replies will be spoken"
	if m.tts == nil || !m.tts.Available() {
		spoken = "no TTS found — replies shown as text (set EIGEN_VOICE_TTS_CMD)"
	}
	m.note("conversation mode ON — just talk; click ◉ again (or esc) to exit; " + spoken)
	if m.state == stInput {
		return m.startListening(true)
	}
	return nil // mid-turn: the turn-done path starts the listen loop
}

// stopListening cancels the in-flight recording. The VAD recorder returns
// whatever speech it captured, so "stop" means "I'm done talking" — the
// transcript still arrives via voiceSpokenMsg (same generation: NOT stale).
func (m *model) stopListening(note string) {
	if m.voiceStop != nil {
		m.voiceStop()
		m.voiceStop = nil
	}
	if m.voiceMic == voiceListening {
		m.voiceMic = voiceTranscribing
	}
	if note != "" {
		m.note(note)
	}
}

// exitVoiceMode stops conversation mode: cancels listening/speech, bumps the
// epoch so in-flight recordings/timers die stale (the codex patch.js epoch
// guard), and discards any pending dictation.
func (m *model) exitVoiceMode(note string) {
	m.voiceOn = false
	m.voiceGen++
	m.stopSpeaking()
	if m.voiceStop != nil {
		m.voiceStop()
		m.voiceStop = nil
	}
	m.voiceMic = voiceIdle
	if note != "" {
		m.note(note)
	}
}

// startListening kicks off one VAD-endpointed recording off the UI loop.
// conv marks it as a conversation-mode leg (reply spoken, then listen again).
func (m *model) startListening(conv bool) tea.Cmd {
	if m.stt == nil || !m.stt.Available() {
		m.note("voice input unavailable — need a recorder (arecord) + whisper.cpp + a model (see EIGEN_WHISPER_BIN/MODEL)")
		return nil
	}
	if m.voiceMic == voiceListening || m.voiceMic == voiceTranscribing {
		return nil // already capturing speech
	}
	m.stopSpeaking()
	m.voiceMic = voiceListening
	m.voiceGen++
	gen := m.voiceGen
	lctx, cancel := context.WithCancel(m.ctx)
	m.voiceStop = cancel
	stt := m.stt
	return func() tea.Msg {
		text, err := stt.Listen(lctx)
		return voiceSpokenMsg{text: text, err: err, conv: conv, gen: gen}
	}
}

// handleSpoken routes a finished recording: stale generations are dropped
// (mode exited / a newer recording started), dictation submits as a turn.
func (m *model) handleSpoken(msg voiceSpokenMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.voiceGen {
		return m, nil // stale: mode exited or superseded
	}
	m.voiceMic = voiceIdle
	m.voiceStop = nil
	if msg.err != nil {
		if msg.conv {
			m.exitVoiceMode("voice: " + msg.err.Error() + " — conversation mode off")
		} else {
			m.note("voice: " + msg.err.Error())
		}
		return m, nil
	}
	text := strings.TrimSpace(msg.text)
	if text == "" {
		if msg.conv && m.voiceOn {
			// Nothing heard: keep listening (hands-free loop continues).
			return m, m.startListening(true)
		}
		m.note("voice: nothing heard")
		return m, nil
	}
	// Submit the transcript as a normal turn (spoken input → message).
	return m, m.submit(text)
}

// voiceTurnDone hooks the end of a turn: in conversation mode, speak the
// answer, THEN listen again. Speaking and listening are SEQUENCED — the
// returned command blocks on the speech (cancelable: esc / button / a new
// recording) and reports voiceSpeechDoneMsg, which starts the next listen.
// Starting the mic immediately would kill the speech via stopSpeaking — the
// "never reads back" bug. Returns nil when voice mode is off (callers fall
// through to the normal read-aloud toggle path).
func (m *model) voiceTurnDone(err error) tea.Cmd {
	if !m.voiceOn {
		return nil
	}
	ans := ""
	if err == nil {
		ans = m.lastAssistantText()
	}
	if ans == "" || m.tts == nil || !m.tts.Available() {
		return m.startListening(true) // nothing to speak — straight back to the mic
	}
	m.stopSpeaking()
	ctx, cancel := context.WithCancel(m.ctx)
	m.voiceCancel = cancel
	m.voiceMic = voiceSpeaking
	gen := m.voiceGen
	tts := m.tts
	speak := func() tea.Msg {
		_ = tts.Speak(ctx, ans)
		return voiceSpeechDoneMsg{gen: gen}
	}
	// Interrupt-on-speech (the codex conversation-mode monitor): while the
	// reply plays, watch the mic with a HIGHER threshold; the user talking
	// over it cuts the speech. The monitor shares the speech ctx, so it dies
	// with the speech (done, skipped, or mode exit) — and cancel() makes
	// Speak return, which delivers the SAME voiceSpeechDoneMsg that starts
	// the next listen. No extra message type needed.
	if mon, ok := m.stt.(voice.InterruptMonitor); ok {
		monitor := func() tea.Msg {
			if mon.MonitorInterrupt(ctx) {
				cancel() // cut the speech; its speechDoneMsg relistens
			}
			return nil
		}
		return tea.Batch(speak, monitor)
	}
	return speak
}

// speakLastAnswer speaks the most recent assistant answer once (the read-aloud
// button: write, click, listen — no persistent toggle, no voice mode).
func (m *model) speakLastAnswer() {
	if m.speaker == nil || !m.speaker.Available() {
		m.note("no TTS command found (set tts_cmd in /config or install espeak-ng)")
		return
	}
	ans := m.lastAssistantText()
	if ans == "" {
		m.note("nothing to read yet")
		return
	}
	m.speaker.Stop()
	m.speaker.Speak(ans)
	m.note("reading answer aloud")
}

// stopSpeaking cancels in-flight TTS (used on interrupt / new utterance / exit).
func (m *model) stopSpeaking() {
	if m.voiceCancel != nil {
		m.voiceCancel()
		m.voiceCancel = nil
	}
	if m.speaker != nil {
		m.speaker.Stop()
	}
}

// micGlyph renders the conversation-mode button state.
func (m *model) micGlyph() string {
	switch {
	case m.voiceOn && m.voiceMic == voiceListening:
		return "● listening"
	case m.voiceOn && m.voiceMic == voiceTranscribing:
		return "◌ thinking"
	case m.voiceOn && m.voiceMic == voiceSpeaking:
		return "▷ speaking"
	case m.voiceOn:
		return "◉ voice on"
	default:
		return "◉ voice"
	}
}

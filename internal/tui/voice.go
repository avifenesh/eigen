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
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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

// voiceState: what the mic is doing right now (sidebar glyph + routing).
type voiceState int

const (
	voiceIdle voiceState = iota
	voiceListening
	voiceTranscribing
)

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
// On: starts listening immediately (that IS the mode). Off: stops everything.
func (m *model) toggleVoice() tea.Cmd {
	if m.voiceOn {
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
	if m.voiceMic != voiceIdle {
		return nil // already listening/transcribing
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
// answer then listen again. Returns false when voice mode is off (callers
// fall through to the normal read-aloud toggle path).
func (m *model) voiceTurnDone(err error) tea.Cmd {
	if !m.voiceOn {
		return nil
	}
	if err == nil {
		if ans := m.lastAssistantText(); ans != "" {
			m.speakAnswer(ans)
		}
	}
	// Listen for the next utterance while/after the reply speaks; the VAD
	// threshold keeps quiet playback from triggering, and speaking again
	// interrupts via startListening's stopSpeaking.
	return m.startListening(true)
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

// speakAnswer speaks text via the voice-mode TTS off the UI loop, cancelable.
func (m *model) speakAnswer(text string) {
	if m.tts == nil || !m.tts.Available() || text == "" {
		return
	}
	m.stopSpeaking()
	ctx, cancel := context.WithCancel(m.ctx)
	m.voiceCancel = cancel
	tts := m.tts
	go func() {
		_ = tts.Speak(ctx, text)
	}()
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
	case m.voiceOn:
		return "◉ voice on"
	default:
		return "◉ voice"
	}
}

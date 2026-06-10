package tui

// Conversation mode: spoken input (push-to-talk) and spoken replies. The voice
// backends (STT/TTS) shell out to local tools; this file is the TUI glue —
// /voice toggles the mode, ctrl+t records-and-submits a spoken turn, and each
// completed assistant answer is spoken aloud. Speech runs off the UI loop and
// is cancelable so a new utterance interrupts the current reply.

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

// voiceSpokenMsg carries a transcribed utterance back to the UI loop.
type voiceSpokenMsg struct {
	text string
	err  error
}

// toggleVoice turns conversation mode on/off, reporting why if unavailable.
func (m *model) toggleVoice() {
	if m.voiceOn {
		m.stopSpeaking()
		m.voiceOn = false
		m.note("conversation mode off")
		return
	}
	if m.stt == nil || !m.stt.Available() {
		m.note("voice input unavailable — need a recorder (arecord) + whisper.cpp + a model (see EIGEN_WHISPER_BIN/MODEL)")
		return
	}
	m.voiceOn = true
	spoken := "replies will be spoken"
	if m.tts == nil || !m.tts.Available() {
		spoken = "no TTS found — replies shown as text (set EIGEN_VOICE_TTS_CMD)"
	}
	m.note("conversation mode ON — ctrl+t to talk; " + spoken)
}

// listen records a spoken utterance and returns it as a message. It is a
// command (runs off the UI loop). Stops any current speech first (interrupt).
func (m *model) listen() tea.Cmd {
	if m.stt == nil || !m.stt.Available() {
		return nil
	}
	m.stopSpeaking()
	m.status = "listening…"
	stt := m.stt
	ctx := m.ctx
	return func() tea.Msg {
		text, err := stt.Listen(ctx)
		return voiceSpokenMsg{text: text, err: err}
	}
}

// speakAnswer speaks text aloud off the UI loop, cancelable via stopSpeaking.
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
}

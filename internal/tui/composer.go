package tui

// The composer bar (Tier 15): voice controls anchored AT the input — basic
// design: mic things live where you type, not in a nav list. One row directly
// under the input box: ⏺ speak (dictate once) · ▶ read (speak last answer) ·
// ◉ voice (conversation mode, shows live mic state). Renderer and click
// hit-test share one segment layout (the status-bar convention) so geometry
// cannot drift.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// composerSeg is one clickable control on the composer bar.
type composerSeg struct {
	text   string
	lit    bool // accent styling (active state)
	action actionID
}

// composerParts assembles the bar's segments with live state. While a
// dictation recording is live the ⏺ button BECOMES the stop control — the
// label says so (nothing should ever look stuck on "listening" with no exit).
// In conversation mode a ⊘ mute segment appears: muted = stay in the
// conversation, replies speak, mic parked.
func (m *model) composerParts() []composerSeg {
	speak := composerSeg{text: "⏺ speak", action: actDictate}
	if !m.voiceOn {
		switch m.voiceMic {
		case voiceListening:
			speak = composerSeg{text: "⏺ stop · listening…", action: actDictate, lit: true}
		case voiceTranscribing:
			speak = composerSeg{text: "◌ transcribing…", action: actDictate, lit: true}
		}
	}
	segs := []composerSeg{
		speak,
		{text: "▶ read", action: actSpeakAnswer},
		{text: m.micGlyph(), action: actVoiceToggle, lit: m.voiceOn},
	}
	if m.voiceOn {
		mute := composerSeg{text: "⊘ mute", action: actVoiceMute}
		if m.voiceMuted {
			mute = composerSeg{text: "⊘ muted", action: actVoiceMute, lit: true}
		}
		segs = append(segs, mute)
	}
	return segs
}

// composerBarVisible: the bar renders when there's room for chrome at all.
// On very short terminals every row matters more than mic buttons.
func (m *model) composerBarVisible() bool {
	return m.height >= 12 && m.width >= 40
}

// composerBarView renders the single bar row, right-aligned under the input
// box so it reads as part of the composer, dim by default with active states
// lit.
func (m *model) composerBarView() string {
	var b strings.Builder
	w := 0
	for i, s := range m.composerParts() {
		if i > 0 {
			b.WriteString(dim(" · "))
			w += 3
		}
		if s.lit {
			b.WriteString(styleAccent.Render(s.text))
		} else {
			b.WriteString(dim(s.text))
		}
		w += ansi.StringWidth(s.text)
	}
	// Right-align inside the input box's width (the box has a 1-col border +
	// padding feel; 2 cols in from the right edge matches the rounded corner).
	pad := m.width - w - 3
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + b.String()
}

// composerActionAt maps a click on the bar row (bar-local x) to its segment's
// action, mirroring composerBarView's exact column math.
func (m *model) composerActionAt(x int) actionID {
	segs := m.composerParts()
	// Recompute the leading pad.
	w := 0
	for i, s := range segs {
		if i > 0 {
			w += 3
		}
		w += ansi.StringWidth(s.text)
	}
	col := m.width - w - 3
	if col < 0 {
		col = 0
	}
	for i, s := range segs {
		if i > 0 {
			col += 3 // " · "
		}
		sw := ansi.StringWidth(s.text)
		if x >= col && x < col+sw {
			return s.action
		}
		col += sw
	}
	return actNone
}

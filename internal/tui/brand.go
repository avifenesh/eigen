package tui

import (
	"github.com/avifenesh/eigen/internal/theme"
	"github.com/charmbracelet/lipgloss"
)

// The eigen mark: λ (lambda) — THE eigenvalue symbol (A·v = λ·v). It's the
// project's face: the wordmark prefix (top-left in the sidebar) and the
// empty-transcript welcome. Deliberately NOT a radiating sunburst (Claude) or
// a sparkle (others) — eigen's identity is the linear-algebra λ.
//
// The working loader is a BREATHING λ: the mark inhales to bright and exhales
// to faint, in place, like a slow heartbeat — paired with a soft dot after the
// caret that pulses in time. The mark IS the loader; nothing slides, so the
// status text beside it never jitters.
const brandGlyph = "λ"

// breathRamp is the λ's brightness cycle while working — theme-owned (see
// theme.BreathRamp): faint → dim → accent → bright → accent → dim → loop.
var breathRamp = theme.BreathRamp

// workingRamp is the brightness ramp for the working λ (orange), theme-owned.
var workingRamp = theme.WorkingRamp

// breathDot is the synced beat after the caret: faint on the exhale, lit
// (Working orange) at the peak of the inhale — a readable pulse even on a
// low-contrast terminal. Indexed by the same frame as breathRamp.
func breathDot(frame int) string {
	// Lit on the inhale peak (frames 2..4 of the 6-frame cycle), faint else.
	switch frame % len(breathRamp) {
	case 2, 3, 4:
		return styleWorking.Render("•")
	default:
		return theme.SFaint.Render("·")
	}
}

// breathingLambda renders the λ at the brightness for the given frame (bold so
// the glow reads at one cell).
func breathingLambda(frame int) string {
	c := breathRamp[frame%len(breathRamp)]
	return lipgloss.NewStyle().Foreground(c).Bold(true).Render(brandGlyph)
}

// workingLambda renders a breathing λ on the WORKING ramp (orange) — the ONE
// "a session is working" signature, shared by the chat rail and the app
// shell's live sessions so the motion reads identically across surfaces.
func workingLambda(frame int) string {
	c := workingRamp[frame%len(workingRamp)]
	return lipgloss.NewStyle().Foreground(c).Bold(true).Render(brandGlyph)
}

// loaderView is the full working loader: a breathing λ, the steady caret, and
// the synced dot — constant width, anchored, no sliding.
func loaderView(frame int) string {
	return breathingLambda(frame) + styleAccent.Render("❯") + breathDot(frame)
}

// brandMark is the top-left identity: the static λ when idle, the breathing λ
// while a turn runs — the mark is alive exactly when the agent is.
func (m *model) brandMark() string {
	if m.state == stRunning {
		return breathingLambda(m.brandTick)
	}
	return styleAccent.Bold(true).Render(brandGlyph)
}

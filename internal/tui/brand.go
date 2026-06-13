package tui

// The eigen mark: λ (lambda) — THE eigenvalue symbol (A·v = λ·v). It's the
// project's face: the wordmark prefix (top-left in the sidebar) and the empty-
// transcript welcome. Deliberately NOT a radiating sunburst (Claude) or a
// sparkle (others) — eigen's identity is the linear-algebra λ.
//
// The working loader is a rotating vector: a line sweeping through its angles
// (│ ╱ ─ ╲), evoking an eigenvector turning under a transform. A classic
// line-spinner idiom — motion that's on-theme without copying any tool's mark.
const brandGlyph = "λ"

// brandSweep is the working animation: a vector rotating through 180° in 45°
// steps. Single display cell per frame (width 1 to the layout math) so it
// never disturbs the band.
var brandSweep = []string{"│", "╱", "─", "╲"}

// brandFrame returns the rotating-vector frame for tick i (wraps).
func brandFrame(i int) string {
	if len(brandSweep) == 0 {
		return brandGlyph
	}
	return brandSweep[i%len(brandSweep)]
}

// brandMark is the top-left identity: the static λ when idle, the rotating
// vector while a turn runs — the mark is alive exactly when the agent is.
func (m *model) brandMark() string {
	if m.state == stRunning {
		return brandFrame(m.brandTick)
	}
	return brandGlyph
}

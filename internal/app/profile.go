package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/avifenesh/eigen/internal/observe"
	tea "github.com/charmbracelet/bubbletea"
)

// profileState is the user's cross-session profile surface: usage totals plus a
// single editable personalization prompt stored in global memory as USER.md.
type profileState struct {
	editing bool
	input   string
	status  string
	err     string
	clicks  clickMap
}

func (s *profileState) init(*Data) {}

func (s *profileState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if s.editing {
		s.status, s.err = "", ""
		switch key {
		case "esc":
			s.editing = false
			s.input = ""
		case "enter":
			if m.data == nil || m.data.GlobalMem == nil {
				s.err = "global memory unavailable"
				return m, nil
			}
			if err := m.data.GlobalMem.WriteUserProfile(s.input); err != nil {
				s.err = "save failed: " + err.Error()
				return m, nil
			}
			s.editing = false
			s.input = ""
			s.status = "profile prompt saved ✓"
		case "backspace":
			if len(s.input) > 0 {
				s.input = s.input[:len(s.input)-1]
			}
		default:
			if len(msg.Runes) > 0 {
				s.input += string(msg.Runes)
			} else if key == "space" || key == " " {
				s.input += " "
			}
		}
		return m, nil
	}
	switch key {
	case "e", "a", "enter":
		if m.data == nil || m.data.GlobalMem == nil {
			s.err = "global memory unavailable"
			break
		}
		s.editing = true
		s.input = strings.TrimSpace(m.data.GlobalMem.UserProfile())
		s.status, s.err = "", ""
	case "R":
		s.status, s.err = "refreshed", ""
	}
	return m, nil
}

func (s *profileState) view(m *Model, w, h int) string {
	out := pageTitle("profile", "usage and personalization prompt", w)
	out += profileUsageSummary(m.data, w)
	out += profilePromptView(s, m.data, w, h)
	return out
}

func profileUsageSummary(d *Data, w int) string {
	if d == nil {
		return sFaint.Render("  usage unavailable") + "\n\n"
	}
	obs := d.Observe
	in, outTok, cacheRead, cacheWrite, turns := 0, 0, 0, 0, 0
	for _, m := range obs.Models {
		turns += m.Turns
		in += m.InTokens
		outTok += m.OutTokens
		cacheRead += m.CacheReadTokens
		cacheWrite += m.CacheWriteTokens
	}
	parts := []string{
		fmt.Sprintf("%d sessions", len(d.Sessions)),
		fmt.Sprintf("%d projects", len(d.Projects)),
		fmt.Sprintf("%d turns", turns),
		fmt.Sprintf("%d events", obs.Records),
	}
	if in+outTok > 0 {
		parts = append(parts, fmt.Sprintf("tokens %d/%d", in, outTok))
	}
	if cacheRead+cacheWrite > 0 {
		parts = append(parts, fmt.Sprintf("cache %d/%d", cacheRead, cacheWrite))
	}
	if errors := countTotal(obs.Errors); errors > 0 {
		parts = append(parts, fmt.Sprintf("%d errors", errors))
	}
	out := "  " + sectionLabel("usage statistics", min(w, 70)-2) + "\n"
	out += "  " + sViolet.Render(truncate(strings.Join(parts, "  ·  "), max(20, w-4))) + "\n"
	if len(obs.Models) == 0 {
		out += "  " + sFaint.Render("no model usage recorded yet") + "\n\n"
		return out
	}
	out += "\n  " + sDim.Render("top models") + "\n"
	keys := profileTopModelKeys(obs.Models, 3)
	for _, k := range keys {
		m := obs.Models[k]
		line := fmt.Sprintf("%s turns=%d tokens=%d/%d cache=%d/%d", pad(truncate(k, 28), 28), m.Turns, m.InTokens, m.OutTokens, m.CacheReadTokens, m.CacheWriteTokens)
		out += "  " + truncate(line, w-2) + "\n"
	}
	return out + "\n"
}

func profilePromptView(s *profileState, d *Data, w, h int) string {
	out := "  " + sectionLabel("personalization prompt", min(w, 70)-2) + "\n"
	if d == nil || d.GlobalMem == nil {
		return out + sFaint.Render("  global memory unavailable") + "\n"
	}
	profile := strings.TrimSpace(d.GlobalMem.UserProfile())
	if s.editing {
		text := s.input
		if text == "" {
			text = "(empty)"
		}
		out += "\n"
		lines := memoryDetailLines(text, w)
		visible := max(3, h-12)
		for i, line := range lines {
			if i >= visible {
				out += sFaint.Render(fmt.Sprintf("  ⋯ %d more lines", len(lines)-i)) + "\n"
				break
			}
			out += "  " + sText.Render(truncate(line, w-4)) + "\n"
		}
		out += "  " + sAccent.Render("▏") + "\n"
		if s.err != "" {
			out += sErr.Render("  "+truncate(s.err, max(20, w-2))) + "\n"
		}
		out += sFaint.Render("  type one prompt · enter save · esc cancel")
		return out
	}
	if profile == "" {
		out += "\n" + sFaint.Render("  empty — press e to write the one prompt Eigen should use to personalize to you") + "\n"
	} else {
		out += profilePromptSummary(profile, w, h)
	}
	if s.err != "" {
		out += "\n" + sErr.Render("  "+truncate(s.err, max(20, w-2)))
	} else if s.status != "" {
		out += "\n" + sOk.Render("  "+truncate(s.status, max(20, w-2)))
	} else {
		out += "\n" + sFaint.Render("  e edit profile prompt · R refresh")
	}
	return out
}

func profilePromptSummary(profile string, w, h int) string {
	lines := memoryDetailLines(profile, w)
	visible := max(3, h-13)
	var b strings.Builder
	b.WriteString("\n")
	for i, line := range lines {
		if i >= visible {
			b.WriteString(sFaint.Render(fmt.Sprintf("  ⋯ %d more lines", len(lines)-i)) + "\n")
			break
		}
		b.WriteString("  " + sText.Render(truncate(line, w-4)) + "\n")
	}
	return b.String()
}

func (s *profileState) clickAt(m *Model, _ int) (tea.Cmd, bool) {
	if s.editing || m.data == nil || m.data.GlobalMem == nil {
		return nil, false
	}
	s.editing = true
	s.input = strings.TrimSpace(m.data.GlobalMem.UserProfile())
	s.status, s.err = "", ""
	return nil, true
}

func profileTopModelKeys(models map[string]observe.ModelSummary, limit int) []string {
	keys := make([]string, 0, len(models))
	for k := range models {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := models[keys[i]].InTokens + models[keys[i]].OutTokens
		right := models[keys[j]].InTokens + models[keys[j]].OutTokens
		if left == right {
			return keys[i] < keys[j]
		}
		return left > right
	})
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}
	return keys
}

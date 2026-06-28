package memory

import "strings"

// SplitNotes breaks curated MEMORY.md into renderable entries (same rules as the GUI).
func SplitNotes(content string) []string {
	return splitNotesForEdit(content)
}

func splitOnTopLevelHeadings(content string) []string {
	lines := strings.Split(content, "\n")
	var sections []string
	var cur []string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		if s := strings.TrimSpace(strings.Join(cur, "\n")); s != "" {
			sections = append(sections, s)
		}
		cur = nil
	}
	started := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, "## ") {
			started = true
			flush()
			cur = []string{ln}
			continue
		}
		if started {
			cur = append(cur, ln)
		}
	}
	if !started {
		return nil
	}
	flush()
	return sections
}

func splitOnBlankLines(content string) []string {
	chunks := strings.Split(content, "\n\n")
	out := make([]string, 0, len(chunks))
	skippedHeading := false
	for _, c := range chunks {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !skippedHeading {
			skippedHeading = true
			if isTopLevelHeading(c) {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

func isTopLevelHeading(chunk string) bool {
	if strings.ContainsRune(chunk, '\n') {
		return false
	}
	return strings.HasPrefix(chunk, "# ")
}
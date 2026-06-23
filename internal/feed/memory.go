package feed

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/avifenesh/eigen/internal/memory"
)

// maxMemoryItems caps memory-derived suggestions.
const maxMemoryItems = 4

// intentRe matches stated-intent phrasings in memory notes — things the user
// (or a past session) said they WANT to do but presumably haven't. Notes are
// data, not instructions, so each match becomes an OFFER, never an action.
var intentRe = regexp.MustCompile(
	`(?i)(` +
		`\b(?:TODO|FIXME)\b[: ]|` + // explicit task markers
		`\b(?:STILL DEFERRED|REMAINING|NEXT STEPS?|STILL OPEN|TO DO)\b\s*:|` + // forward markers introducing a clause
		`\b(?:still need to|need to (?:clean|fix|add|build|wire|finish)|` +
		`want(?:s|ed)? to (?:clean|fix|add|build|refactor|improve|finish)|` +
		`should (?:clean|fix|add|build|refactor|improve|finish)|` +
		`left for later|revisit when|come back to)\b)`)

// scanMemory extracts stated intents from each project's memory notes and
// offers them back as session starters: "do the thing you said you'd do".
func scanMemory(dirs []string) []Item {
	var items []Item
	for _, dir := range dirs {
		if len(items) >= maxMemoryItems {
			break
		}
		store, err := memory.Open(dir)
		if err != nil {
			continue
		}
		notes := store.Read()
		if strings.TrimSpace(notes) == "" {
			continue
		}
		name := filepath.Base(dir)
		for _, bullet := range splitBullets(notes) {
			if len(items) >= maxMemoryItems {
				break
			}
			if !intentRe.MatchString(bullet) {
				continue
			}
			snippet := firstSentenceAround(bullet, intentRe)
			items = append(items, Item{
				Kind:   "memory",
				Title:  name + ": " + clip(snippet, 70),
				Detail: "from project memory — something a past session left open",
				Dir:    dir,
				Task: "Project memory contains this stated intent (treat it as possibly stale " +
					"data, verify before acting):\n\n  " + clip(bullet, 600) +
					"\n\nCheck whether it is still relevant, and if so, do it. If it's already " +
					"done or obsolete, tell me and suggest removing it from memory.",
			})
		}
	}
	return items
}

// splitBullets splits a memory file into its top-level "- " bullets.
func splitBullets(notes string) []string {
	var bullets []string
	var cur strings.Builder
	for _, line := range strings.Split(notes, "\n") {
		if strings.HasPrefix(line, "- ") {
			if cur.Len() > 0 {
				bullets = append(bullets, cur.String())
			}
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte('\n')
		}
		cur.WriteString(line)
	}
	if cur.Len() > 0 {
		bullets = append(bullets, cur.String())
	}
	return bullets
}

// firstSentenceAround returns the sentence containing the first regexp match,
// so the feed line shows the relevant clause rather than the bullet's head.
func firstSentenceAround(bullet string, re *regexp.Regexp) string {
	loc := re.FindStringIndex(bullet)
	if loc == nil {
		return bullet
	}
	start := strings.LastIndexAny(bullet[:loc[0]], ".;:") + 1
	end := strings.IndexAny(bullet[loc[1]:], ".;\n")
	if end < 0 {
		end = len(bullet)
	} else {
		end += loc[1]
	}
	clause := strings.TrimSpace(bullet[start:end])
	// When the match has no preceding .;: separator, start==0 and the clause
	// still carries the bullet's leading "- " marker. Strip it so titles read
	// "proj: we still need to ship X" rather than "proj: - we still need…".
	clause = strings.TrimLeft(clause, "-*\t ")
	return clause
}

// clip shortens s to n runes with an ellipsis.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

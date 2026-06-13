package fuzzy

import "strings"

// Score ranks how well query q matches s (both lowercased by the caller or
// here): substring matches beat subsequence matches, earlier starts beat
// later ones. Returns -1 when q doesn't match at all, 0 is the best score.
// Shared by the palette, session search, and the switcher so every "type to
// find" surface ranks identically.
func Score(s, q string) int {
	s = strings.ToLower(s)
	q = strings.ToLower(q)
	if q == "" {
		return 0
	}
	if idx := strings.Index(s, q); idx >= 0 {
		return idx // substring: rank by how early it starts
	}
	// Subsequence: every rune of q appears in order. Penalized behind any
	// substring hit by a large offset.
	pos := 0
	for _, r := range q {
		i := strings.IndexRune(s[pos:], r)
		if i < 0 {
			return -1
		}
		pos += i + 1
	}
	return 1000 + pos
}

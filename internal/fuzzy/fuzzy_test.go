package fuzzy

import "testing"

func TestScore(t *testing.T) {
	// Empty query matches anything at best score.
	if Score("anything", "") != 0 {
		t.Fatal("empty query should score 0")
	}
	// No match.
	if Score("config", "xyz") >= 0 {
		t.Fatal("non-matching query should be negative")
	}
	// Substring beats subsequence.
	sub := Score("config panel", "config")
	seq := Score("config panel", "cfg")
	if sub < 0 || seq < 0 {
		t.Fatal("both should match")
	}
	if !(sub < seq) {
		t.Fatalf("substring (%d) should rank better than subsequence (%d)", sub, seq)
	}
	// Earlier substring start ranks better.
	early := Score("voice mode", "voice")
	late := Score("the voice", "voice")
	if !(early < late) {
		t.Fatalf("earlier start (%d) should beat later (%d)", early, late)
	}
	// Case-insensitive.
	if Score("Refactor Daemon", "daemon") < 0 {
		t.Fatal("match should be case-insensitive")
	}
}

func TestScoreSubsequenceOrder(t *testing.T) {
	// Subsequence requires order: "ba" does NOT match "abc".
	if Score("abc", "ba") >= 0 {
		t.Fatal("subsequence must respect order")
	}
	if Score("abc", "ac") < 0 {
		t.Fatal("'ac' is an in-order subsequence of 'abc'")
	}
}

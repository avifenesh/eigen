package tui

import "testing"

func TestSplitBan(t *testing.T) {
	cases := map[string][2]string{
		"No hedging: don't start with I think": {"No hedging", "don't start with I think"},
		"No X | do not do X":                   {"No X", "do not do X"},
		"Title - the rule":                     {"Title", "the rule"},
		"noseparator":                          {"", ""},
	}
	for in, want := range cases {
		ti, ru := splitBan(in)
		if ti != want[0] || ru != want[1] {
			t.Errorf("splitBan(%q) = (%q,%q), want %v", in, ti, ru, want)
		}
	}
}

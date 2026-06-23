package main

import "testing"

// TestMergeTaskStdin pins the task/stdin precedence: piped stdin is never
// dropped — with no positional task it becomes the task, with one it is
// appended below (APP-062).
func TestMergeTaskStdin(t *testing.T) {
	cases := []struct {
		name  string
		task  string
		piped string
		want  string
	}{
		{"stdin only", "", "from stdin", "from stdin"},
		{"task only", "from arg", "", "from arg"},
		{"both appended", "summarize this", "the piped body", "summarize this\n\nthe piped body"},
		{"both empty", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mergeTaskStdin(tc.task, tc.piped); got != tc.want {
				t.Errorf("mergeTaskStdin(%q, %q) = %q, want %q", tc.task, tc.piped, got, tc.want)
			}
		})
	}
}

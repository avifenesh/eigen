package memory

import (
	"strings"
	"testing"
)

func TestRedactPatterns(t *testing.T) {
	cases := []struct {
		in       string
		mustLose []string // substrings that must NOT survive
		mustKeep []string // substrings that must survive
	}{
		{
			in:       "use AKIA_REDACTED_EXAMPLE for s3",
			mustLose: []string{"AKIA_REDACTED_EXAMPLE"},
			mustKeep: []string{"for s3"},
		},
		{
			in:       "gh token ghp_REDACTED_EXAMPLE",
			mustLose: []string{"ghp_REDACTED_EXAMPLE"},
		},
		{
			in:       "OPENAI sk_REDACTED_EXAMPLE",
			mustLose: []string{"sk_REDACTED_EXAMPLE"},
		},
		{
			in:       "export GLM_API_KEY=d41d8cd98f00b204e9800998ecf8427e",
			mustLose: []string{"d41d8cd98f00b204e9800998ecf8427e"},
			mustKeep: []string{"GLM_API_KEY="},
		},
		{
			in:       "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			mustLose: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
			mustKeep: []string{"Bearer"},
		},
		{
			in:       "BEGIN_RSA_PRIVATE_KEY_TEST_MARKER\nMIIB\nEND_RSA_PRIVATE_KEY_TEST_MARKER",
			mustLose: []string{"MIIB"},
		},
		{
			// Benign text must pass through untouched.
			in:       "run go test ./... and check the token count in the status bar",
			mustKeep: []string{"go test ./...", "token count"},
		},
		{
			// Short values after token-ish names are not credentials.
			in:       "set search=on and token=off",
			mustKeep: []string{"search=on", "token=off"},
		},
	}
	for _, c := range cases {
		got := Redact(c.in)
		for _, lose := range c.mustLose {
			if strings.Contains(got, lose) {
				t.Errorf("Redact(%q) kept secret %q: %q", c.in, lose, got)
			}
		}
		for _, keep := range c.mustKeep {
			if !strings.Contains(got, keep) {
				t.Errorf("Redact(%q) lost benign text %q: %q", c.in, keep, got)
			}
		}
	}
}

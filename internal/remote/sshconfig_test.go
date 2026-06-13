package remote

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectSSHHosts(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
	os.WriteFile(cfg, []byte(`
# comment
Host *
    ForwardAgent yes
Host devbox
    HostName 10.0.0.5
    User deploy
Host gpu1 gpu2
    HostName gpu.example.com
Host bad-*-glob
    HostName nope
`), 0o600)
	got := detectSSHHostsFrom(cfg)
	// Expect devbox, gpu1, gpu2 (sorted); NOT `*` or the glob.
	names := map[string]DetectedHost{}
	for _, h := range got {
		names[h.Name] = h
	}
	if len(got) != 3 {
		t.Fatalf("want 3 concrete hosts, got %d: %+v", len(got), got)
	}
	if names["devbox"].HostName != "10.0.0.5" || names["devbox"].User != "deploy" {
		t.Errorf("devbox not parsed: %+v", names["devbox"])
	}
	if _, ok := names["gpu1"]; !ok {
		t.Error("multi-alias Host line should yield gpu1")
	}
	if _, ok := names["*"]; ok {
		t.Error("wildcard Host * must be skipped")
	}
}

func TestSplitSSHDirective(t *testing.T) {
	for _, c := range []struct{ in, k, v string }{
		{"HostName 1.2.3.4", "HostName", "1.2.3.4"},
		{"HostName=1.2.3.4", "HostName", "1.2.3.4"},
		{"User  deploy", "User", "deploy"},
	} {
		k, v, ok := splitSSHDirective(c.in)
		if !ok || k != c.k || v != c.v {
			t.Errorf("splitSSHDirective(%q) = (%q,%q,%v)", c.in, k, v, ok)
		}
	}
}

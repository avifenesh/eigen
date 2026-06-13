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

func TestMachinesUseAliasNotResolvedHost(t *testing.T) {
	// Detected hosts must use the ssh-config ALIAS as the SSH target so ssh
	// reads the Host block (IdentityFile/User/ProxyJump) — resolving to
	// user@hostname drops the key and breaks install/connect. Regression for
	// the "install fails" bug.
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, ".ssh", "config")
	os.WriteFile(cfg, []byte("Host dev\n    HostName 10.0.0.9\n    User ubuntu\n    IdentityFile ~/.ssh/dev.pem\n"), 0o600)

	ms := Machines()
	var dev *Machine
	for i := range ms {
		if ms[i].Name == "dev" {
			dev = &ms[i]
		}
	}
	if dev == nil {
		t.Fatal("dev not detected")
	}
	if dev.SSH != "dev" {
		t.Errorf("SSH target must be the alias 'dev' (so ssh uses IdentityFile), got %q", dev.SSH)
	}
	if dev.Addr != "ubuntu@10.0.0.9" {
		t.Errorf("Addr (display) = %q, want ubuntu@10.0.0.9", dev.Addr)
	}
}

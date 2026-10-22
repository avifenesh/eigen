package cred

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeKeyring installs cred-get / cred-set fakes backed by one file per
// credential in a temp dir, and returns that dir.
func fakeKeyring(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	kr := filepath.Join(dir, "kr")
	if err := os.Mkdir(kr, 0o700); err != nil {
		t.Fatal(err)
	}
	get := filepath.Join(dir, "cred-get")
	set := filepath.Join(dir, "cred-set")
	write := func(p, body string) {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(get, `[ -f "`+kr+`/$1" ] || { echo "$1 not found" >&2; exit 1; }
cat "`+kr+`/$1"
`)
	write(set, `printf '%s' "$*" > "`+dir+`/set-argv"
cat > "`+kr+`/$1"
`)
	t.Setenv("EIGEN_CRED_GET", get)
	t.Setenv("EIGEN_CRED_SET", set)
	return kr
}

func TestParseRef(t *testing.T) {
	for v, want := range map[string]string{
		"cred:glm-api-key": "glm-api-key",
		" cred:a.b_c-1 ":   "a.b_c-1",
		"cred:":            "",
		"cred:../x":        "",
		"sk-literal":       "",
		"cred:a b":         "",
	} {
		got, ok := ParseRef(v)
		if got != want || ok != (want != "") {
			t.Errorf("ParseRef(%q) = %q,%v want %q", v, got, ok, want)
		}
	}
}

func TestResolveEnvReplacesRefsAndUnsetsMissing(t *testing.T) {
	kr := fakeKeyring(t)
	if err := os.WriteFile(filepath.Join(kr, "tc-test-fake-a"), []byte("fake-value-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TC_FAKE_A", "cred:tc-test-fake-a")
	t.Setenv("TC_FAKE_MISSING", "cred:tc-test-fake-missing")
	t.Setenv("TC_FAKE_PLAIN", "plain-setting")
	errs := ResolveEnv()
	if got := os.Getenv("TC_FAKE_A"); got != "fake-value-a" {
		t.Fatalf("TC_FAKE_A = %q", got)
	}
	if _, set := os.LookupEnv("TC_FAKE_MISSING"); set {
		t.Fatal("an unresolved reference must be unset, not passed on literally")
	}
	if os.Getenv("TC_FAKE_PLAIN") != "plain-setting" {
		t.Fatal("plain values must stay")
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "TC_FAKE_MISSING") {
		t.Fatalf("errs = %v", errs)
	}
	if Resolved()["TC_FAKE_A"] != "tc-test-fake-a" {
		t.Fatalf("Resolved() = %v", Resolved())
	}
}

func TestStoreSendsValueOnStdinOnly(t *testing.T) {
	kr := fakeKeyring(t)
	if err := Store("tc-test-fake-b", "fake-secret-b"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(kr, "tc-test-fake-b"))
	if string(got) != "fake-secret-b" {
		t.Fatalf("stored %q", got)
	}
	argv, _ := os.ReadFile(filepath.Join(filepath.Dir(kr), "set-argv"))
	if strings.Contains(string(argv), "fake-secret-b") || string(argv) != "tc-test-fake-b" {
		t.Fatalf("cred-set argv = %q; the value must never be an argument", argv)
	}
	if v, err := Get("tc-test-fake-b"); err != nil || v != "fake-secret-b" {
		t.Fatalf("Get = %q, %v", v, err)
	}
}

func TestOffDisablesHelpers(t *testing.T) {
	t.Setenv("EIGEN_CRED_GET", "off")
	t.Setenv("EIGEN_CRED_SET", "off")
	if Available() {
		t.Fatal("Available() with EIGEN_CRED_SET=off")
	}
	if _, err := Get("x"); err == nil {
		t.Fatal("Get with EIGEN_CRED_GET=off must fail")
	}
}

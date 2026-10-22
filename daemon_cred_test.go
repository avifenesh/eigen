package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/cred"
)

func fakeCredHelpers(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	kr := filepath.Join(dir, "kr")
	_ = os.Mkdir(kr, 0o700)
	get, set := filepath.Join(dir, "cred-get"), filepath.Join(dir, "cred-set")
	// cred-get fails for every entry while kr/.locked exists, like a locked keyring.
	_ = os.WriteFile(get, []byte("#!/bin/sh\n[ -f \""+kr+"/.locked\" ] && exit 1\n[ -f \""+kr+"/$1\" ] || exit 1\ncat \""+kr+"/$1\"\n"), 0o755)
	_ = os.WriteFile(set, []byte("#!/bin/sh\nif [ \"$1\" = --new ]; then shift; [ -f \""+kr+"/$1\" ] && exit 3; fi\ncat > \""+kr+"/$1\"\n"), 0o755)
	t.Setenv("EIGEN_CRED_GET", get)
	t.Setenv("EIGEN_CRED_SET", set)
	for _, k := range credentialEnvKeys {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	return kr
}

// daemon install writes key names, never values.
func TestDaemonEnvLinesWriteReferencesNotValues(t *testing.T) {
	kr := fakeCredHelpers(t)
	_ = os.WriteFile(filepath.Join(kr, "tc-test-fake-glm"), []byte("fake-glm"), 0o600)
	t.Setenv("GLM_API_KEY", "fake-glm")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "fake-bearer")
	t.Setenv("AWS_REGION", "us-east-2")

	lines, notes, err := daemonEnvLines(map[string]string{"GLM_API_KEY": "tc-test-fake-glm"})
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(lines, "\n")
	for _, secret := range []string{"fake-glm", "fake-bearer"} {
		if strings.Contains(strings.ReplaceAll(all, "cred:tc-test-fake-glm", ""), secret) {
			t.Fatalf("daemon.env would hold a key value:\n%s", all)
		}
	}
	for _, want := range []string{"GLM_API_KEY=cred:tc-test-fake-glm", "AWS_BEARER_TOKEN_BEDROCK=cred:eigen-bedrock-bearer", "AWS_REGION=us-east-2"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in:\n%s", want, all)
		}
	}
	// The bearer was not in the keyring yet: it went there, not to the file.
	if b, _ := os.ReadFile(filepath.Join(kr, "eigen-bedrock-bearer")); string(b) != "fake-bearer" {
		t.Fatalf("bearer not stored in keyring: %q (notes %v)", b, notes)
	}
}

// A keyring entry with a different value is never silently repointed.
func TestDaemonEnvLinesRefusesMismatch(t *testing.T) {
	kr := fakeCredHelpers(t)
	_ = os.WriteFile(filepath.Join(kr, "glm-api-key"), []byte("fake-other"), 0o600)
	t.Setenv("GLM_API_KEY", "fake-glm")
	if _, _, err := daemonEnvLines(nil); err == nil || strings.Contains(err.Error(), "fake-") {
		t.Fatalf("want a mismatch error without values, got %v", err)
	}
}

// Without cred helpers the old snapshot behaviour stays.
func TestDaemonEnvLinesWithoutKeyring(t *testing.T) {
	fakeCredHelpers(t)
	t.Setenv("EIGEN_CRED_GET", "off")
	t.Setenv("EIGEN_CRED_SET", "off")
	t.Setenv("GLM_API_KEY", "fake-glm")
	lines, _, err := daemonEnvLines(nil)
	if err != nil || strings.Join(lines, "\n") != "GLM_API_KEY=fake-glm" {
		t.Fatalf("lines %v err %v", lines, err)
	}
}

// A lookup that fails on a locked keyring must not overwrite the stored key.
func TestDaemonEnvLinesNeverOverwritesAnUnreadableKey(t *testing.T) {
	kr := fakeCredHelpers(t)
	_ = os.WriteFile(filepath.Join(kr, "glm-api-key"), []byte("fake-stored"), 0o600)
	_ = os.WriteFile(filepath.Join(kr, ".locked"), nil, 0o600)
	t.Setenv("GLM_API_KEY", "fake-glm")
	if _, _, err := daemonEnvLines(nil); err == nil || strings.Contains(err.Error(), "fake-") {
		t.Fatalf("want an error without values, got %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(kr, "glm-api-key")); string(b) != "fake-stored" {
		t.Fatal("the stored key was overwritten")
	}
}

// A key that came from a cred:NAME reference stays a reference even without cred-set.
func TestDaemonEnvLinesKeepsResolvedReferenceWithoutCredSet(t *testing.T) {
	kr := fakeCredHelpers(t)
	_ = os.WriteFile(filepath.Join(kr, "tc-test-fake-ref"), []byte("fake-ref-value"), 0o600)
	t.Setenv("GLM_API_KEY", "cred:tc-test-fake-ref")
	if errs := cred.ResolveEnv(); len(errs) != 0 {
		t.Fatal(errs)
	}
	t.Setenv("EIGEN_CRED_SET", "off")
	lines, _, err := daemonEnvLines(nil)
	if err != nil || strings.Join(lines, "\n") != "GLM_API_KEY=cred:tc-test-fake-ref" {
		t.Fatalf("lines %v err %v", lines, err)
	}
}

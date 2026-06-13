package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resetInstance clears the resolved instance for a clean test.
func resetInstance() {
	instanceMu.Lock()
	instance, instanceSet = "", false
	instanceMu.Unlock()
}

func TestDefaultInstanceIsExactHistoricalPaths(t *testing.T) {
	t.Setenv("EIGEN_INSTANCE", "")
	resetInstance()
	SetInstance("")
	home, _ := os.UserHomeDir()
	cases := map[string]string{
		SocketPath():  filepath.Join(home, ".eigen", "daemon.sock"),
		PIDPath():     filepath.Join(home, ".eigen", "daemon.pid"),
		SessionsDir(): filepath.Join(home, ".eigen", "daemon", "sessions"),
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("default path = %q, want exact historical %q", got, want)
		}
	}
	if !IsDefaultInstance() {
		t.Error("empty instance should be the default")
	}
}

func TestNamedInstanceSuffixesPaths(t *testing.T) {
	resetInstance()
	if !SetInstance("dev") {
		t.Fatal("dev should be a valid instance")
	}
	home, _ := os.UserHomeDir()
	if SocketPath() != filepath.Join(home, ".eigen", "daemon-dev.sock") {
		t.Errorf("socket = %q", SocketPath())
	}
	if PIDPath() != filepath.Join(home, ".eigen", "daemon-dev.pid") {
		t.Errorf("pid = %q", PIDPath())
	}
	if SessionsDir() != filepath.Join(home, ".eigen", "daemon-dev", "sessions") {
		t.Errorf("sessions = %q", SessionsDir())
	}
	if IsDefaultInstance() {
		t.Error("dev is not the default instance")
	}
	resetInstance()
}

func TestInstanceValidation(t *testing.T) {
	good := []string{"", "dev", "dev2", "my-instance", "a.b_c", strings.Repeat("x", 32)}
	for _, n := range good {
		if !ValidInstanceName(n) {
			t.Errorf("%q should be valid", n)
		}
	}
	bad := []string{"../foo", "dev/other", "/abs", "a b", strings.Repeat("x", 33), ".hidden", "-lead"}
	for _, n := range bad {
		if ValidInstanceName(n) {
			t.Errorf("%q should be INVALID", n)
		}
	}
}

func TestSetInstanceRejectsInvalid(t *testing.T) {
	resetInstance()
	if SetInstance("../escape") {
		t.Fatal("invalid instance must be rejected (fail closed)")
	}
}

func TestResolveInstancePrecedence(t *testing.T) {
	t.Setenv("EIGEN_INSTANCE", "envval")
	// Flag wins over env.
	if n, ok := ResolveInstance("flagval"); !ok || n != "flagval" {
		t.Fatalf("flag should win: %q %v", n, ok)
	}
	// Empty flag falls back to env.
	if n, ok := ResolveInstance(""); !ok || n != "envval" {
		t.Fatalf("env fallback: %q %v", n, ok)
	}
	// Invalid env fails closed.
	t.Setenv("EIGEN_INSTANCE", "bad/name")
	if _, ok := ResolveInstance(""); ok {
		t.Fatal("invalid env should fail closed")
	}
}

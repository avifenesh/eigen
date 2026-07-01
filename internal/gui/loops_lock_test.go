package gui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestAcquireLoopOwnership verifies the non-blocking flock semantics: the first
// acquire succeeds, a second concurrent acquire (same OR different process) fails
// until the first releases.
func TestAcquireLoopOwnership(t *testing.T) {
	// Clean slate: remove any stale lock from a crashed test run.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	lockPath := filepath.Join(home, ".eigen", "gui-loops.lock")
	_ = os.Remove(lockPath)

	// First acquire should succeed.
	release1, acquired1 := AcquireLoopOwnership()
	if !acquired1 {
		t.Fatal("first AcquireLoopOwnership failed (expected success)")
	}
	defer release1()

	// Second acquire IN-PROCESS should fail (lock already held).
	// NOTE: On some platforms, flock allows the same process to double-lock the
	// same file descriptor. To get honest non-blocking semantics, we must open
	// a SECOND file descriptor (os.OpenFile again) — which is exactly what a
	// second Bridge.Start() call would do. The implementation already does this
	// (each call to AcquireLoopOwnership opens its own file), so this in-process
	// test is valid.
	release2, acquired2 := AcquireLoopOwnership()
	defer release2() // defensive; should be a no-op since acquired2 is false
	if acquired2 {
		t.Fatal("second in-process AcquireLoopOwnership succeeded (expected failure — lock held)")
	}

	// Verify a SUBPROCESS also cannot acquire while the lock is held. Spawn a
	// helper that tries to acquire, writes "ok" or "fail" to stdout, then exits.
	// This tests the real coexistence scenario: two separate processes (Wails GUI
	// and guiserver) both calling AcquireLoopOwnership.
	cmd := exec.Command(os.Args[0], "-test.run=TestAcquireLoopOwnershipHelper")
	cmd.Env = append(os.Environ(), "EIGEN_TEST_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess helper failed: %v\noutput: %s", err, out)
	}
	// The helper should write "locked" (meaning it correctly FAILED to acquire).
	if got := string(out); got != "locked\n" {
		t.Errorf("subprocess acquired lock while parent holds it (expected 'locked', got %q)", got)
	}

	// Release the first lock.
	release1()

	// Now a third acquire should succeed (lock is free).
	release3, acquired3 := AcquireLoopOwnership()
	if !acquired3 {
		t.Fatal("third AcquireLoopOwnership failed after release (expected success)")
	}
	release3()
}

// TestAcquireLoopOwnershipHelper is the subprocess helper for the cross-process
// flock test. It tries to acquire the lock and writes "acquired" or "locked" to
// stdout. DO NOT run this test directly — it is invoked by the parent test via
// `go test -test.run=TestAcquireLoopOwnershipHelper`.
func TestAcquireLoopOwnershipHelper(t *testing.T) {
	if os.Getenv("EIGEN_TEST_HELPER") != "1" {
		t.Skip("not running as subprocess helper")
	}
	// Give the parent a moment to fully acquire (defensive against scheduler races).
	time.Sleep(50 * time.Millisecond)

	release, acquired := AcquireLoopOwnership()
	defer release()
	if acquired {
		// BAD: we acquired the lock even though the parent holds it.
		os.Stdout.WriteString("acquired\n")
	} else {
		// GOOD: lock is held by the parent, we correctly failed to acquire.
		os.Stdout.WriteString("locked\n")
	}
	os.Exit(0)
}

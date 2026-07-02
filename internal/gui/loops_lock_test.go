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
// until the first releases. Runs in an isolated temp HOME to avoid disrupting a
// live coexistence lock on the dev machine.
func TestAcquireLoopOwnership(t *testing.T) {
	// Isolate: set HOME to a temp dir so the test never touches ~/.eigen/gui-loops.lock.
	// This prevents the test from removing a LIVE coexistence lock that a real GUI
	// process might be holding.
	tempHome := t.TempDir()
	lockPath := filepath.Join(tempHome, ".eigen", "gui-loops.lock")

	// First acquire should succeed.
	release1, acquired1 := acquireLoopOwnershipAt(lockPath)
	if !acquired1 {
		t.Fatal("first acquireLoopOwnershipAt failed (expected success)")
	}
	defer release1()

	// Second acquire IN-PROCESS should fail (lock already held).
	// NOTE: On some platforms, flock allows the same process to double-lock the
	// same file descriptor. To get honest non-blocking semantics, we must open
	// a SECOND file descriptor (os.OpenFile again) — which is exactly what a
	// second Bridge.Start() call would do. The implementation already does this
	// (each call to acquireLoopOwnershipAt opens its own file), so this in-process
	// test is valid.
	release2, acquired2 := acquireLoopOwnershipAt(lockPath)
	defer release2() // defensive; should be a no-op since acquired2 is false
	if acquired2 {
		t.Fatal("second in-process acquireLoopOwnershipAt succeeded (expected failure — lock held)")
	}

	// Verify a SUBPROCESS also cannot acquire while the lock is held. Spawn a
	// helper that tries to acquire, writes "acquired" or "locked" to stdout, then exits.
	// This tests the real coexistence scenario: two separate processes (Wails GUI
	// and guiserver) both calling AcquireLoopOwnership. Pass the isolated lock path
	// via env so the subprocess helper uses the same temp path.
	cmd := exec.Command(os.Args[0], "-test.run=TestAcquireLoopOwnershipHelper")
	cmd.Env = append(os.Environ(), "EIGEN_TEST_HELPER=1", "EIGEN_TEST_LOCK_PATH="+lockPath)
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
	release3, acquired3 := acquireLoopOwnershipAt(lockPath)
	if !acquired3 {
		t.Fatal("third acquireLoopOwnershipAt failed after release (expected success)")
	}
	release3()
}

// TestAcquireLoopOwnershipHelper is the subprocess helper for the cross-process
// flock test. It tries to acquire the lock and writes "acquired" or "locked" to
// stdout. DO NOT run this test directly — it is invoked by the parent test via
// `go test -test.run=TestAcquireLoopOwnershipHelper`. The parent passes the
// isolated lock path via EIGEN_TEST_LOCK_PATH env var.
func TestAcquireLoopOwnershipHelper(t *testing.T) {
	if os.Getenv("EIGEN_TEST_HELPER") != "1" {
		t.Skip("not running as subprocess helper")
	}
	// Give the parent a moment to fully acquire (defensive against scheduler races).
	time.Sleep(50 * time.Millisecond)

	lockPath := os.Getenv("EIGEN_TEST_LOCK_PATH")
	if lockPath == "" {
		os.Stdout.WriteString("error: no EIGEN_TEST_LOCK_PATH\n")
		os.Exit(1)
	}

	release, acquired := acquireLoopOwnershipAt(lockPath)
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

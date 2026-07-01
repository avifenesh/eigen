package gui

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// AcquireLoopOwnership tries to acquire exclusive ownership of the background
// loops that must run in EXACTLY ONE GUI process when multiple GUIs are active
// (Wails + guiserver during migration). Returns a release function (must be
// called in Stop) and a bool indicating whether ownership was acquired.
//
// Loop classification:
//   - feedLoop: MUST be gated — suggester LLM calls spend real money
//   - gpuSampleLoop: MUST be gated — duplicate desktop notifications are bad UX
//   - healthLoop: MUST NOT be gated — each process needs its own daemon-status UI
//
// The lock is a flock on ~/.eigen/gui-loops.lock, non-blocking. Not acquiring
// the lock is NORMAL and expected — it means another GUI owns the loops. The
// non-owning GUI still serves all RPC methods and healthLoop; it just skips
// feed/GPU sampling and notifications.
//
// Implementation: O_CREATE + LOCK_EX|LOCK_NB. The lock is held until released,
// surviving process crashes (kernel releases it), so a crashed GUI doesn't
// strand loop ownership.
func AcquireLoopOwnership() (release func(), acquired bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return func() {}, false
	}
	lockPath := filepath.Join(home, ".eigen", "gui-loops.lock")
	// Ensure .eigen exists (the daemon already creates it, but this is defensive).
	_ = os.MkdirAll(filepath.Dir(lockPath), 0755)

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return func() {}, false
	}
	// Try to acquire the lock non-blocking. LOCK_NB means "return EWOULDBLOCK if
	// already locked" rather than blocking. This is the core coexistence mechanism.
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		return func() {}, false // another GUI owns the loops
	}
	// Acquired. Return a release func that unlocks + closes the file. Stop() MUST
	// call this or the lock leaks (the kernel releases it on process exit, but we
	// want explicit release so a GUI restart can immediately reacquire).
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, true
}

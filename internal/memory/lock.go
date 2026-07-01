package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockStore acquires an exclusive advisory lock on this store's .memory.lock
// file, blocking until it becomes available. Returns a release function the
// caller MUST defer. The lock guards read-modify-write operations over
// MEMORY.md and related files so two concurrent Bridges (soon-to-be GUI
// coexistence) cannot interleave their RMW spans and lose writes.
//
// Advisory (flock): processes cooperate voluntarily. The file is created under
// the store's directory (e.g. ~/.eigen/memory/eigen-3e739af1/.memory.lock) and
// held for the entire RMW span. The lock is released by closing the file
// descriptor (happens when release() runs or when the process exits).
func (s *Store) lockStore() (release func(), err error) {
	if s == nil {
		return nil, fmt.Errorf("memory unavailable")
	}
	if err := s.ensureDir(); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(s.dir, ".memory.lock")
	// Open for read/write, create if missing, but don't truncate existing lock
	// metadata (harmless; lock content is never read — the descriptor is the lock).
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	// Exclusive (LOCK_EX) advisory lock — blocks until acquired.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("flock: %w", err)
	}
	// Release unlocks and closes the descriptor. Safe to call multiple times
	// (idempotent) — the second call is a no-op once f is nil.
	return func() {
		if f != nil {
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) // unlock (redundant: close does it)
			_ = f.Close()
			f = nil
		}
	}, nil
}

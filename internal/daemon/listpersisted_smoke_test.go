package daemon

import (
	"os"
	"testing"
)

// TestListPersistedSmoke prints the real on-disk daemon sessions when run
// with EIGEN_SMOKE=1 (manual verification helper; skipped in CI runs).
func TestListPersistedSmoke(t *testing.T) {
	if os.Getenv("EIGEN_SMOKE") == "" {
		t.Skip("set EIGEN_SMOKE=1 to list real persisted sessions")
	}
	for _, p := range ListPersisted() {
		t.Logf("%-4s msgs=%-5d updated=%d %-32s %.55s", p.ID, p.Msgs, p.Updated, p.Dir, p.Title)
	}
}

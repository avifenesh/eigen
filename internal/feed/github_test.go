package feed

import (
	"context"
	"testing"
)

func TestScanGitHubCanceledSkipsCommands(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ghCommandCount.Store(0)
	items := scanGitHub(ctx)
	if len(items) != 0 {
		t.Fatalf("canceled GitHub scan returned %d items", len(items))
	}
	if got := ghCommandCount.Load(); got != 0 {
		t.Fatalf("canceled GitHub scan should not start gh commands, got %d", got)
	}
}

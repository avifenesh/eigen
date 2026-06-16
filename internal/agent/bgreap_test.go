package agent

import (
	"fmt"
	"testing"
	"time"
)

// TestBgRegistryReapsTerminalTasks: the in-memory map is bounded; terminal
// tasks beyond the cap are dropped (oldest first) but running tasks are kept,
// and reaped tasks remain readable from disk.
func TestBgRegistryReapsTerminalTasks(t *testing.T) {
	dir := t.TempDir()
	r := NewBgRegistry(dir)

	// Seed more terminal tasks than the cap.
	total := maxRetainedTasks + 50
	var firstID string
	for i := 0; i < total; i++ {
		id := r.next()
		if i == 0 {
			firstID = id
		}
		r.put(&BgTask{
			ID: id, Task: fmt.Sprintf("t%d", i), Status: "done", Result: "r",
			Started: time.Now(), Finished: time.Now().Add(time.Duration(i) * time.Millisecond),
		})
	}

	r.mu.Lock()
	n := len(r.tasks)
	r.mu.Unlock()
	if n > maxRetainedTasks {
		t.Fatalf("in-memory map should be bounded at %d, got %d", maxRetainedTasks, n)
	}
	// The oldest task was reaped from memory but is still readable from disk.
	r.mu.Lock()
	_, inMem := r.tasks[firstID]
	r.mu.Unlock()
	if inMem {
		t.Fatal("oldest terminal task should have been reaped from memory")
	}
	if got := r.Get(firstID); got == nil {
		t.Fatal("reaped task must still be readable from its jsonl on disk")
	}
}

// Running tasks are never reaped, even past the cap.
func TestBgRegistryNeverReapsRunning(t *testing.T) {
	dir := t.TempDir()
	r := NewBgRegistry(dir)
	var runningIDs []string
	for i := 0; i < 20; i++ {
		id := r.next()
		runningIDs = append(runningIDs, id)
		r.put(&BgTask{ID: id, Task: "live", Status: "running", Started: time.Now()})
	}
	// Flood with terminal tasks to force reaping.
	for i := 0; i < maxRetainedTasks+50; i++ {
		id := r.next()
		r.put(&BgTask{ID: id, Status: "done", Started: time.Now(), Finished: time.Now()})
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range runningIDs {
		if _, ok := r.tasks[id]; !ok {
			t.Fatalf("running task %s must never be reaped", id)
		}
	}
}

package app

import (
	"runtime"
	"testing"
	"time"
)

func settledGoroutines(t *testing.T) int {
	t.Helper()
	deadline := time.Now().Add(250 * time.Millisecond)
	last := -1
	stable := 0
	for time.Now().Before(deadline) {
		runtime.GC()
		n := runtime.NumGoroutine()
		if n == last {
			stable++
			if stable >= 2 {
				return n
			}
		} else {
			last = n
			stable = 0
		}
		time.Sleep(20 * time.Millisecond)
	}
	return runtime.NumGoroutine()
}

func assertGoroutineBound(t *testing.T, before, allowance int, context string) {
	t.Helper()
	after := settledGoroutines(t)
	if after > before+allowance {
		t.Fatalf("%s leaked goroutines: before=%d after=%d allowance=%d", context, before, after, allowance)
	}
}

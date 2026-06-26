package syshealth

import (
	"runtime"
	"testing"
)

// TestReadSane checks Read returns plausible values on this (Linux CI) host:
// CPU count > 0, and any populated metric within bounds. Fields a platform
// can't read stay zero — so we assert "0 or sane", never "must be non-zero".
func TestReadSane(t *testing.T) {
	h := Read()
	if h.CPUs <= 0 {
		t.Fatalf("CPUs should be > 0, got %d", h.CPUs)
	}
	if h.Load1 < 0 {
		t.Errorf("Load1 negative: %f", h.Load1)
	}
	if h.MemUsedPct < 0 || h.MemUsedPct > 100 {
		t.Errorf("MemUsedPct out of range: %f", h.MemUsedPct)
	}
	if h.DiskUsedPct < 0 || h.DiskUsedPct > 100 {
		t.Errorf("DiskUsedPct out of range: %f", h.DiskUsedPct)
	}
	if h.MemUsed > h.MemTotal {
		t.Errorf("MemUsed %d > MemTotal %d", h.MemUsed, h.MemTotal)
	}
	// On Linux, /proc should give us real memory + load numbers.
	if runtime.GOOS == "linux" {
		if h.MemTotal == 0 {
			t.Error("expected non-zero MemTotal on linux (/proc/meminfo)")
		}
		if h.LoadPerCPU < 0 {
			t.Errorf("LoadPerCPU negative: %f", h.LoadPerCPU)
		}
	}
}

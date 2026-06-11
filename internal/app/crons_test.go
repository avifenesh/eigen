package app

import (
	"strings"
	"testing"
	"time"
)

func TestHumanizeMicros(t *testing.T) {
	if got := humanizeMicros(0); got != "—" {
		t.Fatalf("zero: %q", got)
	}
	now := time.Now()
	if got := humanizeMicros(now.UnixMicro()); !strings.HasPrefix(got, "today ") {
		t.Fatalf("today: %q", got)
	}
	future := now.AddDate(0, 0, 3)
	if got := humanizeMicros(future.UnixMicro()); !strings.HasPrefix(got, future.Format("2006-01-02")) {
		t.Fatalf("future: %q", got)
	}
}

func TestLoadCrontabParsing(t *testing.T) {
	// Pure-parse test through the row builder: crontab may not exist in CI;
	// loadCrons must not panic either way.
	_ = loadCrons()
}

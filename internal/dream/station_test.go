package dream

import (
	"context"
	"strings"
	"testing"
)

func TestDistillStationEmptyDigest(t *testing.T) {
	p := &fakeProv{reply: "- should not be used"}
	notes, err := DistillStation(context.Background(), p, "   ", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Fatalf("empty digest should yield no notes, got %v", notes)
	}
}

func TestDistillStationParsesAndDedupes(t *testing.T) {
	p := &fakeProv{reply: "- standup is daily at 9am\n- valkey-glide has been drifting (54 unpushed)\n- standup is daily at 9am"}
	digest := "Upcoming calendar (7d):\n- 2026-07-01T09:00:00Z — Standup\n\nProject state:\n- valkey-glide: 54 unpushed commit(s)\n"
	notes, err := DistillStation(context.Background(), p, digest, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 {
		t.Fatalf("want 2 deduped notes, got %d: %v", len(notes), notes)
	}
	// The digest must reach the model (so it reflects real working-life signal).
	if !strings.Contains(p.gotUser, "valkey-glide") || !strings.Contains(p.gotUser, "Standup") {
		t.Errorf("digest not passed to model: %q", p.gotUser)
	}
	// A note already in global memory is dropped.
	notes2, _ := DistillStation(context.Background(), p, digest, "- standup is daily at 9am")
	for _, n := range notes2 {
		if strings.Contains(n, "standup is daily at 9am") {
			t.Errorf("existing note should be deduped out: %v", notes2)
		}
	}
}

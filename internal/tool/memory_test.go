package tool

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeMem struct {
	notes []string
	fail  bool
}

func (f *fakeMem) Append(note string) error {
	if f.fail {
		return errTest("disk full")
	}
	f.notes = append(f.notes, note)
	return nil
}

func TestMemoryToolAppends(t *testing.T) {
	fm := &fakeMem{}
	args, _ := json.Marshal(map[string]string{"note": "use make build"})
	if _, err := Memory(fm).Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if len(fm.notes) != 1 || fm.notes[0] != "use make build" {
		t.Fatalf("note not appended: %v", fm.notes)
	}
}

func TestMemoryToolPropagatesError(t *testing.T) {
	fm := &fakeMem{fail: true}
	args, _ := json.Marshal(map[string]string{"note": "x"})
	if _, err := Memory(fm).Run(context.Background(), args); err == nil {
		t.Fatal("append failure should propagate")
	}
}

func TestMemoryToolIsReadOnly(t *testing.T) {
	if !Memory(&fakeMem{}).ReadOnly {
		t.Fatal("memory tool should be read-only (writes only eigen's store)")
	}
}

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
	if _, err := Memory(fm, nil).Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if len(fm.notes) != 1 || fm.notes[0] != "use make build" {
		t.Fatalf("note not appended: %v", fm.notes)
	}
}

func TestMemoryToolPropagatesError(t *testing.T) {
	fm := &fakeMem{fail: true}
	args, _ := json.Marshal(map[string]string{"note": "x"})
	if _, err := Memory(fm, nil).Run(context.Background(), args); err == nil {
		t.Fatal("append failure should propagate")
	}
}

func TestMemoryToolIsReadOnly(t *testing.T) {
	if !Memory(&fakeMem{}, nil).ReadOnly {
		t.Fatal("memory tool should be read-only (writes only eigen's store)")
	}
}

func TestMemoryToolGlobalScope(t *testing.T) {
	proj, glob := &fakeMem{}, &fakeMem{}
	args, _ := json.Marshal(map[string]string{"note": "user commits often", "scope": "global"})
	if _, err := Memory(proj, glob).Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if len(glob.notes) != 1 || len(proj.notes) != 0 {
		t.Fatalf("global scope should write to global store: proj=%v glob=%v", proj.notes, glob.notes)
	}
}

func TestMemoryToolDefaultsToProject(t *testing.T) {
	proj, glob := &fakeMem{}, &fakeMem{}
	args, _ := json.Marshal(map[string]string{"note": "make test"})
	if _, err := Memory(proj, glob).Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if len(proj.notes) != 1 || len(glob.notes) != 0 {
		t.Fatalf("default scope should be project: proj=%v glob=%v", proj.notes, glob.notes)
	}
}

func TestMemoryToolGlobalScopeFallsBackWhenNoGlobal(t *testing.T) {
	proj := &fakeMem{}
	args, _ := json.Marshal(map[string]string{"note": "x", "scope": "global"})
	if _, err := Memory(proj, nil).Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if len(proj.notes) != 1 {
		t.Fatal("global scope with no global store should fall back to project")
	}
}

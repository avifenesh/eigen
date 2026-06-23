package gui

import (
	"encoding/json"
	"testing"

	"github.com/avifenesh/eigen/internal/memory"
)

// TestNewDreamPipelineWiring verifies the on-demand dream pipeline is wired the
// same way main.newMemoryPipeline wires the CLI's: the Store/Index are attached
// and all three model-facing callbacks (Stage1, Consolidate, Summarize) are
// non-nil. A regression that drops a callback would make MaybeConsolidate /
// RegenSummary silently no-op (they guard on a nil callback), so a button press
// would do nothing — this catches that.
func TestNewDreamPipelineWiring(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s, err := memory.Open("")
	if err != nil {
		t.Fatalf("memory.Open: %v", err)
	}
	idx, err := memory.OpenIndex()
	if err != nil {
		t.Fatalf("memory.OpenIndex: %v", err)
	}
	defer idx.Close()

	// A nil provider is fine here: we only assert wiring, never invoke the model.
	pipe := newDreamPipeline(nil, s, idx)
	if pipe == nil {
		t.Fatal("newDreamPipeline returned nil")
	}
	if pipe.Store != s {
		t.Error("pipeline Store not wired to the scope store")
	}
	if pipe.Index != idx {
		t.Error("pipeline Index not wired to the memory index")
	}
	if pipe.Stage1 == nil {
		t.Error("Stage1 callback is nil — Stage1Sessions would skip every session")
	}
	if pipe.Consolidate == nil {
		t.Error("Consolidate callback is nil — MaybeConsolidate would no-op")
	}
	if pipe.Summarize == nil {
		t.Error("Summarize callback is nil — RegenSummary would no-op")
	}
}

// TestDreamReportDTOJSON pins the wire shape the (follow-up) frontend button
// reads back: camelCase keys, no surprises. The bindings regen depends on these
// tags, so a rename here is a breaking change worth a failing test.
func TestDreamReportDTOJSON(t *testing.T) {
	b, err := json.Marshal(DreamReportDTO{
		Scope:          "project",
		Report:         "consolidated MEMORY.md",
		Consolidated:   true,
		SummaryRegened: false,
		Changed:        true,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"scope", "report", "consolidated", "summaryRegened", "changed"} {
		if _, ok := m[key]; !ok {
			t.Errorf("DreamReportDTO JSON missing key %q (got %s)", key, b)
		}
	}
}

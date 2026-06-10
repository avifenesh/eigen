package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
)

func TestRunnerFiresMatchingHook(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "fired")
	// A hook that writes its stdin payload to a file.
	script := filepath.Join(dir, "h.sh")
	os.WriteFile(script, []byte("#!/bin/sh\ncat > "+marker+"\n"), 0o755)

	r := New([]Spec{{Event: OnToolStart, Command: []string{script}}})
	if r == nil {
		t.Fatal("runner should exist")
	}
	r.Fire(Payload{Event: OnToolStart, Tool: "bash", Session: "s1"})

	// Wait for the async hook.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if data, err := os.ReadFile(marker); err == nil {
			var p Payload
			if json.Unmarshal(data, &p) == nil && p.Tool == "bash" && p.Event == OnToolStart {
				return // success
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("hook did not fire with the right payload")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRunnerIgnoresUnregistered(t *testing.T) {
	r := New([]Spec{{Event: OnNote, Command: []string{"true"}}})
	// Firing a different event must be a no-op (no panic, no hang).
	r.Fire(Payload{Event: OnToolResult})
}

func TestNewSkipsMalformed(t *testing.T) {
	r := New([]Spec{
		{Event: "", Command: []string{"x"}}, // no event
		{Event: OnNote, Command: nil},       // no command
	})
	if r != nil {
		t.Fatal("all-malformed specs should yield a nil runner")
	}
}

func TestNilRunnerNoop(t *testing.T) {
	var r *Runner
	r.Fire(Payload{Event: OnToolStart}) // must not panic
	next := 0
	sink := r.Wrap(func(agent.Event) { next++ }, "s")
	sink(agent.Event{Kind: agent.EventToolStart})
	if next != 1 {
		t.Fatal("nil runner Wrap should forward unchanged")
	}
}

func TestWrapFiresOnEvents(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "n")
	script := filepath.Join(dir, "h.sh")
	os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755)
	r := New([]Spec{{Event: OnTurnDone, Command: []string{script}}})

	var forwarded int
	sink := r.Wrap(func(agent.Event) { forwarded++ }, "s")
	sink(agent.Event{Kind: agent.EventDone})
	if forwarded != 1 {
		t.Fatal("event should still forward")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("turn_done hook should fire on EventDone")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	// array form
	p1 := filepath.Join(dir, "a.json")
	os.WriteFile(p1, []byte(`[{"event":"note","command":["echo","hi"]}]`), 0o644)
	if r, err := Load(p1); err != nil || r == nil {
		t.Fatalf("array load: %v %v", r, err)
	}
	// object form
	p2 := filepath.Join(dir, "b.json")
	os.WriteFile(p2, []byte(`{"hooks":[{"event":"tool_start","command":["x"]}]}`), 0o644)
	if r, err := Load(p2); err != nil || r == nil {
		t.Fatalf("object load: %v %v", r, err)
	}
	// missing file → nil, nil
	if r, err := Load(filepath.Join(dir, "nope.json")); err != nil || r != nil {
		t.Fatalf("missing: %v %v", r, err)
	}
	// malformed → error
	p3 := filepath.Join(dir, "c.json")
	os.WriteFile(p3, []byte(`not json`), 0o644)
	if _, err := Load(p3); err == nil {
		t.Fatal("malformed config should error")
	}
}

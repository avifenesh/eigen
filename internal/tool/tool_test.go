package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegistrySkipsDisabledTools(t *testing.T) {
	disabled := Definition{Name: "off", Disabled: true}
	enabled := Definition{Name: "on", Run: func(context.Context, json.RawMessage) (string, error) { return "ok", nil }}
	reg, err := NewRegistry(disabled, enabled)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("off"); ok {
		t.Fatal("disabled tool should be omitted")
	}
	if _, ok := reg.Get("on"); !ok {
		t.Fatal("enabled tool should be registered")
	}
}

func TestRegistrySubsetAndReadOnly(t *testing.T) {
	ro := Definition{Name: "read", ReadOnly: true, Parameters: json.RawMessage(`{"type":"object"}`),
		Run: func(context.Context, json.RawMessage) (string, error) { return "", nil }}
	mut := Definition{Name: "write", ReadOnly: false, Parameters: json.RawMessage(`{"type":"object"}`),
		Run: func(context.Context, json.RawMessage) (string, error) { return "", nil }}
	reg, err := NewRegistry(ro, mut)
	if err != nil {
		t.Fatal(err)
	}
	sub := reg.Subset("read", "nonexistent")
	if _, ok := sub.Get("read"); !ok {
		t.Fatal("subset should contain read")
	}
	if _, ok := sub.Get("write"); ok {
		t.Fatal("subset should not contain write")
	}
	if !sub.AllReadOnly() {
		t.Fatal("read-only subset should report AllReadOnly")
	}
	// Parent untouched.
	if _, ok := reg.Get("write"); !ok {
		t.Fatal("Subset must not mutate the parent registry")
	}
	if reg.Subset("read", "write").AllReadOnly() {
		t.Fatal("a subset containing write must not be AllReadOnly")
	}
}

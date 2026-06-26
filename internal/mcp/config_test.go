package mcp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigCRUD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")

	// Empty/missing file → no servers, no error.
	if list, err := ListServers(path); err != nil || len(list) != 0 {
		t.Fatalf("empty list: %v %v", list, err)
	}

	// Add a stdio server.
	if err := SaveServer(path, ServerEntry{
		Name:        "local",
		Command:     []string{"node", "srv.js"},
		Description: "a local server",
	}); err != nil {
		t.Fatal(err)
	}
	// Add a remote connector.
	if err := SaveServer(path, ServerEntry{
		Name:        "notion",
		URL:         "https://mcp.notion.com/mcp",
		Type:        "http",
		Description: "Notion connector",
	}); err != nil {
		t.Fatal(err)
	}

	list, err := ListServers(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 servers, got %d", len(list))
	}
	// Sorted by name: local, notion.
	if list[0].Name != "local" || list[1].Name != "notion" {
		t.Fatalf("order/name wrong: %+v", list)
	}
	if list[0].Remote || !list[1].Remote {
		t.Fatalf("remote flags wrong: local.Remote=%v notion.Remote=%v", list[0].Remote, list[1].Remote)
	}

	// Replace by name (case-insensitive) — edit notion's description.
	if err := SaveServer(path, ServerEntry{Name: "NOTION", URL: "https://mcp.notion.com/mcp", Type: "http", Description: "edited"}); err != nil {
		t.Fatal(err)
	}
	list, _ = ListServers(path)
	if len(list) != 2 {
		t.Fatalf("replace should not add a duplicate, got %d", len(list))
	}
	for _, s := range list {
		if s.Name == "NOTION" && s.Description != "edited" {
			t.Errorf("description not updated: %q", s.Description)
		}
	}

	// Disable.
	if ok, err := SetServerDisabled(path, "local", true); err != nil || !ok {
		t.Fatalf("disable: %v %v", ok, err)
	}
	list, _ = ListServers(path)
	for _, s := range list {
		if s.Name == "local" && !s.Disabled {
			t.Error("local should be disabled")
		}
	}

	// Remove.
	if ok, err := RemoveServer(path, "local"); err != nil || !ok {
		t.Fatalf("remove: %v %v", ok, err)
	}
	if ok, _ := RemoveServer(path, "nonexistent"); ok {
		t.Error("removing a missing server should return false")
	}
	list, _ = ListServers(path)
	if len(list) != 1 || !strings.EqualFold(list[0].Name, "notion") {
		t.Fatalf("after remove, want only notion: %+v", list)
	}
}

func TestValidateEntry(t *testing.T) {
	cases := []struct {
		name string
		e    ServerEntry
		ok   bool
	}{
		{"stdio ok", ServerEntry{Name: "a", Command: []string{"x"}}, true},
		{"remote ok", ServerEntry{Name: "a", URL: "https://x"}, true},
		{"no name", ServerEntry{Command: []string{"x"}}, false},
		{"both url and command", ServerEntry{Name: "a", URL: "https://x", Command: []string{"x"}}, false},
		{"neither", ServerEntry{Name: "a"}, false},
	}
	for _, c := range cases {
		err := validateEntry(c.e)
		if (err == nil) != c.ok {
			t.Errorf("%s: validateEntry ok=%v, err=%v", c.name, c.ok, err)
		}
	}
}

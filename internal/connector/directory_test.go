package connector

import "testing"

func TestDirectory(t *testing.T) {
	dir := Directory()
	if len(dir) == 0 {
		t.Fatal("catalog should not be empty")
	}
	// Every entry needs a name, display, and a URL — those drive the one-click add.
	seen := map[string]bool{}
	for _, e := range dir {
		if e.Name == "" || e.Display == "" || e.URL == "" {
			t.Errorf("incomplete catalog entry: %+v", e)
		}
		if seen[e.Name] {
			t.Errorf("duplicate catalog name %q", e.Name)
		}
		seen[e.Name] = true
	}
	// Returned slice is a copy — mutating it must not affect the package catalog.
	dir[0].Name = "MUTATED"
	if Directory()[0].Name == "MUTATED" {
		t.Error("Directory() must return a copy, not the backing slice")
	}
}

func TestCatalogByName(t *testing.T) {
	if _, ok := CatalogByName("notion"); !ok {
		t.Error("notion should be in the catalog")
	}
	// Case-insensitive.
	if _, ok := CatalogByName("NOTION"); !ok {
		t.Error("lookup should be case-insensitive")
	}
	if _, ok := CatalogByName("definitely-not-a-connector"); ok {
		t.Error("unknown name should not resolve")
	}
}

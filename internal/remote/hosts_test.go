package remote

import (
	"path/filepath"
	"testing"
)

func TestHostsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts.json")

	// Missing file → empty, no error.
	h, err := loadHostsFrom(path)
	if err != nil || len(h) != 0 {
		t.Fatalf("missing file: %v %v", h, err)
	}

	h["work"] = Host{SSH: "me@box", Dir: "/srv/app", Model: "openai.gpt-5.5", Perm: "auto"}
	if err := saveHostsTo(path, h); err != nil {
		t.Fatal(err)
	}
	got, err := loadHostsFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got["work"].SSH != "me@box" || got["work"].Dir != "/srv/app" || got["work"].Model != "openai.gpt-5.5" {
		t.Fatalf("round-trip mismatch: %+v", got["work"])
	}
}

func TestHostsResolve(t *testing.T) {
	h := Hosts{
		"work": {SSH: "me@box", Dir: "/srv/app", Model: "m1", Perm: "auto"},
	}
	// Saved name → uses saved defaults.
	s, model, perm, err := h.Resolve("work")
	if err != nil || s.Host != "box" || s.User != "me" || s.Dir != "/srv/app" || model != "m1" || perm != "auto" {
		t.Fatalf("resolve saved: %+v m=%q p=%q err=%v", s, model, perm, err)
	}
	// Literal spec → no saved defaults.
	s2, model2, _, err := h.Resolve("other@host2:/x")
	if err != nil || s2.Host != "host2" || s2.User != "other" || s2.Dir != "/x" || model2 != "" {
		t.Fatalf("resolve literal: %+v m=%q err=%v", s2, model2, err)
	}
	// Inline :dir on a literal overrides nothing saved (no entry).
	if _, _, _, err := h.Resolve("plainhost"); err != nil {
		t.Fatalf("resolve plain: %v", err)
	}
}

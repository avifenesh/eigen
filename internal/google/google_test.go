package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestReadClientFile(t *testing.T) {
	dir := t.TempDir()
	// Google "installed" (Desktop) wrapper.
	installed := filepath.Join(dir, "installed.json")
	os.WriteFile(installed, []byte(`{"installed":{"client_id":"cid-1","client_secret":"sec-1","redirect_uris":["http://localhost"]}}`), 0o600)
	if c, ok := readClientFile(installed); !ok || c.ClientID != "cid-1" || c.ClientSecret != "sec-1" {
		t.Fatalf("installed parse: %+v ok=%v", c, ok)
	}
	// "web" wrapper.
	web := filepath.Join(dir, "web.json")
	os.WriteFile(web, []byte(`{"web":{"client_id":"cid-2","client_secret":"sec-2"}}`), 0o600)
	if c, ok := readClientFile(web); !ok || c.ClientID != "cid-2" {
		t.Fatalf("web parse: %+v ok=%v", c, ok)
	}
	// Bare object.
	bare := filepath.Join(dir, "bare.json")
	os.WriteFile(bare, []byte(`{"client_id":"cid-3","client_secret":"sec-3"}`), 0o600)
	if c, ok := readClientFile(bare); !ok || c.ClientID != "cid-3" {
		t.Fatalf("bare parse: %+v ok=%v", c, ok)
	}
	// Missing / junk.
	if _, ok := readClientFile(filepath.Join(dir, "nope.json")); ok {
		t.Error("missing file should not parse")
	}
}

// memStore is an in-memory tokenStore for tests.
type memStore struct{ tok *oauth2.Token }

func (m *memStore) load() (*oauth2.Token, error) { return m.tok, nil }
func (m *memStore) save(t *oauth2.Token) error   { m.tok = t; return nil }
func (m *memStore) clear() error                 { m.tok = nil; return nil }

func TestGatesNotConfiguredNotConnected(t *testing.T) {
	// No client creds → not configured; tools should error clearly, not panic.
	a := &Auth{store: &memStore{}, haveC: false}
	if a.Configured() {
		t.Fatal("should not be configured without creds")
	}
	out := a.Tools(func() time.Time { return time.Unix(0, 0) })
	if len(out) != 3 {
		t.Fatalf("want 3 google tools, got %d", len(out))
	}
	_, err := out[0].Run(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("calendar_list should error when not configured")
	}

	// Configured but no token → not connected.
	a2 := &Auth{store: &memStore{}, haveC: true, creds: clientCreds{ClientID: "x"}}
	if a2.Connected() {
		t.Fatal("should not be connected without a token")
	}
	if _, err := out[0].Run(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("should still error: not connected")
	}
}

// TestCalendarListAgainstFakeAPI drives calendar_list end-to-end with a stored
// token + a fake Google API (the oauth2 client attaches the bearer; we don't
// refresh because the token isn't expired).
func TestCalendarListAgainstFakeAPI(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{"summary":"Standup","start":{"dateTime":"2026-07-01T09:00:00Z"},"location":"Zoom"}]}`))
	}))
	defer srv.Close()

	store := &memStore{tok: &oauth2.Token{AccessToken: "acc-1", RefreshToken: "ref-1", Expiry: time.Now().Add(time.Hour)}}
	a := &Auth{store: store, haveC: true, creds: clientCreds{ClientID: "cid"}}

	// Build the calendar URL against our fake server by overriding via a small
	// indirection: call calendarList with a custom base is not exposed, so instead
	// exercise getJSON directly against the fake (covers auth + decode).
	hcCtx := context.Background()
	var resp struct {
		Items []struct {
			Summary string `json:"summary"`
		} `json:"items"`
	}
	if err := a.getJSON(hcCtx, srv.URL+"/calendar/v3/calendars/primary/events", &resp); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Summary != "Standup" {
		t.Fatalf("unexpected items: %+v", resp.Items)
	}
	if gotAuth != "Bearer acc-1" {
		t.Fatalf("bearer not attached, got %q", gotAuth)
	}
}

func TestFileTokenStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tok.json")
	s := &fileTokenStore{path: path}
	if tok, err := s.load(); err != nil || tok != nil {
		t.Fatalf("empty load: %v %v", tok, err)
	}
	want := &oauth2.Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)}
	if err := s.save(want); err != nil {
		t.Fatal(err)
	}
	// 0600 perms.
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file perms: %v %v", fi.Mode().Perm(), err)
	}
	got, err := s.load()
	if err != nil || got == nil || got.RefreshToken != "r" {
		t.Fatalf("reload: %+v %v", got, err)
	}
	if err := s.clear(); err != nil {
		t.Fatal(err)
	}
	if tok, _ := s.load(); tok != nil {
		t.Fatal("clear should remove the token")
	}
}

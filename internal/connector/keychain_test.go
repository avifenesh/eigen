package connector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

// TestKeychainStoreRoundTrip proves the keychain store keeps the token OUT of
// the on-disk metadata file and round-trips it through the OS keyring. Uses
// go-keyring's in-memory mock so it runs anywhere (no real Keychain/libsecret).
func TestKeychainStoreRoundTrip(t *testing.T) {
	keyring.MockInit() // swap in the in-memory keyring backend for this test
	dir := t.TempDir()
	metaPath := filepath.Join(dir, "connectors.json")
	ks := &keychainStore{meta: newFileStore(metaPath)}

	recs := map[string]record{
		"notion": {
			Name:      "notion",
			ServerURL: "https://mcp.notion.com/mcp",
			AuthURL:   "https://auth/authorize",
			TokenURL:  "https://auth/token",
			ClientID:  "client-1",
			Scopes:    []string{"read"},
			Token: &oauth2.Token{
				AccessToken:  "secret-access",
				RefreshToken: "secret-refresh",
				Expiry:       time.Now().Add(time.Hour),
			},
		},
	}
	if err := ks.save(recs); err != nil {
		t.Fatal(err)
	}

	// The plaintext metadata file must NOT contain the secret token material.
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-access") || strings.Contains(string(raw), "secret-refresh") {
		t.Fatalf("token leaked into plaintext metadata file:\n%s", raw)
	}
	// But the non-secret metadata IS there.
	if !strings.Contains(string(raw), "client-1") {
		t.Errorf("metadata file should keep the client_id:\n%s", raw)
	}

	// Loading re-attaches the token from the keyring.
	got, err := ks.load()
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := got["notion"]
	if !ok || rec.Token == nil {
		t.Fatalf("token not restored from keychain: %+v", got)
	}
	if rec.Token.AccessToken != "secret-access" || rec.Token.RefreshToken != "secret-refresh" {
		t.Fatalf("restored token mismatch: %+v", rec.Token)
	}

	// Removing a connector deletes its keychain entry.
	if err := ks.save(map[string]record{}); err != nil {
		t.Fatal(err)
	}
	if _, err := keyring.Get(keychainService, "notion"); err == nil {
		t.Error("keychain entry should be deleted when the connector is removed")
	}
}

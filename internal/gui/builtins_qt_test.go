package gui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/avifenesh/eigen/internal/google"
)

func TestSetObsidianVaultAcceptsQtSelectedPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	vault := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(filepath.Join(vault, ".obsidian"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := (&Bridge{}).SetObsidianVault(vault)
	if err != nil {
		t.Fatalf("SetObsidianVault: %v", err)
	}
	if got != vault {
		t.Fatalf("vault = %q, want %q", got, vault)
	}
}

func TestImportGoogleClientFromPathAcceptsQtSelectedFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	clientPath := filepath.Join(t.TempDir(), "client.json")
	clientJSON := `{"installed":{"client_id":"qt-client","client_secret":"secret","redirect_uris":["http://localhost"]}}`
	if err := os.WriteFile(clientPath, []byte(clientJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	imported, err := (&Bridge{}).ImportGoogleClientFromPath(clientPath)
	if err != nil {
		t.Fatalf("ImportGoogleClientFromPath: %v", err)
	}
	if !imported {
		t.Fatal("ImportGoogleClientFromPath returned false")
	}
	data, err := os.ReadFile(google.ClientPath())
	if err != nil {
		t.Fatalf("read imported client: %v", err)
	}
	if string(data) != clientJSON {
		t.Fatalf("imported client = %q, want %q", data, clientJSON)
	}
}

package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestSecretEnvKeychain proves a stdio server's secret env value is stored in
// the keychain (not the plaintext mcp.json), recorded by key name, and merged
// back into the server env at connect time.
func TestSecretEnvKeychain(t *testing.T) {
	keyring.MockInit()
	path := filepath.Join(t.TempDir(), "mcp.json")

	err := SaveServer(path, ServerEntry{
		Name:      "github",
		Command:   []string{"docker", "run", "ghcr.io/github/github-mcp-server"},
		SecretEnv: map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_supersecret"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The plaintext file must NOT contain the secret value, but MUST record the key.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "ghp_supersecret") {
		t.Fatalf("secret value leaked into mcp.json:\n%s", raw)
	}
	if !strings.Contains(string(raw), "GITHUB_PERSONAL_ACCESS_TOKEN") || !strings.Contains(string(raw), "secret_env_keys") {
		t.Fatalf("secret key name should be recorded:\n%s", raw)
	}

	// ListServers reports the key name, never the value.
	list, _ := ListServers(path)
	if len(list) != 1 || len(list[0].SecretEnvKeys) != 1 || list[0].SecretEnvKeys[0] != "GITHUB_PERSONAL_ACCESS_TOKEN" {
		t.Fatalf("secret key not surfaced: %+v", list)
	}
	if list[0].SecretEnv != nil {
		t.Error("read-back must not carry secret values")
	}

	// At connect time the value is merged into the env from the keychain.
	var sc serverConfig
	for _, s := range mustConfig(t, path).Servers {
		if s.Name == "github" {
			sc = s
		}
	}
	env := serverEnvWithSecrets(sc)
	found := false
	for _, kv := range env {
		if kv == "GITHUB_PERSONAL_ACCESS_TOKEN=ghp_supersecret" {
			found = true
		}
	}
	if !found {
		t.Fatal("secret env value not merged from keychain at connect time")
	}

	// Editing a NON-secret field without re-supplying the secret keeps it stored.
	if err := SaveServer(path, ServerEntry{
		Name:          "github",
		Command:       []string{"docker", "run", "ghcr.io/github/github-mcp-server"},
		Description:   "edited",
		SecretEnvKeys: []string{"GITHUB_PERSONAL_ACCESS_TOKEN"},
	}); err != nil {
		t.Fatal(err)
	}
	if serverSecrets("github")["GITHUB_PERSONAL_ACCESS_TOKEN"] != "ghp_supersecret" {
		t.Fatal("secret should survive an edit that doesn't re-supply it")
	}

	// Removing the server deletes the keychain secret.
	if _, err := RemoveServer(path, "github"); err != nil {
		t.Fatal(err)
	}
	if serverSecrets("github") != nil {
		t.Error("secrets should be deleted with the server")
	}
}

func mustConfig(t *testing.T, path string) mcpConfig {
	t.Helper()
	cfg, err := readConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

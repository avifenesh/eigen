package mcp

import (
	"encoding/json"

	"github.com/zalando/go-keyring"
)

// MCP env-secret store. A stdio MCP server often needs an API key in its `env`
// (e.g. GITHUB_PERSONAL_ACCESS_TOKEN). Storing those in plaintext mcp.json is
// the weak link (the same gap Claude Desktop's manual-config path has). Instead
// we keep secret env values in the OS keychain, keyed by server name; mcp.json
// records only the secret KEY NAMES (secret_env_keys), never the values. At
// connect time the loader merges the keychain values into the server's env.
//
// The mcp package owns this (not internal/connector) because connector imports
// mcp — a shared keychain helper there would cycle. Service name is distinct
// from the connector token service.
const secretService = "eigen-mcp-env"

// SecretsAvailable reports whether the OS keyring is usable for storing MCP env
// secrets. The GUI gates the "mark secret" affordance on this — without a
// keyring we keep values as plaintext env rather than give a false sense of
// security.
func SecretsAvailable() bool {
	const probe = "__eigen_mcp_probe__"
	if err := keyring.Set(secretService, probe, "1"); err != nil {
		return false
	}
	_, _ = keyring.Get(secretService, probe)
	_ = keyring.Delete(secretService, probe)
	return true
}

// serverSecrets returns the keychain-stored secret env for a server (empty on
// miss / no keyring / decode error — secrets are best-effort at load: a missing
// keyring means the server simply runs without them).
func serverSecrets(server string) map[string]string {
	raw, err := keyring.Get(secretService, server)
	if err != nil || raw == "" {
		return nil
	}
	var m map[string]string
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	return m
}

// setServerSecrets stores (or, with an empty map, deletes) a server's secret env
// in the keychain. Returns an error only when the keyring write itself fails.
func setServerSecrets(server string, secrets map[string]string) error {
	if len(secrets) == 0 {
		err := keyring.Delete(secretService, server)
		if err == keyring.ErrNotFound {
			return nil
		}
		return err
	}
	raw, err := json.Marshal(secrets)
	if err != nil {
		return err
	}
	return keyring.Set(secretService, server, string(raw))
}

// deleteServerSecrets removes a server's secrets (used on server removal).
func deleteServerSecrets(server string) {
	if err := keyring.Delete(secretService, server); err != nil && err != keyring.ErrNotFound {
		// best-effort; a stale entry is harmless (orphaned key, no value leak)
		_ = err
	}
}

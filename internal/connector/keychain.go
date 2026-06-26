package connector

import (
	"encoding/json"
	"errors"

	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

// keychainService is the OS-keychain service name under which connector tokens
// are stored (one keychain entry per connector, keyed by connector name).
const keychainService = "eigen-connectors"

// keychainStore keeps the SECRET half of each record — the OAuth token (access +
// refresh) — in the OS keychain (macOS Keychain, libsecret/GNOME Keyring,
// Windows Credential Manager via go-keyring), and the non-secret metadata
// (endpoints, client_id, scopes) in a JSON file alongside. This matches the
// Claude/Codex model: credentials never sit in plaintext on disk.
//
// If the platform has no usable keyring (ErrUnsupportedPlatform / a libsecret
// daemon that isn't running), newSecretStore falls back to the plain fileStore
// so connectors still work (with the documented plaintext tradeoff) rather than
// failing outright.
type keychainStore struct {
	meta *fileStore // non-secret metadata (token field left nil on disk)
}

// keychainAvailable probes the OS keyring with a round-trip on a throwaway key.
// A nil error (or ErrNotFound on the Get) means the keyring works; an
// ErrUnsupportedPlatform or a backend error means fall back to the file store.
func keychainAvailable() bool {
	const probeUser = "__eigen_probe__"
	if err := keyring.Set(keychainService, probeUser, "1"); err != nil {
		return false
	}
	_, _ = keyring.Get(keychainService, probeUser)
	_ = keyring.Delete(keychainService, probeUser)
	return true
}

// newSecretStore returns a keychain-backed store when the OS keyring is usable,
// else the plain file store. metaPath is the JSON file for non-secret metadata
// (and the full fallback store).
func newSecretStore(metaPath string) secretStore {
	if keychainAvailable() {
		return &keychainStore{meta: newFileStore(metaPath)}
	}
	return newFileStore(metaPath)
}

func (k *keychainStore) load() (map[string]record, error) {
	recs, err := k.meta.load()
	if err != nil {
		return nil, err
	}
	// Re-attach each record's token from the keychain.
	for name, rec := range recs {
		raw, err := keyring.Get(keychainService, name)
		if errors.Is(err, keyring.ErrNotFound) || raw == "" {
			rec.Token = nil
			recs[name] = rec
			continue
		}
		if err != nil {
			// Keyring read failed mid-session: treat as no token (the connector
			// shows disconnected) rather than erroring the whole load.
			rec.Token = nil
			recs[name] = rec
			continue
		}
		var tok oauth2.Token
		if json.Unmarshal([]byte(raw), &tok) == nil {
			rec.Token = &tok
		}
		recs[name] = rec
	}
	return recs, nil
}

func (k *keychainStore) save(recs map[string]record) error {
	// Persist tokens to the keychain; store metadata (with token nil-ed) on disk.
	onDisk := make(map[string]record, len(recs))
	for name, rec := range recs {
		if rec.Token != nil && rec.Token.AccessToken != "" {
			if raw, err := json.Marshal(rec.Token); err == nil {
				_ = keyring.Set(keychainService, name, string(raw))
			}
		} else {
			// No token → ensure no stale secret lingers in the keychain.
			_ = keyring.Delete(keychainService, name)
		}
		rec.Token = nil // never write the token to the plaintext metadata file
		onDisk[name] = rec
	}
	// Delete keychain entries for connectors no longer present.
	if prev, err := k.meta.load(); err == nil {
		for name := range prev {
			if _, still := recs[name]; !still {
				_ = keyring.Delete(keychainService, name)
			}
		}
	}
	return k.meta.save(onDisk)
}

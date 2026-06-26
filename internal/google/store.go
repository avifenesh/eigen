package google

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

// keychainService / keychainUser identify the Google token entry in the OS
// keychain. A single user account per machine (the eigen user's Google login).
const (
	keychainService = "eigen-google"
	keychainUser    = "default"
)

// tokenStore persists the Google OAuth token (refresh + access). The keychain
// impl is preferred; a 0600 file is the fallback when no keyring is available.
type tokenStore interface {
	load() (*oauth2.Token, error)
	save(*oauth2.Token) error
	clear() error
}

// newTokenStore returns the keychain store when the OS keyring works, else a
// file store at path.
func newTokenStore(path string) tokenStore {
	if keychainAvailable() {
		return keychainTokenStore{}
	}
	return &fileTokenStore{path: path}
}

func keychainAvailable() bool {
	const probe = "__eigen_google_probe__"
	if err := keyring.Set(keychainService, probe, "1"); err != nil {
		return false
	}
	_, _ = keyring.Get(keychainService, probe)
	_ = keyring.Delete(keychainService, probe)
	return true
}

// keychainTokenStore keeps the token JSON in the OS keychain.
type keychainTokenStore struct{}

func (keychainTokenStore) load() (*oauth2.Token, error) {
	raw, err := keyring.Get(keychainService, keychainUser)
	if errors.Is(err, keyring.ErrNotFound) || raw == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tok oauth2.Token
	if err := json.Unmarshal([]byte(raw), &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

func (keychainTokenStore) save(tok *oauth2.Token) error {
	raw, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	return keyring.Set(keychainService, keychainUser, string(raw))
}

func (keychainTokenStore) clear() error {
	if err := keyring.Delete(keychainService, keychainUser); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}

// fileTokenStore is the no-keyring fallback: a 0600 JSON file, atomic writes.
type fileTokenStore struct {
	path string
	mu   sync.Mutex
}

func (s *fileTokenStore) load() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var tok oauth2.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

func (s *fileTokenStore) save(tok *oauth2.Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *fileTokenStore) clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// DefaultTokenPath is the file-store fallback location.
func DefaultTokenPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "google_token.json"
	}
	return filepath.Join(home, ".eigen", "google_token.json")
}

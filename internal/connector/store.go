package connector

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// record is the persisted state for one connector: the discovered endpoints,
// the (dynamically registered) client credentials, and the current OAuth token.
// One record per MCP server name.
type record struct {
	Name         string         `json:"name"`
	ServerURL    string         `json:"server_url"`
	AuthURL      string         `json:"auth_url"`
	TokenURL     string         `json:"token_url"`
	ClientID     string         `json:"client_id"`
	ClientSecret string         `json:"client_secret,omitempty"`
	Scopes       []string       `json:"scopes,omitempty"`
	Token        *oauth2.Token  `json:"token,omitempty"`
	Connected    time.Time      `json:"connected,omitempty"`
}

// Status is a connector's connection state for the GUI/CLI.
type Status struct {
	Name      string    `json:"name"`
	ServerURL string    `json:"serverUrl"`
	Connected bool      `json:"connected"`
	Expiry    time.Time `json:"expiry,omitempty"`
	Scopes    []string  `json:"scopes,omitempty"`
}

// secretStore persists connector records. The JSON-file impl is the default;
// Phase 3 swaps in an OS-keychain-backed impl behind the same interface (the
// token bytes move to the keychain, the non-secret metadata stays in the file).
type secretStore interface {
	load() (map[string]record, error)
	save(map[string]record) error
}

// fileStore persists records as one JSON file under ~/.eigen. Written 0600.
type fileStore struct {
	path string
	mu   sync.Mutex
}

func newFileStore(path string) *fileStore { return &fileStore{path: path} }

func (s *fileStore) load() (map[string]record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]record{}, nil
		}
		return nil, err
	}
	var recs map[string]record
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, fmt.Errorf("%s: %w", s.path, err)
	}
	if recs == nil {
		recs = map[string]record{}
	}
	return recs, nil
}

func (s *fileStore) save(recs map[string]record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// sortedStatuses returns connector statuses in stable name order.
func sortedStatuses(recs map[string]record) []Status {
	out := make([]Status, 0, len(recs))
	for _, r := range recs {
		st := Status{Name: r.Name, ServerURL: r.ServerURL, Scopes: r.Scopes}
		if r.Token != nil && r.Token.AccessToken != "" {
			st.Connected = true
			st.Expiry = r.Token.Expiry
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

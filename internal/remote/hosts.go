package remote

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Host is a saved remote target in ~/.eigen/hosts.json. A short Name (the map
// key) lets `eigen --remote work` stand in for a full user@host:dir, and lets a
// remote carry its own defaults (a different model, a project root) since the
// remote machine may have a different AWS profile / repo layout than local.
type Host struct {
	SSH   string `json:"ssh"`             // user@host or ~/.ssh/config alias (required)
	Dir   string `json:"dir,omitempty"`   // default remote session root
	Model string `json:"model,omitempty"` // default model id/ref for sessions on this host
	Perm  string `json:"perm,omitempty"`  // default permission posture (gated|auto)
}

// Hosts is the parsed ~/.eigen/hosts.json: a map of short name → Host.
type Hosts map[string]Host

// HostsPath is ~/.eigen/hosts.json (instance-independent: remotes are shared).
func HostsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eigen", "hosts.json")
}

// LoadHosts reads the hosts file. A missing file is not an error (empty map);
// a malformed file IS, so a typo doesn't silently drop every saved host.
func LoadHosts() (Hosts, error) {
	return loadHostsFrom(HostsPath())
}

// SaveHosts writes the hosts file atomically (temp + rename), 0600.
func SaveHosts(h Hosts) error {
	return saveHostsTo(HostsPath(), h)
}

func saveHostsTo(path string, h Hosts) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadHostsFrom(path string) (Hosts, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Hosts{}, nil
		}
		return nil, err
	}
	var h Hosts
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, err
	}
	if h == nil {
		h = Hosts{}
	}
	return h, nil
}

// Resolve turns a `--remote` argument into a concrete HostSpec, model, and
// perm. The argument is either a saved host NAME (looked up in hosts) or a
// literal `[user@]host[:dir]` spec. A literal spec's inline :dir overrides the
// saved Dir; otherwise the saved host's defaults fill in.
func (h Hosts) Resolve(arg string) (spec HostSpec, model, perm string, err error) {
	if saved, ok := h[arg]; ok {
		s, perr := ParseHostSpec(saved.SSH)
		if perr != nil {
			return HostSpec{}, "", "", perr
		}
		if saved.Dir != "" && s.Dir == "" {
			s.Dir = saved.Dir
		}
		return s, saved.Model, saved.Perm, nil
	}
	s, perr := ParseHostSpec(arg)
	if perr != nil {
		return HostSpec{}, "", "", perr
	}
	return s, "", "", nil
}

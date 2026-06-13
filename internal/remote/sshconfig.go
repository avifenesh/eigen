package remote

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DetectedHost is a remote machine found in ~/.ssh/config. Name is the Host
// alias (what you'd `ssh <name>`); HostName is the resolved address when the
// config sets one (informational).
type DetectedHost struct {
	Name     string
	HostName string // from a HostName directive, if present
	User     string // from a User directive, if present
}

// DetectSSHHosts parses ~/.ssh/config for concrete Host aliases (skipping
// wildcard/negated patterns like `Host *` and tokens with * ? !). It's a
// best-effort convenience for the Machines page — auto-discovering the hosts
// the user already configured for ssh — not a full ssh_config implementation.
func DetectSSHHosts() []DetectedHost {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return detectSSHHostsFrom(filepath.Join(home, ".ssh", "config"))
}

func detectSSHHostsFrom(path string) []DetectedHost {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var hosts []DetectedHost
	// Track the alias(es) the current block applies to so HostName/User
	// directives that follow can be attached.
	var current []int // indices into hosts for the active Host line
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := splitSSHDirective(line)
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "host":
			current = current[:0]
			for _, name := range strings.Fields(val) {
				if isConcreteHostAlias(name) {
					hosts = append(hosts, DetectedHost{Name: name})
					current = append(current, len(hosts)-1)
				}
			}
		case "hostname":
			for _, i := range current {
				hosts[i].HostName = val
			}
		case "user":
			for _, i := range current {
				hosts[i].User = val
			}
		}
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Name < hosts[j].Name })
	return hosts
}

// splitSSHDirective splits "Key value" or "Key=value" (ssh_config allows both).
func splitSSHDirective(line string) (key, val string, ok bool) {
	if eq := strings.IndexByte(line, '='); eq >= 0 && !strings.ContainsAny(line[:eq], " \t") {
		return strings.TrimSpace(line[:eq]), strings.TrimSpace(line[eq+1:]), true
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}
	return fields[0], strings.TrimSpace(strings.TrimPrefix(line, fields[0])), true
}

// isConcreteHostAlias rejects wildcard/negated patterns (Host *, !foo, a?b) —
// only literal aliases you can `ssh <name>` make sense as machine entries.
func isConcreteHostAlias(s string) bool {
	if s == "" {
		return false
	}
	return !strings.ContainsAny(s, "*?!")
}

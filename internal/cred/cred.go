// Package cred resolves credentials kept in the OS keyring, so env files can
// name a key instead of holding it.
//
// A value of the form "cred:NAME" in ~/.eigen/.env, ~/.eigen/daemon.env or the
// process environment is a reference: eigen replaces it at start with the
// output of `cred-get NAME` (Secret Service service=cred name=NAME, then
// the systemd-creds copy). Writing a key goes through `cred-set NAME`, which
// reads the value on stdin. Values never reach argv, logs or disk.
//
// cred-get / cred-set are looked up on PATH, then in ~/.local/bin. Override
// with EIGEN_CRED_GET / EIGEN_CRED_SET (a path, or "off" to disable; tests use
// fakes or "off" so they never touch the real keyring).
package cred

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// RefPrefix marks an env value that names a keyring credential.
const RefPrefix = "cred:"

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ErrNoHelper means cred-get / cred-set is not installed on this machine.
var ErrNoHelper = errors.New("credential helper not installed")

// ErrExists means StoreNew found the credential already stored.
var ErrExists = errors.New("credential already stored")

const timeout = 20 * time.Second

// ValidName reports whether name is a usable credential name.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// Ref returns the env value that references credential name.
func Ref(name string) string { return RefPrefix + name }

// ParseRef returns NAME when v is exactly "cred:NAME".
func ParseRef(v string) (string, bool) {
	name, ok := strings.CutPrefix(strings.TrimSpace(v), RefPrefix)
	if !ok || !ValidName(name) {
		return "", false
	}
	return name, true
}

func helper(envVar, bin string) (string, error) {
	if p := strings.TrimSpace(os.Getenv(envVar)); p != "" {
		// "off" disables the keyring (tests); any other value must exist.
		if p == "off" {
			return "", fmt.Errorf("%s: %w (%s=off)", bin, ErrNoHelper, envVar)
		}
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return "", fmt.Errorf("%s: %w (%s=%s)", bin, ErrNoHelper, envVar, p)
		}
		return p, nil
	}
	if p, err := exec.LookPath(bin); err == nil {
		return p, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".local", "bin", bin)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s: %w", bin, ErrNoHelper)
}

// Available reports whether the machine has the cred-set helper, i.e. keys can
// be written to the keyring instead of a file.
func Available() bool {
	_, err := helper("EIGEN_CRED_SET", "cred-set")
	return err == nil
}

// Get returns credential name from the keyring via cred-get. The error never
// carries the value.
func Get(name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("bad credential name %q", name)
	}
	bin, err := helper("EIGEN_CRED_GET", "cred-get")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, name)
	cmd.Env = os.Environ()
	// Under systemd a locked keyring would only stall the start: read the
	// systemd-creds copy first, as cred-exec does.
	if os.Getenv("INVOCATION_ID") != "" && os.Getenv("CRED_GET_PREFER_CREDS") == "" {
		cmd.Env = append(cmd.Env, "CRED_GET_PREFER_CREDS=1")
	}
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("cred-get %s: %v: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	v := strings.TrimRight(out.String(), "\r\n")
	if v == "" {
		return "", fmt.Errorf("cred-get %s: empty value", name)
	}
	return v, nil
}

// Store writes value to the keyring as name via `cred-set name` (value on
// stdin), which also refreshes the systemd-creds copy.
func Store(name, value string) error { return store(name, value, false) }

// StoreNew is Store that never replaces an existing credential: it runs
// `cred-set --new name` and returns ErrExists when name is already stored.
func StoreNew(name, value string) error { return store(name, value, true) }

func store(name, value string, onlyNew bool) error {
	if !ValidName(name) {
		return fmt.Errorf("bad credential name %q", name)
	}
	if value == "" {
		return fmt.Errorf("cred-set %s: empty value", name)
	}
	bin, err := helper("EIGEN_CRED_SET", "cred-set")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*timeout)
	defer cancel()
	args := []string{name}
	if onlyNew {
		args = []string{"--new", name}
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(value)
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = nil, &stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if onlyNew && errors.As(err, &exit) && exit.ExitCode() == 3 {
			return fmt.Errorf("cred-set %s: %w", name, ErrExists)
		}
		return fmt.Errorf("cred-set %s: %v: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

var resolved = map[string]string{}

// Resolved returns env var -> credential name for the references ResolveEnv
// replaced, so a writer (daemon install) can keep naming the same credential.
func Resolved() map[string]string {
	out := make(map[string]string, len(resolved))
	for k, v := range resolved {
		out[k] = v
	}
	return out
}

// ResolveEnv replaces every environment variable whose value is a "cred:NAME"
// reference with the keyring value. A reference that cannot be resolved is
// unset (a literal "cred:..." must never reach a provider as a key) and
// reported by variable and credential name only.
func ResolveEnv() []error {
	var errs []error
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		name, isRef := ParseRef(v)
		if !isRef {
			continue
		}
		val, err := Get(name)
		if err != nil {
			_ = os.Unsetenv(k)
			errs = append(errs, fmt.Errorf("%s: %w", k, err))
			continue
		}
		_ = os.Setenv(k, val)
		resolved[k] = name
	}
	return errs
}

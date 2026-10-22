package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTelegramTokenGoesToKeyring(t *testing.T) {
	dir := t.TempDir()
	kr := filepath.Join(dir, "kr")
	_ = os.Mkdir(kr, 0o700)
	get, set := filepath.Join(dir, "cred-get"), filepath.Join(dir, "cred-set")
	_ = os.WriteFile(get, []byte("#!/bin/sh\n[ -f \""+kr+"/$1\" ] || exit 1\ncat \""+kr+"/$1\"\n"), 0o755)
	_ = os.WriteFile(set, []byte("#!/bin/sh\ncat > \""+kr+"/$1\"\n"), 0o755)
	t.Setenv("EIGEN_CRED_GET", get)
	t.Setenv("EIGEN_CRED_SET", set)
	t.Setenv("EIGEN_TELEGRAM_TOKEN", "")
	resetStoredTelegramToken()
	t.Cleanup(resetStoredTelegramToken)

	var c Config
	if err := Set(&c, "telegram_token", "123:fake"); err != nil {
		t.Fatal(err)
	}
	if c.TelegramToken != "" {
		t.Fatal("with a keyring the token must not land in config.json")
	}
	if b, _ := os.ReadFile(filepath.Join(kr, TelegramTokenCred)); string(b) != "123:fake" {
		t.Fatalf("keyring holds %q", b)
	}
	if TelegramToken(c) != "123:fake" || Get(c, "telegram_token") != "set" {
		t.Fatal("token not read back from the keyring")
	}
	t.Setenv("EIGEN_TELEGRAM_TOKEN", "999:env")
	if TelegramToken(c) != "999:env" {
		t.Fatal("EIGEN_TELEGRAM_TOKEN must win")
	}
}

// Get runs on every redraw: the keyring is read once, not per call. Clearing the
// field while the keyring holds the token reports it instead of claiming the bridge is off.
func TestTelegramTokenReadOnceAndClearReports(t *testing.T) {
	dir := t.TempDir()
	kr := filepath.Join(dir, "kr")
	_ = os.Mkdir(kr, 0o700)
	count := filepath.Join(dir, "calls")
	get, set := filepath.Join(dir, "cred-get"), filepath.Join(dir, "cred-set")
	_ = os.WriteFile(get, []byte("#!/bin/sh\necho x >> \""+count+"\"\n[ -f \""+kr+"/$1\" ] || exit 1\ncat \""+kr+"/$1\"\n"), 0o755)
	_ = os.WriteFile(set, []byte("#!/bin/sh\ncat > \""+kr+"/$1\"\n"), 0o755)
	t.Setenv("EIGEN_CRED_GET", get)
	t.Setenv("EIGEN_CRED_SET", set)
	t.Setenv("EIGEN_TELEGRAM_TOKEN", "")
	_ = os.WriteFile(filepath.Join(kr, TelegramTokenCred), []byte("123:fake"), 0o600)
	resetStoredTelegramToken()
	t.Cleanup(resetStoredTelegramToken)

	var c Config
	for i := 0; i < 5; i++ {
		if Get(c, "telegram_token") != "set" {
			t.Fatal("token not seen")
		}
	}
	if b, _ := os.ReadFile(count); strings.Count(string(b), "x") != 1 {
		t.Fatalf("cred-get ran %d times for 5 reads", strings.Count(string(b), "x"))
	}
	err := Set(&c, "telegram_token", "")
	if err == nil || strings.Contains(err.Error(), "123:fake") || !strings.Contains(err.Error(), TelegramTokenCred) {
		t.Fatalf("want an error naming the keyring entry, got %v", err)
	}
}

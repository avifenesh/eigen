package config

import (
	"os"
	"path/filepath"
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

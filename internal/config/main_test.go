package config

import (
	"os"
	"testing"
)

// Tests never touch the machine's real keyring.
func TestMain(m *testing.M) {
	os.Setenv("EIGEN_CRED_GET", "off")
	os.Setenv("EIGEN_CRED_SET", "off")
	os.Exit(m.Run())
}

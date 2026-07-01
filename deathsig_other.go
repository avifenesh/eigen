//go:build !linux

package main

import "os/exec"

// setDeathSig is a no-op where PR_SET_PDEATHSIG doesn't exist. The supervisor's
// lock probe still prevents duplicate bridges; an orphaned bridge just isn't
// force-killed on parent death on these platforms.
func setDeathSig(cmd *exec.Cmd) {}

//go:build linux

package main

import (
	"os/exec"
	"syscall"
)

// setDeathSig makes the child die when its parent does (SIGKILL via
// PR_SET_PDEATHSIG). Linux-only; other platforms get the no-op fallback.
func setDeathSig(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

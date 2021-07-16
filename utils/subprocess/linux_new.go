//+build linux

// ^ syscall.SysProcAttr only has field Pdeathsig on Linux
// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package subprocess

import (
	"os/exec"
	"syscall"
)

func New(path string, args ...string) *exec.Cmd {
	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	return cmd
}

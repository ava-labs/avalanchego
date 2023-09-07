// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build linux
// +build linux

// ^ syscall.SysProcAttr only has field Pdeathsig on Linux

package subprocess

import (
	"os/exec"
	"syscall"
)

func NewCmd(path string, args ...string) *exec.Cmd {
	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	return cmd
}

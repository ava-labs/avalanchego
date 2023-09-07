// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !linux
// +build !linux

package subprocess

import (
	"os/exec"
)

func NewCmd(path string, args ...string) *exec.Cmd {
	return exec.Command(path, args...)
}

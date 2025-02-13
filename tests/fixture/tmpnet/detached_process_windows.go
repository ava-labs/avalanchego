// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build windows

package tmpnet

import "os/exec"

func configureDetachedProcess(*exec.Cmd) {
	panic("tmpnet deployment to windows is not supported")
}

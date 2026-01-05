// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import "fmt"

var (
	// GitCommit is set by the build script
	GitCommit string
	// Version is the version of AvalancheGo/Subnet-EVM
	Version string = "v1.14.0"
)

func init() {
	if len(GitCommit) != 0 {
		Version = fmt.Sprintf("%s@%s", Version, GitCommit)
	}
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import "fmt"

var (
	// GitCommit is set by the build script
	GitCommit string
	// Version is the version of Subnet EVM
	Version string = "v0.7.10"
)

func init() {
	if len(GitCommit) != 0 {
		Version = fmt.Sprintf("%s@%s", Version, GitCommit)
	}
}

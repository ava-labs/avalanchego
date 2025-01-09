// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
)

var (
	// GitCommit is set by the build script
	GitCommit string
	// Version is the version of Coreth
	Version string = "v0.14.1"
)

func init() {
	if len(GitCommit) != 0 {
		Version = fmt.Sprintf("%s@%s", Version, GitCommit)
	}
}

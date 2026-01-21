// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/version"
)

var (
	// GitCommit is set by the build script
	GitCommit string
	// Version is the version of Coreth, derived from AvalancheGo version
	Version string
)

func init() {
	Version = fmt.Sprintf("v%d.%d.%d", version.Current.Major, version.Current.Minor, version.Current.Patch)
	if len(GitCommit) != 0 {
		Version = fmt.Sprintf("%s@%s", Version, GitCommit)
	}
}

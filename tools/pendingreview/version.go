// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import repoversion "github.com/ava-labs/avalanchego/version"

func VersionString() string {
	commit := repoversion.GitCommit
	if commit == "" {
		commit = "unknown"
	}
	return "gh-pending-review commit=" + commit
}

// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************
// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"

	sdkVersion "github.com/ava-labs/avalanchego/version"
)

var (
	// String is displayed when CLI arg --version is used
	String string

	// Following vars are set in the build script at compile time
	GitCommit  = "unknown"
	GitVersion = "unknown"
)

func init() {
	format := "camino-node: %s, commit: %s\ncaminogo: %s, commit: %s\n  compat: %s [database: %s]\n"
	args := []interface{}{
		GitVersion,
		GitCommit,
		sdkVersion.GitVersion,
		sdkVersion.GitCommit,
		sdkVersion.Current,
		sdkVersion.CurrentDatabase,
	}
	String = fmt.Sprintf(format, args...)
}

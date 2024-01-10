// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"runtime"
	"strings"
)

var (
	// String is displayed when CLI arg --version is used
	String string

	// GitCommit is set in the build script at compile time
	GitCommit string
)

func init() {
	format := "%s [database=%s, rpcchainvm=%d"
	args := []interface{}{
		CurrentApp,
		CurrentDatabase,
		RPCChainVMProtocol,
	}
	if GitCommit != "" {
		format += ", commit=%s"
		args = append(args, GitCommit)
	}

	// add golang version
	goVersion := runtime.Version()
	goVersionNumber := strings.TrimPrefix(goVersion, "go")
	format += ", go=%s"
	args = append(args, goVersionNumber)

	format += "]\n"
	String = fmt.Sprintf(format, args...)
}

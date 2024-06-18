// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

var (
	// String is displayed when CLI arg --version is used
	String string

	// String is displayed when CLI arg --json-version is used
	JSONString string

	// GitCommit is set in the build script at compile time
	GitCommit string
)

func init() {
	flags := map[string]string{}
	format := "%s [database=%s, rpcchainvm=%d"
	args := []interface{}{
		CurrentApp,
		CurrentDatabase,
		RPCChainVMProtocol,
	}
	flags["app"] = CurrentApp.String()
	flags["database"] = CurrentDatabase.String()
	flags["rpcchainvm"] = strconv.FormatUint(uint64(RPCChainVMProtocol), 10)
	if GitCommit != "" {
		format += ", commit=%s"
		args = append(args, GitCommit)
		flags["commit"] = GitCommit
	}

	// add golang version
	goVersion := runtime.Version()
	goVersionNumber := strings.TrimPrefix(goVersion, "go")
	format += ", go=%s"
	args = append(args, goVersionNumber)
	flags["go"] = goVersionNumber

	format += "]\n"
	String = fmt.Sprintf(format, args...)

	jsonBytes, err := json.MarshalIndent(flags, "", "  ")
	if err != nil {
		panic(err)
	}
	JSONString = string(jsonBytes) + "\n"
}

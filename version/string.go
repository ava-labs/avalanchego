// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"runtime"
	"strings"
)

// GitCommit is set in the build script at compile time
var GitCommit string

// Versions contains the versions relevant to a build of avalanchego. In
// addition to supporting construction of the string displayed by
// --version, it is used to produce the output of --version-json and can
// be used to unmarshal that output.
type Versions struct {
	Application string `json:"application"`
	Database    string `json:"database"`
	RPCChainVM  uint64 `json:"rpcchainvm"`
	// Commit may be empty if GitCommit was not set at compile time
	Commit string `json:"commit"`
	Go     string `json:"go"`
}

func GetVersions() *Versions {
	return &Versions{
		Application: Current.String(),
		Database:    CurrentDatabase,
		RPCChainVM:  uint64(RPCChainVMProtocol),
		Commit:      GitCommit,
		Go:          strings.TrimPrefix(runtime.Version(), "go"),
	}
}

func (v *Versions) String() string {
	// This format maintains consistency with previous --version output
	versionString := fmt.Sprintf("%s [database=%s, rpcchainvm=%d, ", v.Application, v.Database, v.RPCChainVM)
	if len(v.Commit) > 0 {
		versionString += fmt.Sprintf("commit=%s, ", v.Commit)
	}
	return versionString + fmt.Sprintf("go=%s]", v.Go)
}

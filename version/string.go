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

// GitCommit is set in the build script at compile time
var GitCommit string

// Versions contains the versions relevant to a build of avalanchego. In
// addition to supporting construction of the strings displayed by
// --version and --version-json, it can be used to unmarshal the output
// of --version-json.
type Versions struct {
	Application string `json:"application"`
	Database    string `json:"database"`
	RPCChainVM  string `json:"rpcchainvm"`
	// Commit may be empty if GitCommit was not set at compile time
	Commit string `json:"commit"`
	Go     string `json:"go"`
}

func GetVersions() *Versions {
	versions := &Versions{
		Application: CurrentApp.String(),
		Database:    CurrentDatabase.String(),
		RPCChainVM:  strconv.FormatUint(uint64(RPCChainVMProtocol), 10),
		Go:          strings.TrimPrefix(runtime.Version(), "go"),
	}
	if GitCommit != "" {
		versions.Commit = GitCommit
	}
	return versions
}

func (v *Versions) String() string {
	// This format maintains consistency with previous --version output
	versionString := fmt.Sprintf("%s [database=%s, rpcchainvm=%s, ", v.Application, v.Database, v.RPCChainVM)
	if len(v.Commit) > 0 {
		versionString += fmt.Sprintf("commit=%s, ", v.Commit)
	}
	return versionString + fmt.Sprintf("go=%s]", v.Go)
}

func (v *Versions) JSON() string {
	jsonBytes, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(jsonBytes)
}

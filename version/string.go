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

type namedVersion struct {
	name    string
	version string
}

func init() {
	// Define the ordered set of versions that are common to regular and JSON
	// output. The order is maintained to ensure consistency with previous
	// --version output.
	versions := []namedVersion{
		{name: "database", version: CurrentDatabase.String()},
		{name: "rpcchainvm", version: strconv.FormatUint(uint64(RPCChainVMProtocol), 10)},
	}

	// Add git commit if available
	if GitCommit != "" {
		versions = append(versions, namedVersion{name: "commit", version: GitCommit})
	}

	// Add golang version
	goVersion := runtime.Version()
	goVersionNumber := strings.TrimPrefix(goVersion, "go")
	versions = append(versions, namedVersion{name: "go", version: goVersionNumber})

	// Set the value of the regular version string
	versionPairs := make([]string, len(versions))
	for i, v := range versions {
		versionPairs[i] = fmt.Sprintf("%s=%s", v.name, v.version)
	}
	// This format maintains consistency with previous --version output
	String = fmt.Sprintf("%s [%s]\n", CurrentApp, strings.Join(versionPairs, ", "))

	// Include the app version as a regular value in the JSON output
	versions = append(versions, namedVersion{name: "app", version: CurrentApp.String()})

	// Convert versions to a map for more compact JSON output
	versionMap := make(map[string]string, len(versions))
	for _, v := range versions {
		versionMap[v.name] = v.version
	}

	jsonBytes, err := json.MarshalIndent(versionMap, "", "  ")
	if err != nil {
		panic(err)
	}
	JSONString = string(jsonBytes) + "\n"
}

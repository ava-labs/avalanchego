// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionsGetString(t *testing.T) {
	versions := Versions{
		Application: "1",
		Database:    "2",
		RPCChainVM:  "3",
		Go:          "5",
	}
	expectedVersions := fmt.Sprintf(
		"%s [database=%s, rpcchainvm=%s, go=%s]",
		versions.Application,
		versions.Database,
		versions.RPCChainVM,
		versions.Go,
	)
	require.Equal(t, expectedVersions, versions.String())

	versions.Commit = "eafd"
	expectedVersions = fmt.Sprintf(
		"%s [database=%s, rpcchainvm=%s, commit=%s, go=%s]",
		versions.Application,
		versions.Database,
		versions.RPCChainVM,
		versions.Commit,
		versions.Go,
	)
	require.Equal(t, expectedVersions, versions.String())
}

func TestVersionsJSON(t *testing.T) {
	versions := GetVersions()
	jsonString := versions.JSON()
	unmarshalledVersions := &Versions{}
	require.NoError(t, json.Unmarshal([]byte(jsonString), unmarshalledVersions))
	require.Equal(t, versions, unmarshalledVersions)
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	_ "embed"
)

//go:embed current.txt
var currentFile string

func TestCurrentRPCChainVMCompatible(t *testing.T) {
	compatibleVersions := RPCChainVMProtocolCompatibility[RPCChainVMProtocol]
	require.Contains(
		t,
		compatibleVersions,
		fmt.Sprintf("v%d.%d.%d", Current.Major, Current.Minor, Current.Patch),
	)
}

func TestCurrentFileMatchesCurrent(t *testing.T) {
	require.Equal(t, Current.Semantic(), currentFile, "current.txt")
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCurrentRPCChainVMCompatible(t *testing.T) {
	compatibleVersions := RPCChainVMProtocolCompatibility[RPCChainVMProtocol]
	require.Contains(
		t,
		compatibleVersions,
		fmt.Sprintf("v%d.%d.%d", Current.Major, Current.Minor, Current.Patch),
	)
}

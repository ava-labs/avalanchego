// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/constants"
)

func TestSampleBootstrappers(t *testing.T) {
	require := require.New(t)

	for networkID, networkName := range constants.NetworkIDToNetworkName {
		length := 10
		bootstrappers := SampleBootstrappers(networkID, length)
		t.Logf("%s bootstrappers: %+v", networkName, bootstrappers)

		if networkID == constants.MainnetID || networkID == constants.FujiID {
			require.Len(bootstrappers, length)
		}
	}
}

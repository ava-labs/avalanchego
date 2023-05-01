// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/stretchr/testify/require"
)

func TestSampleBootstrappers(t *testing.T) {
	require := require.New(t)

	for networkID, networkName := range constants.NetworkIDToNetworkName {
		bootstrappers := SampleBootstrappers(networkID, 10)
		t.Logf("%s bootstrappers: %+v", networkName, bootstrappers)

		if networkID == constants.MainnetID || networkID == constants.FujiID {
			require.NotEmpty(bootstrappers)
		}
	}
}

// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/stretchr/testify/require"
)

func TestGenesisChainData(t *testing.T) {
	type vmTest struct {
		vmIDs      []ids.ID
		expectedID []string
	}
	tests := []struct {
		networkID uint32
		vmTest    vmTest
	}{
		{
			networkID: constants.CaminoID,
			vmTest: vmTest{
				vmIDs:      []ids.ID{constants.AVMID, constants.EVMID},
				expectedID: []string{"4Y8KXHrpNRiRBAC3nC6mMzGiE19Rnnwh2rUQ6RU7HdMhvfkS3", "2qv12ysjDcdVpJvz3xSaPduX4XhkQNGXafLvvJFLrJgVF7CSjU"},
			},
		},
	}

	for _, test := range tests {
		t.Run(constants.NetworkIDToNetworkName[test.networkID], func(t *testing.T) {
			require := require.New(t)

			config := GetConfig(test.networkID)
			genesisBytes, _, err := FromConfig(config)
			require.NoError(err)

			genesisTx, lock, err := GenesisChainData(genesisBytes, test.vmTest.vmIDs)
			require.NoError(err)
			require.True(lock)

			for idx, tx := range genesisTx {
				require.Equal(
					test.vmTest.expectedID[idx],
					tx.ID().String(),
					"%s genesisID with networkID %d mismatch",
					test.vmTest.vmIDs[idx],
					test.networkID,
				)
			}
		})
	}
}

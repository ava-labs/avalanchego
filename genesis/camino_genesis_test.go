// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
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
				expectedID: []string{"yMQo4UEa2Gkk6aSmifkUuBsystV1iu1NppatvoYz6yCDnRjiq", "RinAZCjd5Dm4wk1FBWiXiiSW2VZkjzgNyR7nNBRkuCvG9zRkJ"},
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

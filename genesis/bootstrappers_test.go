// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/stretchr/testify/require"
)

// Make sure all hard-coded node IPs and IDs are in a valid format.
func TestSampleBootstrappers(t *testing.T) {
	require := require.New(t)

	for _, networkID := range []uint32{constants.FujiID, constants.MainnetID} {
		beaconIPs, beaconIDs := SampleBootstrappers(networkID, 5)
		for i, beaconID := range beaconIDs {
			_, err := ids.NodeIDFromString(beaconID)
			require.NoError(err)

			_, err = ips.ToIPPort(beaconIPs[i])
			require.NoError(err)
		}
	}
}

// Check the compatibility during migration to JSON-based beacon lists
func TestInitializedBootstrappers(t *testing.T) {
	require := require.New(t)

	for _, networkID := range []uint32{constants.FujiID, constants.MainnetID} {
		bootstrappersFromJSON := bootstrappersPerNetworkID[networkID]
		loaded := getBootstrappers(networkID)
		for i, node := range bootstrappersFromJSON {
			require.Equal(node.ID, loaded[i].ID, fmt.Sprintf("%d-th node mismatch ID", i))
			require.Equal(node.IP, loaded[i].IP, fmt.Sprintf("%d-th node mismatch IP", i))
		}
	}
}

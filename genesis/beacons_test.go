// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/stretchr/testify/require"
)

// Make sure all hard-coded node IPs and IDs are in a valid format.
func TestSampleBeacons(t *testing.T) {
	require := require.New(t)

	for _, networkID := range []uint32{constants.FujiID, constants.MainnetID} {
		beaconIPs, beaconIDs := SampleBeacons(networkID, 5)
		for i, beaconID := range beaconIDs {
			_, err := ids.NodeIDFromString(beaconID)
			require.NoError(err)

			_, err = ips.ToIPPort(beaconIPs[i])
			require.NoError(err)
		}
	}
}

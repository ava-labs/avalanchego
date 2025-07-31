// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

func TestOverriddenManager(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	nodeID1 := ids.GenerateTestNodeID()
	subnetID0 := ids.GenerateTestID()
	subnetID1 := ids.GenerateTestID()

	m := validators.NewManager()
	require.NoError(m.AddStaker(subnetID0, nodeID0, nil, ids.Empty, 1))
	require.NoError(m.AddStaker(subnetID1, nodeID1, nil, ids.Empty, 1))

	om := newOverriddenManager(subnetID0, m)
	_, ok := om.GetValidator(subnetID0, nodeID0)
	require.True(ok)
	_, ok = om.GetValidator(subnetID0, nodeID1)
	require.False(ok)
	_, ok = om.GetValidator(subnetID1, nodeID0)
	require.True(ok)
	_, ok = om.GetValidator(subnetID1, nodeID1)
	require.False(ok)

	require.NoError(om.RemoveWeight(subnetID1, nodeID0, 1))
	_, ok = om.GetValidator(subnetID0, nodeID0)
	require.False(ok)
	_, ok = om.GetValidator(subnetID0, nodeID1)
	require.False(ok)
	_, ok = om.GetValidator(subnetID1, nodeID0)
	require.False(ok)
	_, ok = om.GetValidator(subnetID1, nodeID1)
	require.False(ok)
}

func TestOverriddenString(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.EmptyNodeID
	nodeID1, err := ids.NodeIDFromString("NodeID-QLbz7JHiBTspS962RLKV8GndWFwdYhk6V")
	require.NoError(err)

	subnetID0, err := ids.FromString("TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES")
	require.NoError(err)
	subnetID1, err := ids.FromString("2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w")
	require.NoError(err)

	m := validators.NewManager()
	require.NoError(m.AddStaker(subnetID0, nodeID0, nil, ids.Empty, 1))
	require.NoError(m.AddStaker(subnetID0, nodeID1, nil, ids.Empty, math.MaxInt64-1))
	require.NoError(m.AddStaker(subnetID1, nodeID1, nil, ids.Empty, 1))

	om := newOverriddenManager(subnetID0, m)
	expected := `Overridden Validator Manager (SubnetID = TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES): Validator Manager: (Size = 2)
    Subnet[TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES]: Validator Set: (Size = 2, Weight = 9223372036854775807)
        Validator[0]: NodeID-111111111111111111116DBWJs, 1
        Validator[1]: NodeID-QLbz7JHiBTspS962RLKV8GndWFwdYhk6V, 9223372036854775806
    Subnet[2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w]: Validator Set: (Size = 1, Weight = 1)
        Validator[0]: NodeID-QLbz7JHiBTspS962RLKV8GndWFwdYhk6V, 1`
	result := om.String()
	require.Equal(expected, result)
}

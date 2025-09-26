// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var _ ManagerCallbackListener = (*managerCallbackListener)(nil)

type managerCallbackListener struct {
	t         *testing.T
	onAdd     func(ids.ID, ids.NodeID, *bls.PublicKey, ids.ID, uint64)
	onWeight  func(ids.ID, ids.NodeID, uint64, uint64)
	onRemoved func(ids.ID, ids.NodeID, uint64)
}

func (c *managerCallbackListener) OnValidatorAdded(subnetID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
	if c.onAdd != nil {
		c.onAdd(subnetID, nodeID, pk, txID, weight)
	} else {
		c.t.Fail()
	}
}

func (c *managerCallbackListener) OnValidatorRemoved(subnetID ids.ID, nodeID ids.NodeID, weight uint64) {
	if c.onRemoved != nil {
		c.onRemoved(subnetID, nodeID, weight)
	} else {
		c.t.Fail()
	}
}

func (c *managerCallbackListener) OnValidatorWeightChanged(subnetID ids.ID, nodeID ids.NodeID, oldWeight, newWeight uint64) {
	if c.onWeight != nil {
		c.onWeight(subnetID, nodeID, oldWeight, newWeight)
	} else {
		c.t.Fail()
	}
}

func TestAddZeroWeight(t *testing.T) {
	require := require.New(t)

	m := NewManager().(*manager)
	err := m.AddStaker(ids.GenerateTestID(), ids.GenerateTestNodeID(), nil, ids.Empty, 0)
	require.ErrorIs(err, ErrZeroWeight)
	require.Empty(m.subnetToVdrs)
}

func TestAddDuplicate(t *testing.T) {
	require := require.New(t)

	m := NewManager()
	subnetID := ids.GenerateTestID()

	nodeID := ids.GenerateTestNodeID()
	require.NoError(m.AddStaker(subnetID, nodeID, nil, ids.Empty, 1))

	err := m.AddStaker(subnetID, nodeID, nil, ids.Empty, 1)
	require.ErrorIs(err, errDuplicateValidator)
}

func TestAddOverflow(t *testing.T) {
	require := require.New(t)

	m := NewManager()
	subnetID := ids.GenerateTestID()
	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()
	require.NoError(m.AddStaker(subnetID, nodeID1, nil, ids.Empty, 1))

	require.NoError(m.AddStaker(subnetID, nodeID2, nil, ids.Empty, math.MaxUint64))

	_, err := m.TotalWeight(subnetID)
	require.ErrorIs(err, errTotalWeightNotUint64)

	set := set.Of(nodeID1, nodeID2)
	_, err = m.SubsetWeight(subnetID, set)
	require.ErrorIs(err, safemath.ErrOverflow)
}

func TestAddWeightZeroWeight(t *testing.T) {
	require := require.New(t)

	m := NewManager()
	subnetID := ids.GenerateTestID()

	nodeID := ids.GenerateTestNodeID()
	require.NoError(m.AddStaker(subnetID, nodeID, nil, ids.Empty, 1))

	err := m.AddWeight(subnetID, nodeID, 0)
	require.ErrorIs(err, ErrZeroWeight)
}

func TestAddWeightOverflow(t *testing.T) {
	require := require.New(t)

	m := NewManager()
	subnetID := ids.GenerateTestID()
	require.NoError(m.AddStaker(subnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	nodeID := ids.GenerateTestNodeID()
	require.NoError(m.AddStaker(subnetID, nodeID, nil, ids.Empty, 1))

	require.NoError(m.AddWeight(subnetID, nodeID, math.MaxUint64-1))

	_, err := m.TotalWeight(subnetID)
	require.ErrorIs(err, errTotalWeightNotUint64)
}

func TestGetWeight(t *testing.T) {
	require := require.New(t)

	m := NewManager()
	subnetID := ids.GenerateTestID()

	nodeID := ids.GenerateTestNodeID()
	require.Zero(m.GetWeight(subnetID, nodeID))

	require.NoError(m.AddStaker(subnetID, nodeID, nil, ids.Empty, 1))

	totalWeight, err := m.TotalWeight(subnetID)
	require.NoError(err)
	require.Equal(uint64(1), totalWeight)
}

func TestSubsetWeight(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()

	weight0 := uint64(93)
	weight1 := uint64(123)
	weight2 := uint64(810)

	subset := set.Of(nodeID0, nodeID1)

	m := NewManager()
	subnetID := ids.GenerateTestID()

	require.NoError(m.AddStaker(subnetID, nodeID0, nil, ids.Empty, weight0))
	require.NoError(m.AddStaker(subnetID, nodeID1, nil, ids.Empty, weight1))
	require.NoError(m.AddStaker(subnetID, nodeID2, nil, ids.Empty, weight2))

	expectedWeight := weight0 + weight1
	subsetWeight, err := m.SubsetWeight(subnetID, subset)
	require.NoError(err)
	require.Equal(expectedWeight, subsetWeight)
}

func TestRemoveWeightZeroWeight(t *testing.T) {
	require := require.New(t)

	m := NewManager()
	subnetID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	require.NoError(m.AddStaker(subnetID, nodeID, nil, ids.Empty, 1))

	err := m.RemoveWeight(subnetID, nodeID, 0)
	require.ErrorIs(err, ErrZeroWeight)
}

func TestRemoveWeightMissingValidator(t *testing.T) {
	require := require.New(t)

	m := NewManager()
	subnetID := ids.GenerateTestID()

	require.NoError(m.AddStaker(subnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	err := m.RemoveWeight(subnetID, ids.GenerateTestNodeID(), 1)
	require.ErrorIs(err, errMissingValidator)
}

func TestRemoveWeightUnderflow(t *testing.T) {
	require := require.New(t)

	m := NewManager()
	subnetID := ids.GenerateTestID()

	require.NoError(m.AddStaker(subnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	nodeID := ids.GenerateTestNodeID()
	require.NoError(m.AddStaker(subnetID, nodeID, nil, ids.Empty, 1))

	err := m.RemoveWeight(subnetID, nodeID, 2)
	require.ErrorIs(err, safemath.ErrUnderflow)

	totalWeight, err := m.TotalWeight(subnetID)
	require.NoError(err)
	require.Equal(uint64(2), totalWeight)
}

func TestGet(t *testing.T) {
	require := require.New(t)

	m := NewManager()
	subnetID := ids.GenerateTestID()

	nodeID := ids.GenerateTestNodeID()
	_, ok := m.GetValidator(subnetID, nodeID)
	require.False(ok)

	sk, err := localsigner.New()
	require.NoError(err)

	pk := sk.PublicKey()
	require.NoError(m.AddStaker(subnetID, nodeID, pk, ids.Empty, 1))

	vdr0, ok := m.GetValidator(subnetID, nodeID)
	require.True(ok)
	require.Equal(nodeID, vdr0.NodeID)
	require.Equal(pk, vdr0.PublicKey)
	require.Equal(uint64(1), vdr0.Weight)

	require.NoError(m.AddWeight(subnetID, nodeID, 1))

	vdr1, ok := m.GetValidator(subnetID, nodeID)
	require.True(ok)
	require.Equal(nodeID, vdr0.NodeID)
	require.Equal(pk, vdr0.PublicKey)
	require.Equal(uint64(1), vdr0.Weight)
	require.Equal(nodeID, vdr1.NodeID)
	require.Equal(pk, vdr1.PublicKey)
	require.Equal(uint64(2), vdr1.Weight)

	require.NoError(m.RemoveWeight(subnetID, nodeID, 2))
	_, ok = m.GetValidator(subnetID, nodeID)
	require.False(ok)
}

func TestNum(t *testing.T) {
	var (
		require = require.New(t)

		m = NewManager()

		subnetID0 = ids.GenerateTestID()
		subnetID1 = ids.GenerateTestID()
		nodeID0   = ids.GenerateTestNodeID()
		nodeID1   = ids.GenerateTestNodeID()
	)

	require.Zero(m.NumSubnets())
	require.Zero(m.NumValidators(subnetID0))
	require.Zero(m.NumValidators(subnetID1))

	require.NoError(m.AddStaker(subnetID0, nodeID0, nil, ids.Empty, 1))

	require.Equal(1, m.NumSubnets())
	require.Equal(1, m.NumValidators(subnetID0))
	require.Zero(m.NumValidators(subnetID1))

	require.NoError(m.AddStaker(subnetID0, nodeID1, nil, ids.Empty, 1))

	require.Equal(1, m.NumSubnets())
	require.Equal(2, m.NumValidators(subnetID0))
	require.Zero(m.NumValidators(subnetID1))

	require.NoError(m.AddStaker(subnetID1, nodeID1, nil, ids.Empty, 2))

	require.Equal(2, m.NumSubnets())
	require.Equal(2, m.NumValidators(subnetID0))
	require.Equal(1, m.NumValidators(subnetID1))

	require.NoError(m.RemoveWeight(subnetID0, nodeID1, 1))

	require.Equal(2, m.NumSubnets())
	require.Equal(1, m.NumValidators(subnetID0))
	require.Equal(1, m.NumValidators(subnetID1))

	require.NoError(m.RemoveWeight(subnetID0, nodeID0, 1))

	require.Equal(1, m.NumSubnets())
	require.Zero(m.NumValidators(subnetID0))
	require.Equal(1, m.NumValidators(subnetID1))

	require.NoError(m.RemoveWeight(subnetID1, nodeID1, 2))

	require.Zero(m.NumSubnets())
	require.Zero(m.NumValidators(subnetID0))
	require.Zero(m.NumValidators(subnetID1))
}

func TestGetMap(t *testing.T) {
	require := require.New(t)

	m := NewManager()
	subnetID := ids.GenerateTestID()

	mp := m.GetMap(subnetID)
	require.Empty(mp)

	sk, err := localsigner.New()
	require.NoError(err)

	pk := sk.PublicKey()
	nodeID0 := ids.GenerateTestNodeID()
	require.NoError(m.AddStaker(subnetID, nodeID0, pk, ids.Empty, 2))

	mp = m.GetMap(subnetID)
	require.Len(mp, 1)
	require.Contains(mp, nodeID0)

	node0 := mp[nodeID0]
	require.Equal(nodeID0, node0.NodeID)
	require.Equal(pk, node0.PublicKey)
	require.Equal(uint64(2), node0.Weight)

	nodeID1 := ids.GenerateTestNodeID()
	require.NoError(m.AddStaker(subnetID, nodeID1, nil, ids.Empty, 1))

	mp = m.GetMap(subnetID)
	require.Len(mp, 2)
	require.Contains(mp, nodeID0)
	require.Contains(mp, nodeID1)

	node0 = mp[nodeID0]
	require.Equal(nodeID0, node0.NodeID)
	require.Equal(pk, node0.PublicKey)
	require.Equal(uint64(2), node0.Weight)

	node1 := mp[nodeID1]
	require.Equal(nodeID1, node1.NodeID)
	require.Nil(node1.PublicKey)
	require.Equal(uint64(1), node1.Weight)

	require.NoError(m.RemoveWeight(subnetID, nodeID0, 1))
	require.Equal(nodeID0, node0.NodeID)
	require.Equal(pk, node0.PublicKey)
	require.Equal(uint64(2), node0.Weight)

	mp = m.GetMap(subnetID)
	require.Len(mp, 2)
	require.Contains(mp, nodeID0)
	require.Contains(mp, nodeID1)

	node0 = mp[nodeID0]
	require.Equal(nodeID0, node0.NodeID)
	require.Equal(pk, node0.PublicKey)
	require.Equal(uint64(1), node0.Weight)

	node1 = mp[nodeID1]
	require.Equal(nodeID1, node1.NodeID)
	require.Nil(node1.PublicKey)
	require.Equal(uint64(1), node1.Weight)

	require.NoError(m.RemoveWeight(subnetID, nodeID0, 1))

	mp = m.GetMap(subnetID)
	require.Len(mp, 1)
	require.Contains(mp, nodeID1)

	node1 = mp[nodeID1]
	require.Equal(nodeID1, node1.NodeID)
	require.Nil(node1.PublicKey)
	require.Equal(uint64(1), node1.Weight)

	require.NoError(m.RemoveWeight(subnetID, nodeID1, 1))

	require.Empty(m.GetMap(subnetID))
}

func TestGetAllMaps(t *testing.T) {
	require := require.New(t)

	m := NewManager()
	subnetID0 := ids.GenerateTestID()
	subnetID1 := ids.GenerateTestID()

	maps := m.GetAllMaps()
	require.Empty(maps)

	sk, err := localsigner.New()
	require.NoError(err)

	pk := sk.PublicKey()
	nodeID0 := ids.GenerateTestNodeID()
	require.NoError(m.AddStaker(subnetID0, nodeID0, pk, ids.Empty, 2))

	maps = m.GetAllMaps()
	require.Len(maps, 1)
	map0, ok := maps[subnetID0]
	require.True(ok)
	require.Len(map0, 1)
	require.Contains(map0, nodeID0)

	node0 := map0[nodeID0]
	require.Equal(nodeID0, node0.NodeID)
	require.Equal(pk, node0.PublicKey)
	require.Equal(uint64(2), node0.Weight)

	nodeID1 := ids.GenerateTestNodeID()
	require.NoError(m.AddStaker(subnetID1, nodeID1, nil, ids.Empty, 1))

	maps = m.GetAllMaps()
	require.Len(maps, 2)
	map0, ok = maps[subnetID0]
	require.True(ok)
	require.Contains(map0, nodeID0)
	map1, ok := maps[subnetID1]
	require.True(ok)
	require.Contains(map1, nodeID1)

	node0 = map0[nodeID0]
	require.Equal(nodeID0, node0.NodeID)
	require.Equal(pk, node0.PublicKey)
	require.Equal(uint64(2), node0.Weight)

	node1 := map1[nodeID1]
	require.Equal(nodeID1, node1.NodeID)
	require.Nil(node1.PublicKey)
	require.Equal(uint64(1), node1.Weight)

	require.NoError(m.RemoveWeight(subnetID0, nodeID0, 1))
	require.Equal(nodeID0, node0.NodeID)
	require.Equal(pk, node0.PublicKey)
	require.Equal(uint64(2), node0.Weight)

	maps = m.GetAllMaps()
	require.Len(maps, 2)
	map0, ok = maps[subnetID0]
	require.True(ok)
	map1, ok = maps[subnetID1]
	require.True(ok)
	require.Contains(map0, nodeID0)
	require.Contains(map1, nodeID1)

	node0 = map0[nodeID0]
	require.Equal(nodeID0, node0.NodeID)
	require.Equal(pk, node0.PublicKey)
	require.Equal(uint64(1), node0.Weight)

	node1 = map1[nodeID1]
	require.Equal(nodeID1, node1.NodeID)
	require.Nil(node1.PublicKey)
	require.Equal(uint64(1), node1.Weight)

	require.NoError(m.RemoveWeight(subnetID0, nodeID0, 1))

	maps = m.GetAllMaps()
	require.Len(maps, 1)
	map0, ok = maps[subnetID0]
	require.False(ok)
	map1, ok = maps[subnetID1]
	require.True(ok)
	require.Contains(map1, nodeID1)

	node1 = map1[nodeID1]
	require.Equal(nodeID1, node1.NodeID)
	require.Nil(node1.PublicKey)
	require.Equal(uint64(1), node1.Weight)

	require.NoError(m.RemoveWeight(subnetID1, nodeID1, 1))

	require.Empty(m.GetAllMaps())
}

func TestWeight(t *testing.T) {
	require := require.New(t)

	vdr0 := ids.BuildTestNodeID([]byte{1})
	weight0 := uint64(93)
	vdr1 := ids.BuildTestNodeID([]byte{2})
	weight1 := uint64(123)

	m := NewManager()
	subnetID := ids.GenerateTestID()
	require.NoError(m.AddStaker(subnetID, vdr0, nil, ids.Empty, weight0))

	require.NoError(m.AddStaker(subnetID, vdr1, nil, ids.Empty, weight1))

	setWeight, err := m.TotalWeight(subnetID)
	require.NoError(err)
	expectedWeight := weight0 + weight1
	require.Equal(expectedWeight, setWeight)
}

func TestSample(t *testing.T) {
	require := require.New(t)

	m := NewManager()
	subnetID := ids.GenerateTestID()

	sampled, err := m.Sample(subnetID, 0)
	require.NoError(err)
	require.Empty(sampled)

	sk, err := localsigner.New()
	require.NoError(err)

	nodeID0 := ids.GenerateTestNodeID()
	pk := sk.PublicKey()
	require.NoError(m.AddStaker(subnetID, nodeID0, pk, ids.Empty, 1))

	sampled, err = m.Sample(subnetID, 1)
	require.NoError(err)
	require.Equal([]ids.NodeID{nodeID0}, sampled)

	_, err = m.Sample(subnetID, 2)
	require.ErrorIs(err, errInsufficientWeight)

	nodeID1 := ids.GenerateTestNodeID()
	require.NoError(m.AddStaker(subnetID, nodeID1, nil, ids.Empty, math.MaxInt64-1))

	sampled, err = m.Sample(subnetID, 1)
	require.NoError(err)
	require.Equal([]ids.NodeID{nodeID1}, sampled)

	sampled, err = m.Sample(subnetID, 2)
	require.NoError(err)
	require.Equal([]ids.NodeID{nodeID1, nodeID1}, sampled)

	sampled, err = m.Sample(subnetID, 3)
	require.NoError(err)
	require.Equal([]ids.NodeID{nodeID1, nodeID1, nodeID1}, sampled)
}

func TestString(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.EmptyNodeID
	nodeID1, err := ids.NodeIDFromString("NodeID-QLbz7JHiBTspS962RLKV8GndWFwdYhk6V")
	require.NoError(err)

	subnetID0, err := ids.FromString("TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES")
	require.NoError(err)
	subnetID1, err := ids.FromString("2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w")
	require.NoError(err)

	m := NewManager()
	require.NoError(m.AddStaker(subnetID0, nodeID0, nil, ids.Empty, 1))
	require.NoError(m.AddStaker(subnetID0, nodeID1, nil, ids.Empty, math.MaxInt64-1))
	require.NoError(m.AddStaker(subnetID1, nodeID1, nil, ids.Empty, 1))

	expected := `Validator Manager: (Size = 2)
    Subnet[TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES]: Validator Set: (Size = 2, Weight = 9223372036854775807)
        Validator[0]: NodeID-111111111111111111116DBWJs, 1
        Validator[1]: NodeID-QLbz7JHiBTspS962RLKV8GndWFwdYhk6V, 9223372036854775806
    Subnet[2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w]: Validator Set: (Size = 1, Weight = 1)
        Validator[0]: NodeID-QLbz7JHiBTspS962RLKV8GndWFwdYhk6V, 1`
	result := m.String()
	require.Equal(expected, result)
}

func TestAddCallback(t *testing.T) {
	require := require.New(t)

	expectedSK, err := localsigner.New()
	require.NoError(err)

	var (
		expectedNodeID           = ids.GenerateTestNodeID()
		expectedPK               = expectedSK.PublicKey()
		expectedTxID             = ids.GenerateTestID()
		expectedWeight    uint64 = 1
		expectedSubnetID0        = ids.GenerateTestID()
		expectedSubnetID1        = ids.GenerateTestID()

		m                = NewManager()
		managerCallCount = 0
		setCallCount     = 0
	)
	m.RegisterCallbackListener(&managerCallbackListener{
		t: t,
		onAdd: func(subnetID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
			require.Contains([]ids.ID{expectedSubnetID0, expectedSubnetID1}, subnetID)
			require.Equal(expectedNodeID, nodeID)
			require.Equal(expectedPK, pk)
			require.Equal(expectedTxID, txID)
			require.Equal(expectedWeight, weight)
			managerCallCount++
		},
	})
	m.RegisterSetCallbackListener(expectedSubnetID0, &setCallbackListener{
		t: t,
		onAdd: func(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
			require.Equal(expectedNodeID, nodeID)
			require.Equal(expectedPK, pk)
			require.Equal(expectedTxID, txID)
			require.Equal(expectedWeight, weight)
			setCallCount++
		},
	})
	require.NoError(m.AddStaker(expectedSubnetID0, expectedNodeID, expectedPK, expectedTxID, expectedWeight))
	require.Equal(1, managerCallCount) // should be called for expectedSubnetID0
	require.Equal(1, setCallCount)     // should be called for expectedSubnetID0

	require.NoError(m.AddStaker(expectedSubnetID1, expectedNodeID, expectedPK, expectedTxID, expectedWeight))
	require.Equal(2, managerCallCount) // should be called for expectedSubnetID1
	require.Equal(1, setCallCount)     // should not be called for expectedSubnetID1
}

func TestAddWeightCallback(t *testing.T) {
	require := require.New(t)

	expectedSK, err := localsigner.New()
	require.NoError(err)

	var (
		expectedNodeID             = ids.GenerateTestNodeID()
		expectedPK                 = expectedSK.PublicKey()
		expectedTxID               = ids.GenerateTestID()
		expectedOldWeight   uint64 = 1
		expectedAddedWeight uint64 = 10
		expectedNewWeight          = expectedOldWeight + expectedAddedWeight
		expectedSubnetID0          = ids.GenerateTestID()
		expectedSubnetID1          = ids.GenerateTestID()

		m                      = NewManager()
		managerAddCallCount    = 0
		managerChangeCallCount = 0
		setAddCallCount        = 0
		setChangeCallCount     = 0
	)

	require.NoError(m.AddStaker(expectedSubnetID0, expectedNodeID, expectedPK, expectedTxID, expectedOldWeight))

	m.RegisterCallbackListener(&managerCallbackListener{
		t: t,
		onAdd: func(subnetID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
			require.Contains([]ids.ID{expectedSubnetID0, expectedSubnetID1}, subnetID)
			require.Equal(expectedNodeID, nodeID)
			require.Equal(expectedPK, pk)
			require.Equal(expectedTxID, txID)
			require.Equal(expectedOldWeight, weight)
			managerAddCallCount++
		},
		onWeight: func(subnetID ids.ID, nodeID ids.NodeID, oldWeight, newWeight uint64) {
			require.Contains([]ids.ID{expectedSubnetID0, expectedSubnetID1}, subnetID)
			require.Equal(expectedNodeID, nodeID)
			require.Equal(expectedOldWeight, oldWeight)
			require.Equal(expectedNewWeight, newWeight)
			managerChangeCallCount++
		},
	})
	m.RegisterSetCallbackListener(expectedSubnetID0, &setCallbackListener{
		t: t,
		onAdd: func(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
			require.Equal(expectedNodeID, nodeID)
			require.Equal(expectedPK, pk)
			require.Equal(expectedTxID, txID)
			require.Equal(expectedOldWeight, weight)
			setAddCallCount++
		},
		onWeight: func(nodeID ids.NodeID, oldWeight, newWeight uint64) {
			require.Equal(expectedNodeID, nodeID)
			require.Equal(expectedOldWeight, oldWeight)
			require.Equal(expectedNewWeight, newWeight)
			setChangeCallCount++
		},
	})
	require.Equal(1, managerAddCallCount)
	require.Zero(managerChangeCallCount)
	require.Equal(1, setAddCallCount)
	require.Zero(setChangeCallCount)

	require.NoError(m.AddWeight(expectedSubnetID0, expectedNodeID, expectedAddedWeight))
	require.Equal(1, managerAddCallCount)
	require.Equal(1, managerChangeCallCount)
	require.Equal(1, setAddCallCount)
	require.Equal(1, setChangeCallCount)

	require.NoError(m.AddStaker(expectedSubnetID1, expectedNodeID, expectedPK, expectedTxID, expectedOldWeight))
	require.Equal(2, managerAddCallCount)
	require.Equal(1, managerChangeCallCount)
	require.Equal(1, setAddCallCount)
	require.Equal(1, setChangeCallCount)

	require.NoError(m.AddWeight(expectedSubnetID1, expectedNodeID, expectedAddedWeight))
	require.Equal(2, managerAddCallCount)
	require.Equal(2, managerChangeCallCount)
	require.Equal(1, setAddCallCount)
	require.Equal(1, setChangeCallCount)
}

func TestRemoveWeightCallback(t *testing.T) {
	require := require.New(t)

	expectedSK, err := localsigner.New()
	require.NoError(err)

	var (
		expectedNodeID               = ids.GenerateTestNodeID()
		expectedPK                   = expectedSK.PublicKey()
		expectedTxID                 = ids.GenerateTestID()
		expectedNewWeight     uint64 = 1
		expectedRemovedWeight uint64 = 10
		expectedOldWeight            = expectedNewWeight + expectedRemovedWeight
		expectedSubnetID0            = ids.GenerateTestID()
		expectedSubnetID1            = ids.GenerateTestID()

		m                      = NewManager()
		managerAddCallCount    = 0
		managerChangeCallCount = 0
		setAddCallCount        = 0
		setChangeCallCount     = 0
	)

	require.NoError(m.AddStaker(expectedSubnetID0, expectedNodeID, expectedPK, expectedTxID, expectedOldWeight))

	m.RegisterCallbackListener(&managerCallbackListener{
		t: t,
		onAdd: func(subnetID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
			require.Contains([]ids.ID{expectedSubnetID0, expectedSubnetID1}, subnetID)
			require.Equal(expectedNodeID, nodeID)
			require.Equal(expectedPK, pk)
			require.Equal(expectedTxID, txID)
			require.Equal(expectedOldWeight, weight)
			managerAddCallCount++
		},
		onWeight: func(subnetID ids.ID, nodeID ids.NodeID, oldWeight, newWeight uint64) {
			require.Contains([]ids.ID{expectedSubnetID0, expectedSubnetID1}, subnetID)
			require.Equal(expectedNodeID, nodeID)
			require.Equal(expectedOldWeight, oldWeight)
			require.Equal(expectedNewWeight, newWeight)
			managerChangeCallCount++
		},
	})
	m.RegisterSetCallbackListener(expectedSubnetID0, &setCallbackListener{
		t: t,
		onAdd: func(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
			require.Equal(expectedNodeID, nodeID)
			require.Equal(expectedPK, pk)
			require.Equal(expectedTxID, txID)
			require.Equal(expectedOldWeight, weight)
			setAddCallCount++
		},
		onWeight: func(nodeID ids.NodeID, oldWeight, newWeight uint64) {
			require.Equal(expectedNodeID, nodeID)
			require.Equal(expectedOldWeight, oldWeight)
			require.Equal(expectedNewWeight, newWeight)
			setChangeCallCount++
		},
	})
	require.Equal(1, managerAddCallCount)
	require.Zero(managerChangeCallCount)
	require.Equal(1, setAddCallCount)
	require.Zero(setChangeCallCount)

	require.NoError(m.RemoveWeight(expectedSubnetID0, expectedNodeID, expectedRemovedWeight))
	require.Equal(1, managerAddCallCount)
	require.Equal(1, managerChangeCallCount)
	require.Equal(1, setAddCallCount)
	require.Equal(1, setChangeCallCount)

	require.NoError(m.AddStaker(expectedSubnetID1, expectedNodeID, expectedPK, expectedTxID, expectedOldWeight))
	require.Equal(2, managerAddCallCount)
	require.Equal(1, managerChangeCallCount)
	require.Equal(1, setAddCallCount)
	require.Equal(1, setChangeCallCount)

	require.NoError(m.RemoveWeight(expectedSubnetID1, expectedNodeID, expectedRemovedWeight))
	require.Equal(2, managerAddCallCount)
	require.Equal(2, managerChangeCallCount)
	require.Equal(1, setAddCallCount)
	require.Equal(1, setChangeCallCount)
}

func TestRemoveCallback(t *testing.T) {
	require := require.New(t)

	expectedSK, err := localsigner.New()
	require.NoError(err)

	var (
		expectedNodeID           = ids.GenerateTestNodeID()
		expectedPK               = expectedSK.PublicKey()
		expectedTxID             = ids.GenerateTestID()
		expectedWeight    uint64 = 1
		expectedSubnetID0        = ids.GenerateTestID()
		expectedSubnetID1        = ids.GenerateTestID()

		m                      = NewManager()
		managerAddCallCount    = 0
		managerRemoveCallCount = 0
		setAddCallCount        = 0
		setRemoveCallCount     = 0
	)

	require.NoError(m.AddStaker(expectedSubnetID0, expectedNodeID, expectedPK, expectedTxID, expectedWeight))

	m.RegisterCallbackListener(&managerCallbackListener{
		t: t,
		onAdd: func(subnetID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
			require.Contains([]ids.ID{expectedSubnetID0, expectedSubnetID1}, subnetID)
			require.Equal(expectedNodeID, nodeID)
			require.Equal(expectedPK, pk)
			require.Equal(expectedTxID, txID)
			require.Equal(expectedWeight, weight)
			managerAddCallCount++
		},
		onRemoved: func(subnetID ids.ID, nodeID ids.NodeID, weight uint64) {
			require.Contains([]ids.ID{expectedSubnetID0, expectedSubnetID1}, subnetID)
			require.Equal(expectedNodeID, nodeID)
			require.Equal(expectedWeight, weight)
			managerRemoveCallCount++
		},
	})
	m.RegisterSetCallbackListener(expectedSubnetID0, &setCallbackListener{
		t: t,
		onAdd: func(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
			require.Equal(expectedNodeID, nodeID)
			require.Equal(expectedPK, pk)
			require.Equal(expectedTxID, txID)
			require.Equal(expectedWeight, weight)
			setAddCallCount++
		},
		onRemoved: func(nodeID ids.NodeID, weight uint64) {
			require.Equal(expectedNodeID, nodeID)
			require.Equal(expectedWeight, weight)
			setRemoveCallCount++
		},
	})
	require.Equal(1, managerAddCallCount)
	require.Zero(managerRemoveCallCount)
	require.Equal(1, setAddCallCount)
	require.Zero(setRemoveCallCount)

	require.NoError(m.RemoveWeight(expectedSubnetID0, expectedNodeID, expectedWeight))
	require.Equal(1, managerAddCallCount)
	require.Equal(1, managerRemoveCallCount)
	require.Equal(1, setAddCallCount)
	require.Equal(1, setRemoveCallCount)

	require.NoError(m.AddStaker(expectedSubnetID1, expectedNodeID, expectedPK, expectedTxID, expectedWeight))
	require.Equal(2, managerAddCallCount)
	require.Equal(1, managerRemoveCallCount)
	require.Equal(1, setAddCallCount)
	require.Equal(1, setRemoveCallCount)

	require.NoError(m.RemoveWeight(expectedSubnetID1, expectedNodeID, expectedWeight))
	require.Equal(2, managerAddCallCount)
	require.Equal(2, managerRemoveCallCount)
	require.Equal(1, setAddCallCount)
	require.Equal(1, setRemoveCallCount)
}

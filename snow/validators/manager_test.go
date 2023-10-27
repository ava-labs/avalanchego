// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/set"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

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

	sk, err := bls.NewSecretKey()
	require.NoError(err)

	pk := bls.PublicFromSecretKey(sk)
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

func TestLen(t *testing.T) {
	require := require.New(t)

	m := NewManager()
	subnetID := ids.GenerateTestID()

	len := m.Count(subnetID)
	require.Zero(len)

	nodeID0 := ids.GenerateTestNodeID()
	require.NoError(m.AddStaker(subnetID, nodeID0, nil, ids.Empty, 1))

	len = m.Count(subnetID)
	require.Equal(1, len)

	nodeID1 := ids.GenerateTestNodeID()
	require.NoError(m.AddStaker(subnetID, nodeID1, nil, ids.Empty, 1))

	len = m.Count(subnetID)
	require.Equal(2, len)

	require.NoError(m.RemoveWeight(subnetID, nodeID1, 1))

	len = m.Count(subnetID)
	require.Equal(1, len)

	require.NoError(m.RemoveWeight(subnetID, nodeID0, 1))

	len = m.Count(subnetID)
	require.Zero(len)
}

func TestGetMap(t *testing.T) {
	require := require.New(t)

	m := NewManager()
	subnetID := ids.GenerateTestID()

	mp := m.GetMap(subnetID)
	require.Empty(mp)

	sk, err := bls.NewSecretKey()
	require.NoError(err)

	pk := bls.PublicFromSecretKey(sk)
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

func TestWeight(t *testing.T) {
	require := require.New(t)

	vdr0 := ids.NodeID{1}
	weight0 := uint64(93)
	vdr1 := ids.NodeID{2}
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

	sk, err := bls.NewSecretKey()
	require.NoError(err)

	nodeID0 := ids.GenerateTestNodeID()
	pk := bls.PublicFromSecretKey(sk)
	require.NoError(m.AddStaker(subnetID, nodeID0, pk, ids.Empty, 1))

	sampled, err = m.Sample(subnetID, 1)
	require.NoError(err)
	require.Equal([]ids.NodeID{nodeID0}, sampled)

	_, err = m.Sample(subnetID, 2)
	require.ErrorIs(err, sampler.ErrOutOfRange)

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

	expected := "Validator Manager: (Size = 2)\n" +
		"    Subnet[TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES]: Validator Set: (Size = 2, Weight = 9223372036854775807)\n" +
		"        Validator[0]: NodeID-111111111111111111116DBWJs, 1\n" +
		"        Validator[1]: NodeID-QLbz7JHiBTspS962RLKV8GndWFwdYhk6V, 9223372036854775806\n" +
		"    Subnet[2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w]: Validator Set: (Size = 1, Weight = 1)\n" +
		"        Validator[0]: NodeID-QLbz7JHiBTspS962RLKV8GndWFwdYhk6V, 1"
	result := m.String()
	require.Equal(expected, result)
}

func TestAddCallback(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.NodeID{1}
	sk0, err := bls.NewSecretKey()
	require.NoError(err)
	pk0 := bls.PublicFromSecretKey(sk0)
	txID0 := ids.GenerateTestID()
	weight0 := uint64(1)

	m := NewManager()
	subnetID := ids.GenerateTestID()
	callCount := 0
	m.RegisterCallbackListener(subnetID, &callbackListener{
		t: t,
		onAdd: func(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
			require.Equal(nodeID0, nodeID)
			require.Equal(pk0, pk)
			require.Equal(txID0, txID)
			require.Equal(weight0, weight)
			callCount++
		},
	})
	require.NoError(m.AddStaker(subnetID, nodeID0, pk0, txID0, weight0))
	// setup another subnetID
	subnetID2 := ids.GenerateTestID()
	require.NoError(m.AddStaker(subnetID2, nodeID0, nil, txID0, weight0))
	// should not be called for subnetID2
	require.Equal(1, callCount)
}

func TestAddWeightCallback(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.NodeID{1}
	txID0 := ids.GenerateTestID()
	weight0 := uint64(1)
	weight1 := uint64(93)

	m := NewManager()
	subnetID := ids.GenerateTestID()
	require.NoError(m.AddStaker(subnetID, nodeID0, nil, txID0, weight0))

	callCount := 0
	m.RegisterCallbackListener(subnetID, &callbackListener{
		t: t,
		onAdd: func(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
			require.Equal(nodeID0, nodeID)
			require.Nil(pk)
			require.Equal(txID0, txID)
			require.Equal(weight0, weight)
			callCount++
		},
		onWeight: func(nodeID ids.NodeID, oldWeight, newWeight uint64) {
			require.Equal(nodeID0, nodeID)
			require.Equal(weight0, oldWeight)
			require.Equal(weight0+weight1, newWeight)
			callCount++
		},
	})
	require.NoError(m.AddWeight(subnetID, nodeID0, weight1))
	// setup another subnetID
	subnetID2 := ids.GenerateTestID()
	require.NoError(m.AddStaker(subnetID2, nodeID0, nil, txID0, weight0))
	require.NoError(m.AddWeight(subnetID2, nodeID0, weight1))
	// should not be called for subnetID2
	require.Equal(2, callCount)
}

func TestRemoveWeightCallback(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.NodeID{1}
	txID0 := ids.GenerateTestID()
	weight0 := uint64(93)
	weight1 := uint64(92)

	m := NewManager()
	subnetID := ids.GenerateTestID()
	require.NoError(m.AddStaker(subnetID, nodeID0, nil, txID0, weight0))

	callCount := 0
	m.RegisterCallbackListener(subnetID, &callbackListener{
		t: t,
		onAdd: func(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
			require.Equal(nodeID0, nodeID)
			require.Nil(pk)
			require.Equal(txID0, txID)
			require.Equal(weight0, weight)
			callCount++
		},
		onWeight: func(nodeID ids.NodeID, oldWeight, newWeight uint64) {
			require.Equal(nodeID0, nodeID)
			require.Equal(weight0, oldWeight)
			require.Equal(weight0-weight1, newWeight)
			callCount++
		},
	})
	require.NoError(m.RemoveWeight(subnetID, nodeID0, weight1))
	// setup another subnetID
	subnetID2 := ids.GenerateTestID()
	require.NoError(m.AddStaker(subnetID2, nodeID0, nil, txID0, weight0))
	require.NoError(m.RemoveWeight(subnetID2, nodeID0, weight1))
	// should not be called for subnetID2
	require.Equal(2, callCount)
}

func TestValidatorRemovedCallback(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.NodeID{1}
	txID0 := ids.GenerateTestID()
	weight0 := uint64(93)

	m := NewManager()
	subnetID := ids.GenerateTestID()
	require.NoError(m.AddStaker(subnetID, nodeID0, nil, txID0, weight0))

	callCount := 0
	m.RegisterCallbackListener(subnetID, &callbackListener{
		t: t,
		onAdd: func(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
			require.Equal(nodeID0, nodeID)
			require.Nil(pk)
			require.Equal(txID0, txID)
			require.Equal(weight0, weight)
			callCount++
		},
		onRemoved: func(nodeID ids.NodeID, weight uint64) {
			require.Equal(nodeID0, nodeID)
			require.Equal(weight0, weight)
			callCount++
		},
	})
	require.NoError(m.RemoveWeight(subnetID, nodeID0, weight0))
	// setup another subnetID
	subnetID2 := ids.GenerateTestID()
	require.NoError(m.AddStaker(subnetID2, nodeID0, nil, txID0, weight0))
	require.NoError(m.AddWeight(subnetID2, nodeID0, weight0))
	// should not be called for subnetID2
	require.Equal(2, callCount)
}

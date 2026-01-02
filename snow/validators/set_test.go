// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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

var _ SetCallbackListener = (*setCallbackListener)(nil)

type setCallbackListener struct {
	t         *testing.T
	onAdd     func(ids.NodeID, *bls.PublicKey, ids.ID, uint64)
	onWeight  func(ids.NodeID, uint64, uint64)
	onRemoved func(ids.NodeID, uint64)
}

func (c *setCallbackListener) OnValidatorAdded(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
	if c.onAdd != nil {
		c.onAdd(nodeID, pk, txID, weight)
	} else {
		c.t.Fail()
	}
}

func (c *setCallbackListener) OnValidatorRemoved(nodeID ids.NodeID, weight uint64) {
	if c.onRemoved != nil {
		c.onRemoved(nodeID, weight)
	} else {
		c.t.Fail()
	}
}

func (c *setCallbackListener) OnValidatorWeightChanged(nodeID ids.NodeID, oldWeight, newWeight uint64) {
	if c.onWeight != nil {
		c.onWeight(nodeID, oldWeight, newWeight)
	} else {
		c.t.Fail()
	}
}

func TestSetAddDuplicate(t *testing.T) {
	require := require.New(t)

	s := newSet(ids.Empty, nil)

	nodeID := ids.GenerateTestNodeID()
	require.NoError(s.Add(nodeID, nil, ids.Empty, 1))

	err := s.Add(nodeID, nil, ids.Empty, 1)
	require.ErrorIs(err, errDuplicateValidator)
}

func TestSetAddOverflow(t *testing.T) {
	require := require.New(t)

	s := newSet(ids.Empty, nil)
	require.NoError(s.Add(ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	require.NoError(s.Add(ids.GenerateTestNodeID(), nil, ids.Empty, math.MaxUint64))

	_, err := s.TotalWeight()
	require.ErrorIs(err, errTotalWeightNotUint64)
}

func TestSetAddWeightOverflow(t *testing.T) {
	require := require.New(t)

	s := newSet(ids.Empty, nil)

	require.NoError(s.Add(ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	nodeID := ids.GenerateTestNodeID()
	require.NoError(s.Add(nodeID, nil, ids.Empty, 1))

	require.NoError(s.AddWeight(nodeID, math.MaxUint64-1))

	_, err := s.TotalWeight()
	require.ErrorIs(err, errTotalWeightNotUint64)
}

func TestSetGetWeight(t *testing.T) {
	require := require.New(t)

	s := newSet(ids.Empty, nil)

	nodeID := ids.GenerateTestNodeID()
	require.Zero(s.GetWeight(nodeID))

	require.NoError(s.Add(nodeID, nil, ids.Empty, 1))

	require.Equal(uint64(1), s.GetWeight(nodeID))
}

func TestSetSubsetWeight(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()

	weight0 := uint64(93)
	weight1 := uint64(123)
	weight2 := uint64(810)

	subset := set.Of(nodeID0, nodeID1)

	s := newSet(ids.Empty, nil)

	require.NoError(s.Add(nodeID0, nil, ids.Empty, weight0))
	require.NoError(s.Add(nodeID1, nil, ids.Empty, weight1))
	require.NoError(s.Add(nodeID2, nil, ids.Empty, weight2))

	expectedWeight := weight0 + weight1
	subsetWeight, err := s.SubsetWeight(subset)
	require.NoError(err)
	require.Equal(expectedWeight, subsetWeight)
}

func TestSetRemoveWeightMissingValidator(t *testing.T) {
	require := require.New(t)

	s := newSet(ids.Empty, nil)

	require.NoError(s.Add(ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	err := s.RemoveWeight(ids.GenerateTestNodeID(), 1)
	require.ErrorIs(err, errMissingValidator)
}

func TestSetRemoveWeightUnderflow(t *testing.T) {
	require := require.New(t)

	s := newSet(ids.Empty, nil)

	require.NoError(s.Add(ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	nodeID := ids.GenerateTestNodeID()
	require.NoError(s.Add(nodeID, nil, ids.Empty, 1))

	err := s.RemoveWeight(nodeID, 2)
	require.ErrorIs(err, safemath.ErrUnderflow)

	totalWeight, err := s.TotalWeight()
	require.NoError(err)
	require.Equal(uint64(2), totalWeight)
}

func TestSetGet(t *testing.T) {
	require := require.New(t)

	s := newSet(ids.Empty, nil)

	nodeID := ids.GenerateTestNodeID()
	_, ok := s.Get(nodeID)
	require.False(ok)

	sk, err := localsigner.New()
	require.NoError(err)

	pk := sk.PublicKey()
	require.NoError(s.Add(nodeID, pk, ids.Empty, 1))

	vdr0, ok := s.Get(nodeID)
	require.True(ok)
	require.Equal(nodeID, vdr0.NodeID)
	require.Equal(pk, vdr0.PublicKey)
	require.Equal(uint64(1), vdr0.Weight)

	require.NoError(s.AddWeight(nodeID, 1))

	vdr1, ok := s.Get(nodeID)
	require.True(ok)
	require.Equal(nodeID, vdr0.NodeID)
	require.Equal(pk, vdr0.PublicKey)
	require.Equal(uint64(1), vdr0.Weight)
	require.Equal(nodeID, vdr1.NodeID)
	require.Equal(pk, vdr1.PublicKey)
	require.Equal(uint64(2), vdr1.Weight)

	require.NoError(s.RemoveWeight(nodeID, 2))
	_, ok = s.Get(nodeID)
	require.False(ok)
}

func TestSetLen(t *testing.T) {
	require := require.New(t)

	s := newSet(ids.Empty, nil)

	setLen := s.Len()
	require.Zero(setLen)

	nodeID0 := ids.GenerateTestNodeID()
	require.NoError(s.Add(nodeID0, nil, ids.Empty, 1))

	setLen = s.Len()
	require.Equal(1, setLen)

	nodeID1 := ids.GenerateTestNodeID()
	require.NoError(s.Add(nodeID1, nil, ids.Empty, 1))

	setLen = s.Len()
	require.Equal(2, setLen)

	require.NoError(s.RemoveWeight(nodeID1, 1))

	setLen = s.Len()
	require.Equal(1, setLen)

	require.NoError(s.RemoveWeight(nodeID0, 1))

	setLen = s.Len()
	require.Zero(setLen)
}

func TestSetMap(t *testing.T) {
	require := require.New(t)

	s := newSet(ids.Empty, nil)

	m := s.Map()
	require.Empty(m)

	sk, err := localsigner.New()
	require.NoError(err)

	pk := sk.PublicKey()
	nodeID0 := ids.GenerateTestNodeID()
	require.NoError(s.Add(nodeID0, pk, ids.Empty, 2))

	m = s.Map()
	require.Len(m, 1)
	require.Contains(m, nodeID0)

	node0 := m[nodeID0]
	require.Equal(nodeID0, node0.NodeID)
	require.Equal(pk, node0.PublicKey)
	require.Equal(uint64(2), node0.Weight)

	nodeID1 := ids.GenerateTestNodeID()
	require.NoError(s.Add(nodeID1, nil, ids.Empty, 1))

	m = s.Map()
	require.Len(m, 2)
	require.Contains(m, nodeID0)
	require.Contains(m, nodeID1)

	node0 = m[nodeID0]
	require.Equal(nodeID0, node0.NodeID)
	require.Equal(pk, node0.PublicKey)
	require.Equal(uint64(2), node0.Weight)

	node1 := m[nodeID1]
	require.Equal(nodeID1, node1.NodeID)
	require.Nil(node1.PublicKey)
	require.Equal(uint64(1), node1.Weight)

	require.NoError(s.RemoveWeight(nodeID0, 1))
	require.Equal(nodeID0, node0.NodeID)
	require.Equal(pk, node0.PublicKey)
	require.Equal(uint64(2), node0.Weight)

	m = s.Map()
	require.Len(m, 2)
	require.Contains(m, nodeID0)
	require.Contains(m, nodeID1)

	node0 = m[nodeID0]
	require.Equal(nodeID0, node0.NodeID)
	require.Equal(pk, node0.PublicKey)
	require.Equal(uint64(1), node0.Weight)

	node1 = m[nodeID1]
	require.Equal(nodeID1, node1.NodeID)
	require.Nil(node1.PublicKey)
	require.Equal(uint64(1), node1.Weight)

	require.NoError(s.RemoveWeight(nodeID0, 1))

	m = s.Map()
	require.Len(m, 1)
	require.Contains(m, nodeID1)

	node1 = m[nodeID1]
	require.Equal(nodeID1, node1.NodeID)
	require.Nil(node1.PublicKey)
	require.Equal(uint64(1), node1.Weight)

	require.NoError(s.RemoveWeight(nodeID1, 1))

	require.Empty(s.Map())
}

func TestSetWeight(t *testing.T) {
	require := require.New(t)

	vdr0 := ids.BuildTestNodeID([]byte{1})
	weight0 := uint64(93)
	vdr1 := ids.BuildTestNodeID([]byte{2})
	weight1 := uint64(123)

	s := newSet(ids.Empty, nil)
	require.NoError(s.Add(vdr0, nil, ids.Empty, weight0))

	require.NoError(s.Add(vdr1, nil, ids.Empty, weight1))

	setWeight, err := s.TotalWeight()
	require.NoError(err)
	expectedWeight := weight0 + weight1
	require.Equal(expectedWeight, setWeight)
}

func TestSetSample(t *testing.T) {
	require := require.New(t)

	s := newSet(ids.Empty, nil)

	sampled, err := s.Sample(0)
	require.NoError(err)
	require.Empty(sampled)

	sk, err := localsigner.New()
	require.NoError(err)

	nodeID0 := ids.GenerateTestNodeID()
	pk := sk.PublicKey()
	require.NoError(s.Add(nodeID0, pk, ids.Empty, 1))

	sampled, err = s.Sample(1)
	require.NoError(err)
	require.Equal([]ids.NodeID{nodeID0}, sampled)

	_, err = s.Sample(2)
	require.ErrorIs(err, errInsufficientWeight)

	nodeID1 := ids.GenerateTestNodeID()
	require.NoError(s.Add(nodeID1, nil, ids.Empty, math.MaxInt64-1))

	sampled, err = s.Sample(1)
	require.NoError(err)
	require.Equal([]ids.NodeID{nodeID1}, sampled)

	sampled, err = s.Sample(2)
	require.NoError(err)
	require.Equal([]ids.NodeID{nodeID1, nodeID1}, sampled)

	sampled, err = s.Sample(3)
	require.NoError(err)
	require.Equal([]ids.NodeID{nodeID1, nodeID1, nodeID1}, sampled)
}

func TestSetString(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.EmptyNodeID
	nodeID1 := ids.BuildTestNodeID([]byte{
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	})

	s := newSet(ids.Empty, nil)
	require.NoError(s.Add(nodeID0, nil, ids.Empty, 1))

	require.NoError(s.Add(nodeID1, nil, ids.Empty, math.MaxInt64-1))

	expected := `Validator Set: (Size = 2, Weight = 9223372036854775807)
    Validator[0]: NodeID-111111111111111111116DBWJs, 1
    Validator[1]: NodeID-QLbz7JHiBTspS962RLKV8GndWFwdYhk6V, 9223372036854775806`
	result := s.String()
	require.Equal(expected, result)
}

func TestSetAddCallback(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.BuildTestNodeID([]byte{1})
	sk0, err := localsigner.New()
	require.NoError(err)
	pk0 := sk0.PublicKey()
	txID0 := ids.GenerateTestID()
	weight0 := uint64(1)

	s := newSet(ids.Empty, nil)
	callCount := 0
	require.False(s.HasCallbackRegistered())
	s.RegisterCallbackListener(&setCallbackListener{
		t: t,
		onAdd: func(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
			require.Equal(nodeID0, nodeID)
			require.Equal(pk0, pk)
			require.Equal(txID0, txID)
			require.Equal(weight0, weight)
			callCount++
		},
	})
	require.True(s.HasCallbackRegistered())
	require.NoError(s.Add(nodeID0, pk0, txID0, weight0))
	require.Equal(1, callCount)
}

func TestSetAddWeightCallback(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.BuildTestNodeID([]byte{1})
	txID0 := ids.GenerateTestID()
	weight0 := uint64(1)
	weight1 := uint64(93)

	s := newSet(ids.Empty, nil)
	require.NoError(s.Add(nodeID0, nil, txID0, weight0))

	callCount := 0
	require.False(s.HasCallbackRegistered())
	s.RegisterCallbackListener(&setCallbackListener{
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
	require.True(s.HasCallbackRegistered())
	require.NoError(s.AddWeight(nodeID0, weight1))
	require.Equal(2, callCount)
}

func TestSetRemoveWeightCallback(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.BuildTestNodeID([]byte{1})
	txID0 := ids.GenerateTestID()
	weight0 := uint64(93)
	weight1 := uint64(92)

	s := newSet(ids.Empty, nil)
	require.NoError(s.Add(nodeID0, nil, txID0, weight0))

	callCount := 0
	require.False(s.HasCallbackRegistered())
	s.RegisterCallbackListener(&setCallbackListener{
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
	require.True(s.HasCallbackRegistered())
	require.NoError(s.RemoveWeight(nodeID0, weight1))
	require.Equal(2, callCount)
}

func TestSetValidatorRemovedCallback(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.BuildTestNodeID([]byte{1})
	txID0 := ids.GenerateTestID()
	weight0 := uint64(93)

	s := newSet(ids.Empty, nil)
	require.NoError(s.Add(nodeID0, nil, txID0, weight0))

	callCount := 0
	require.False(s.HasCallbackRegistered())
	s.RegisterCallbackListener(&setCallbackListener{
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
	require.True(s.HasCallbackRegistered())
	require.NoError(s.RemoveWeight(nodeID0, weight0))
	require.Equal(2, callCount)
}

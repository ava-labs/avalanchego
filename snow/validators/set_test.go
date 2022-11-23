// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestSamplerSample(t *testing.T) {
	vdr0 := ids.GenerateTestNodeID()
	vdr1 := ids.GenerateTestNodeID()

	s := NewSet()
	err := s.AddWeight(vdr0, 1)
	require.NoError(t, err)

	sampled, err := s.Sample(1)
	require.NoError(t, err)
	require.Len(t, sampled, 1, "should have only sampled one validator")
	require.Equal(t, vdr0, sampled[0].ID(), "should have sampled vdr0")

	_, err = s.Sample(2)
	require.Error(t, err, "should have errored during sampling")

	err = s.AddWeight(vdr1, math.MaxInt64-1)
	require.NoError(t, err)

	sampled, err = s.Sample(1)
	require.NoError(t, err)
	require.Len(t, sampled, 1, "should have only sampled one validator")
	require.Equal(t, vdr1, sampled[0].ID(), "should have sampled vdr1")

	sampled, err = s.Sample(2)
	require.NoError(t, err)
	require.Len(t, sampled, 2, "should have sampled two validators")
	require.Equal(t, vdr1, sampled[0].ID(), "should have sampled vdr1")
	require.Equal(t, vdr1, sampled[1].ID(), "should have sampled vdr1")

	sampled, err = s.Sample(3)
	require.NoError(t, err)
	require.Len(t, sampled, 3, "should have sampled three validators")
	require.Equal(t, vdr1, sampled[0].ID(), "should have sampled vdr1")
	require.Equal(t, vdr1, sampled[1].ID(), "should have sampled vdr1")
	require.Equal(t, vdr1, sampled[2].ID(), "should have sampled vdr1")
}

func TestSamplerDuplicate(t *testing.T) {
	vdr0 := ids.GenerateTestNodeID()
	vdr1 := ids.GenerateTestNodeID()

	s := NewSet()
	err := s.AddWeight(vdr0, 1)
	require.NoError(t, err)

	err = s.AddWeight(vdr1, 1)
	require.NoError(t, err)

	err = s.AddWeight(vdr1, math.MaxInt64-2)
	require.NoError(t, err)

	sampled, err := s.Sample(1)
	require.NoError(t, err)
	require.Len(t, sampled, 1, "should have only sampled one validator")
	require.Equal(t, vdr1, sampled[0].ID(), "should have sampled vdr1")
}

func TestSamplerContains(t *testing.T) {
	vdr := ids.GenerateTestNodeID()

	s := NewSet()
	err := s.AddWeight(vdr, 1)
	require.NoError(t, err)

	contains := s.Contains(vdr)
	require.True(t, contains, "should have contained validator")

	err = s.RemoveWeight(vdr, 1)
	require.NoError(t, err)

	contains = s.Contains(vdr)
	require.False(t, contains, "shouldn't have contained validator")
}

func TestSamplerString(t *testing.T) {
	vdr0 := ids.EmptyNodeID
	vdr1 := ids.NodeID{
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	}

	s := NewSet()
	err := s.AddWeight(vdr0, 1)
	require.NoError(t, err)

	err = s.AddWeight(vdr1, math.MaxInt64-1)
	require.NoError(t, err)

	expected := "Validator Set: (Size = 2, Weight = 9223372036854775807)\n" +
		"    Validator[0]: NodeID-111111111111111111116DBWJs, 1\n" +
		"    Validator[1]: NodeID-QLbz7JHiBTspS962RLKV8GndWFwdYhk6V, 9223372036854775806"
	result := s.String()
	require.Equal(t, expected, result, "wrong string returned")
}

func TestSetWeight(t *testing.T) {
	vdr0 := ids.NodeID{1}
	weight0 := uint64(93)
	vdr1 := ids.NodeID{2}
	weight1 := uint64(123)

	s := NewSet()
	err := s.AddWeight(vdr0, weight0)
	require.NoError(t, err)

	err = s.AddWeight(vdr1, weight1)
	require.NoError(t, err)

	setWeight := s.Weight()
	expectedWeight := weight0 + weight1
	require.Equal(t, expectedWeight, setWeight, "wrong set weight")
}

func TestSetSubsetWeight(t *testing.T) {
	vdr0 := ids.NodeID{1}
	weight0 := uint64(93)
	vdr1 := ids.NodeID{2}
	weight1 := uint64(123)
	vdr2 := ids.NodeID{3}
	weight2 := uint64(810)
	subset := ids.NodeIDSet{}
	subset.Add(vdr0)
	subset.Add(vdr1)

	s := NewSet()
	err := s.AddWeight(vdr0, weight0)
	require.NoError(t, err)

	err = s.AddWeight(vdr1, weight1)
	require.NoError(t, err)
	err = s.AddWeight(vdr2, weight2)
	require.NoError(t, err)

	subsetWeight, err := s.SubsetWeight(subset)
	if err != nil {
		t.Fatal(err)
	}
	expectedWeight := weight0 + weight1
	require.Equal(t, expectedWeight, subsetWeight, "wrong subset weight")
}

var _ SetCallbackListener = (*callbackListener)(nil)

type callbackListener struct {
	t         *testing.T
	onWeight  func(ids.NodeID, uint64, uint64)
	onAdd     func(ids.NodeID, uint64)
	onRemoved func(ids.NodeID, uint64)
}

func (c *callbackListener) OnValidatorAdded(nodeID ids.NodeID, weight uint64) {
	if c.onAdd != nil {
		c.onAdd(nodeID, weight)
	} else {
		c.t.Fail()
	}
}

func (c *callbackListener) OnValidatorRemoved(nodeID ids.NodeID, weight uint64) {
	if c.onRemoved != nil {
		c.onRemoved(nodeID, weight)
	} else {
		c.t.Fail()
	}
}

func (c *callbackListener) OnValidatorWeightChanged(nodeID ids.NodeID, oldWeight, newWeight uint64) {
	if c.onWeight != nil {
		c.onWeight(nodeID, oldWeight, newWeight)
	} else {
		c.t.Fail()
	}
}

func TestSetAddWeightCallback(t *testing.T) {
	vdr0 := ids.NodeID{1}
	weight0 := uint64(1)
	weight1 := uint64(93)

	s := NewSet()
	err := s.AddWeight(vdr0, weight0)
	require.NoError(t, err)
	callcount := 0
	s.RegisterCallbackListener(&callbackListener{
		t: t,
		onAdd: func(nodeID ids.NodeID, weight uint64) {
			if nodeID == vdr0 {
				require.Equal(t, weight0, weight)
			}
			callcount++
		},
		onWeight: func(nodeID ids.NodeID, oldWeight, newWeight uint64) {
			require.Equal(t, vdr0, nodeID)
			require.Equal(t, weight0, oldWeight)
			require.Equal(t, weight0+weight1, newWeight)
			callcount++
		},
	})
	err = s.AddWeight(vdr0, weight1)
	require.NoError(t, err)
	require.Equal(t, 2, callcount)
}

func TestSetRemoveWeightCallback(t *testing.T) {
	vdr0 := ids.NodeID{1}
	weight0 := uint64(93)
	weight1 := uint64(92)

	s := NewSet()
	callcount := 0
	err := s.AddWeight(vdr0, weight0)
	require.NoError(t, err)
	s.RegisterCallbackListener(&callbackListener{
		t: t,
		onAdd: func(nodeID ids.NodeID, weight uint64) {
			if nodeID == vdr0 {
				require.Equal(t, weight0, weight)
			}
			callcount++
		},
		onWeight: func(nodeID ids.NodeID, oldWeight, newWeight uint64) {
			require.Equal(t, vdr0, nodeID)
			require.Equal(t, weight0, oldWeight)
			require.Equal(t, weight0-weight1, newWeight)
			callcount++
		},
	})
	err = s.RemoveWeight(vdr0, weight1)
	require.NoError(t, err)
	require.Equal(t, 2, callcount)
}

func TestSetValidatorRemovedCallback(t *testing.T) {
	vdr0 := ids.NodeID{1}
	weight0 := uint64(93)

	s := NewSet()
	callcount := 0
	err := s.AddWeight(vdr0, weight0)
	require.NoError(t, err)

	s.RegisterCallbackListener(&callbackListener{
		t: t,
		onAdd: func(nodeID ids.NodeID, weight uint64) {
			if nodeID == vdr0 {
				require.Equal(t, weight0, weight)
			}
			callcount++
		},
		onRemoved: func(nodeID ids.NodeID, weight uint64) {
			require.Equal(t, vdr0, nodeID)
			require.Equal(t, weight0, weight)
			callcount++
		},
	})
	err = s.RemoveWeight(vdr0, weight0)
	require.NoError(t, err)
	require.Equal(t, 2, callcount)
}

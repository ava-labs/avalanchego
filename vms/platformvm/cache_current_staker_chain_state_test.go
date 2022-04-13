// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils/constants"
)

func TestPrimaryValidatorSet(t *testing.T) {
	// Initialize the chain state
	nodeID0 := ids.GenerateTestShortID()
	node0Weight := uint64(1)
	vdr0 := &currentValidatorImpl{
		addValidatorTx: &UnsignedAddValidatorTx{
			Validator: Validator{
				Wght: node0Weight,
			},
		},
	}

	nodeID1 := ids.GenerateTestShortID()
	node1Weight := uint64(2)
	vdr1 := &currentValidatorImpl{
		addValidatorTx: &UnsignedAddValidatorTx{
			Validator: Validator{
				Wght: node1Weight,
			},
		},
	}

	nodeID2 := ids.GenerateTestShortID()
	node2Weight := uint64(2)
	vdr2 := &currentValidatorImpl{
		addValidatorTx: &UnsignedAddValidatorTx{
			Validator: Validator{
				Wght: node2Weight,
			},
		},
	}

	cs := &currentStakerChainStateImpl{
		validatorsByNodeID: map[ids.ShortID]*currentValidatorImpl{
			nodeID0: vdr0,
			nodeID1: vdr1,
			nodeID2: vdr2,
		},
	}
	nodeID3 := ids.GenerateTestShortID()

	{
		// Apply the on-chain validator set to [vdrs]
		vdrs, err := cs.ValidatorSet(constants.PrimaryNetworkID)
		assert.NoError(t, err)

		// Validate that the state was applied and the old state was cleared
		assert.EqualValues(t, 3, vdrs.Len())
		assert.EqualValues(t, node0Weight+node1Weight+node2Weight, vdrs.Weight())
		gotNode0Weight, exists := vdrs.GetWeight(nodeID0)
		assert.True(t, exists)
		assert.EqualValues(t, node0Weight, gotNode0Weight)
		gotNode1Weight, exists := vdrs.GetWeight(nodeID1)
		assert.True(t, exists)
		assert.EqualValues(t, node1Weight, gotNode1Weight)
		gotNode2Weight, exists := vdrs.GetWeight(nodeID2)
		assert.True(t, exists)
		assert.EqualValues(t, node2Weight, gotNode2Weight)
		_, exists = vdrs.GetWeight(nodeID3)
		assert.False(t, exists)
	}

	{
		// Apply the on-chain validator set again
		vdrs, err := cs.ValidatorSet(constants.PrimaryNetworkID)
		assert.NoError(t, err)

		// The state should be the same
		assert.EqualValues(t, 3, vdrs.Len())
		assert.EqualValues(t, node0Weight+node1Weight+node2Weight, vdrs.Weight())
		gotNode0Weight, exists := vdrs.GetWeight(nodeID0)
		assert.True(t, exists)
		assert.EqualValues(t, node0Weight, gotNode0Weight)
		gotNode1Weight, exists := vdrs.GetWeight(nodeID1)
		assert.True(t, exists)
		assert.EqualValues(t, node1Weight, gotNode1Weight)
		gotNode2Weight, exists := vdrs.GetWeight(nodeID2)
		assert.True(t, exists)
		assert.EqualValues(t, node2Weight, gotNode2Weight)
	}
}

func TestSubnetValidatorSet(t *testing.T) {
	subnetID := ids.GenerateTestID()

	// Initialize the chain state
	nodeID0 := ids.GenerateTestShortID()
	node0Weight := uint64(1)
	vdr0 := &currentValidatorImpl{
		validatorImpl: validatorImpl{
			subnets: map[ids.ID]*UnsignedAddSubnetValidatorTx{
				subnetID: {
					Validator: SubnetValidator{
						Validator: Validator{
							Wght: node0Weight,
						},
					},
				},
			},
		},
	}

	nodeID1 := ids.GenerateTestShortID()
	node1Weight := uint64(2)
	vdr1 := &currentValidatorImpl{
		validatorImpl: validatorImpl{
			subnets: map[ids.ID]*UnsignedAddSubnetValidatorTx{
				subnetID: {
					Validator: SubnetValidator{
						Validator: Validator{
							Wght: node1Weight,
						},
					},
				},
			},
		},
	}

	nodeID2 := ids.GenerateTestShortID()
	node2Weight := uint64(2)
	vdr2 := &currentValidatorImpl{
		validatorImpl: validatorImpl{
			subnets: map[ids.ID]*UnsignedAddSubnetValidatorTx{
				subnetID: {
					Validator: SubnetValidator{
						Validator: Validator{
							Wght: node2Weight,
						},
					},
				},
			},
		},
	}

	cs := &currentStakerChainStateImpl{
		validatorsByNodeID: map[ids.ShortID]*currentValidatorImpl{
			nodeID0: vdr0,
			nodeID1: vdr1,
			nodeID2: vdr2,
		},
	}

	nodeID3 := ids.GenerateTestShortID()

	{
		// Apply the on-chain validator set to [vdrs]
		vdrs, err := cs.ValidatorSet(subnetID)
		assert.NoError(t, err)

		// Validate that the state was applied and the old state was cleared
		assert.EqualValues(t, 3, vdrs.Len())
		assert.EqualValues(t, node0Weight+node1Weight+node2Weight, vdrs.Weight())
		gotNode0Weight, exists := vdrs.GetWeight(nodeID0)
		assert.True(t, exists)
		assert.EqualValues(t, node0Weight, gotNode0Weight)
		gotNode1Weight, exists := vdrs.GetWeight(nodeID1)
		assert.True(t, exists)
		assert.EqualValues(t, node1Weight, gotNode1Weight)
		gotNode2Weight, exists := vdrs.GetWeight(nodeID2)
		assert.True(t, exists)
		assert.EqualValues(t, node2Weight, gotNode2Weight)
		_, exists = vdrs.GetWeight(nodeID3)
		assert.False(t, exists)
	}

	{
		// Apply the on-chain validator set again
		vdrs, err := cs.ValidatorSet(subnetID)
		assert.NoError(t, err)

		// The state should be the same
		assert.EqualValues(t, 3, vdrs.Len())
		assert.EqualValues(t, node0Weight+node1Weight+node2Weight, vdrs.Weight())
		gotNode0Weight, exists := vdrs.GetWeight(nodeID0)
		assert.True(t, exists)
		assert.EqualValues(t, node0Weight, gotNode0Weight)
		gotNode1Weight, exists := vdrs.GetWeight(nodeID1)
		assert.True(t, exists)
		assert.EqualValues(t, node1Weight, gotNode1Weight)
		gotNode2Weight, exists := vdrs.GetWeight(nodeID2)
		assert.True(t, exists)
		assert.EqualValues(t, node2Weight, gotNode2Weight)
	}
}

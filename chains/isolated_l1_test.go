// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
)

const testIsolatedL1Height = 1234

// newTestIsolatedL1 returns an isolated L1 with two validators.
func newTestIsolatedL1(t *testing.T) *IsolatedL1 {
	vdrs := make(map[ids.NodeID]*validators.GetValidatorOutput)
	for _, weight := range []uint64{6, 4} {
		sk, err := localsigner.New()
		require.NoError(t, err)
		nodeID := ids.GenerateTestNodeID()
		vdrs[nodeID] = &validators.GetValidatorOutput{
			NodeID:    nodeID,
			PublicKey: sk.PublicKey(),
			Weight:    weight,
		}
	}
	return &IsolatedL1{
		Chain: ChainParameters{
			ID:       ids.GenerateTestID(),
			SubnetID: ids.GenerateTestID(),
		},
		Validators: vdrs,
		Height:     testIsolatedL1Height,
	}
}

func TestIsolatedL1FixedHeight(t *testing.T) {
	require := require.New(t)
	l1 := newTestIsolatedL1(t)

	minHeight, err := l1.GetMinimumHeight(t.Context())
	require.NoError(err)
	require.Equal(uint64(testIsolatedL1Height), minHeight)

	currentHeight, err := l1.GetCurrentHeight(t.Context())
	require.NoError(err)
	require.Equal(uint64(testIsolatedL1Height), currentHeight)

	// The validator set is the same at every height.
	for _, height := range []uint64{0, testIsolatedL1Height, testIsolatedL1Height + 1} {
		vdrs, err := l1.GetValidatorSet(t.Context(), height, l1.Chain.SubnetID)
		require.NoError(err)
		require.Equal(l1.Validators, vdrs)
	}
}

// TestIsolatedL1GetSubnetID tests that only the L1 itself may
// be found, so messages from any other chain are rejected.
func TestIsolatedL1GetSubnetID(t *testing.T) {
	l1 := newTestIsolatedL1(t)

	tests := []struct {
		name             string
		chainID          ids.ID
		expectedSubnetID ids.ID
		expectedErr      error
	}{
		{
			name:             "L1 chain",
			chainID:          l1.Chain.ID,
			expectedSubnetID: l1.Chain.SubnetID,
		},
		{
			name:        "P-chain",
			chainID:     constants.PlatformChainID,
			expectedErr: errUnknownChain,
		},
		{
			name:        "other chain",
			chainID:     ids.GenerateTestID(),
			expectedErr: errUnknownChain,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			subnetID, err := l1.GetSubnetID(t.Context(), test.chainID)
			require.ErrorIs(t, err, test.expectedErr)
			require.Equal(t, test.expectedSubnetID, subnetID)
		})
	}
}

// TestIsolatedL1OnlyServesL1Subnet ensures an isolated L1 does not return a
// validator set for the Primary Network.
func TestIsolatedL1OnlyServesL1Subnet(t *testing.T) {
	require := require.New(t)
	l1 := newTestIsolatedL1(t)

	_, err := l1.GetValidatorSet(t.Context(), testIsolatedL1Height, constants.PrimaryNetworkID)
	require.ErrorIs(err, errUnknownSubnet)

	_, _, err = l1.GetCurrentValidatorSet(t.Context(), constants.PrimaryNetworkID)
	require.ErrorIs(err, errUnknownSubnet)
}

func TestIsolatedL1WarpValidatorSetsDisabled(t *testing.T) {
	l1 := newTestIsolatedL1(t)

	_, err := l1.GetWarpValidatorSets(t.Context(), testIsolatedL1Height)
	require.ErrorIs(t, err, ErrWarpDisabled)
}

func TestIsolatedL1GetCurrentValidatorSet(t *testing.T) {
	require := require.New(t)
	l1 := newTestIsolatedL1(t)

	current, height, err := l1.GetCurrentValidatorSet(t.Context(), l1.Chain.SubnetID)
	require.NoError(err)
	require.Equal(uint64(testIsolatedL1Height), height)
	require.Len(current, len(l1.Validators))
	for validationID, vdr := range current {
		expected := l1.Validators[vdr.NodeID]
		require.Equal(validationID, vdr.ValidationID)
		require.Equal(expected.PublicKey, vdr.PublicKey)
		require.Equal(expected.Weight, vdr.Weight)
		require.True(vdr.IsActive)
	}
}

// TestIsolatedL1RegisterValidators ensures the L1 validators
// are added to the validator manager.
func TestIsolatedL1RegisterValidators(t *testing.T) {
	require := require.New(t)
	l1 := newTestIsolatedL1(t)
	vdrs := validators.NewManager()

	require.NoError(l1.RegisterValidators(vdrs))

	require.Equal(len(l1.Validators), vdrs.NumValidators(l1.Chain.SubnetID))
	for nodeID, vdr := range l1.Validators {
		require.Equal(vdr.Weight, vdrs.GetWeight(l1.Chain.SubnetID, nodeID))
	}
	require.Zero(vdrs.NumValidators(constants.PrimaryNetworkID))
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators_test

import (
	"bytes"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"

	. "github.com/ava-labs/avalanchego/vms/platformvm/validators"
)

func newPublicKey(t *testing.T) *bls.PublicKey {
	sk, err := localsigner.New()
	require.NoError(t, err)
	return sk.PublicKey()
}

func TestGetValidatorSet_AfterEtna(t *testing.T) {
	require := require.New(t)

	vdrs := validators.NewManager()
	upgrades := upgradetest.GetConfig(upgradetest.Durango)
	upgradeTime := genesistest.DefaultValidatorStartTime.Add(2 * time.Second)
	upgrades.EtnaTime = upgradeTime
	s := statetest.New(t, statetest.Config{
		Validators: vdrs,
		Upgrades:   upgrades,
	})

	var (
		subnetID      = ids.GenerateTestID()
		startTime     = genesistest.DefaultValidatorStartTime
		endTime       = startTime.Add(24 * time.Hour)
		pk            = newPublicKey(t)
		primaryStaker = &state.Staker{
			TxID:            ids.GenerateTestID(),
			NodeID:          ids.GenerateTestNodeID(),
			PublicKey:       pk,
			SubnetID:        constants.PrimaryNetworkID,
			Weight:          1,
			StartTime:       startTime,
			EndTime:         endTime,
			PotentialReward: 1,
		}
		subnetStaker = &state.Staker{
			TxID:      ids.GenerateTestID(),
			NodeID:    primaryStaker.NodeID,
			PublicKey: nil, // inherited from primaryStaker
			SubnetID:  subnetID,
			Weight:    1,
			StartTime: upgradeTime,
			EndTime:   endTime,
		}
	)

	// Add a subnet staker during the Etna upgrade
	{
		blk, err := block.NewBanffStandardBlock(upgradeTime, s.GetLastAccepted(), 1, nil)
		require.NoError(err)

		s.SetHeight(blk.Height())
		s.SetTimestamp(blk.Timestamp())
		s.AddStatelessBlock(blk)
		s.SetLastAccepted(blk.ID())

		require.NoError(s.PutCurrentValidator(primaryStaker))
		require.NoError(s.PutCurrentValidator(subnetStaker))

		require.NoError(s.Commit())
	}

	// Remove a subnet staker
	{
		blk, err := block.NewBanffStandardBlock(s.GetTimestamp(), s.GetLastAccepted(), 2, nil)
		require.NoError(err)

		s.SetHeight(blk.Height())
		s.SetTimestamp(blk.Timestamp())
		s.AddStatelessBlock(blk)
		s.SetLastAccepted(blk.ID())

		s.DeleteCurrentValidator(subnetStaker)

		require.NoError(s.Commit())
	}

	m := NewManager(
		config.Internal{
			Validators: vdrs,
		},
		s,
		metrics.Noop,
		new(mockable.Clock),
	)

	expectedValidators := []map[ids.NodeID]*validators.GetValidatorOutput{
		{}, // Subnet staker didn't exist at genesis
		{
			subnetStaker.NodeID: {
				NodeID:    subnetStaker.NodeID,
				PublicKey: pk,
				Weight:    subnetStaker.Weight,
			},
		}, // Subnet staker was added at height 1
		{}, // Subnet staker was removed at height 2
	}
	for height, expected := range expectedValidators {
		actual, err := m.GetValidatorSet(t.Context(), uint64(height), subnetID)
		require.NoError(err)
		require.Equal(expected, actual)
	}
}

func TestGetWarpValidatorSets(t *testing.T) {
	require := require.New(t)

	vdrs := validators.NewManager()
	s := statetest.New(t, statetest.Config{
		Validators: vdrs,
	})

	// Warp validators are sorted by their public key bytes, so define the
	// sorted order as 0 then 1.
	var (
		pk0      = newPublicKey(t)
		pk0Bytes = bls.PublicKeyToUncompressedBytes(pk0)
		pk1      = newPublicKey(t)
		pk1Bytes = bls.PublicKeyToUncompressedBytes(pk1)
	)
	if bytes.Compare(pk0Bytes, pk1Bytes) > 0 {
		pk0, pk1 = pk1, pk0
		pk0Bytes, pk1Bytes = pk1Bytes, pk0Bytes
	}

	var (
		subnetID       = ids.GenerateTestID()
		primaryStaker0 = &state.Staker{
			NodeID:    ids.GenerateTestNodeID(),
			PublicKey: pk0,
			SubnetID:  constants.PrimaryNetworkID,
			Weight:    1,
		}
		subnetStaker0 = &state.Staker{
			NodeID:    primaryStaker0.NodeID,
			PublicKey: nil, // inherited from primaryStaker
			SubnetID:  subnetID,
			Weight:    1,
		}
		primaryStaker1 = &state.Staker{
			NodeID:    ids.GenerateTestNodeID(),
			PublicKey: pk1,
			SubnetID:  constants.PrimaryNetworkID,
			Weight:    1,
		}
		subnetStaker1 = &state.Staker{
			NodeID:    primaryStaker1.NodeID,
			PublicKey: nil, // inherited from primaryStaker1
			SubnetID:  subnetID,
			Weight:    math.MaxUint64,
		}
	)

	var lastHeight uint64
	acceptBlock := func(
		newStakers []*state.Staker,
		removedStakers []*state.Staker,
	) {
		t.Helper()

		lastHeight++
		blk, err := block.NewBanffStandardBlock(s.GetTimestamp(), s.GetLastAccepted(), lastHeight, nil)
		require.NoError(err)

		s.SetHeight(blk.Height())
		s.SetTimestamp(blk.Timestamp())
		s.AddStatelessBlock(blk)
		s.SetLastAccepted(blk.ID())

		for _, v := range newStakers {
			require.NoError(s.PutCurrentValidator(v))
		}
		for _, v := range removedStakers {
			s.DeleteCurrentValidator(v)
		}
		require.NoError(s.Commit())
	}

	acceptBlock( // Add a subnet staker during the Etna upgrade
		[]*state.Staker{primaryStaker0, subnetStaker0},
		nil,
	)
	acceptBlock( // Overflow the subnet
		[]*state.Staker{primaryStaker1, subnetStaker1},
		nil,
	)
	acceptBlock( // Remove the subnet overflow
		nil,
		[]*state.Staker{subnetStaker1},
	)
	acceptBlock( // Remove the subnet entirely
		nil,
		[]*state.Staker{subnetStaker0},
	)

	m := NewManager(
		config.Internal{
			Validators: vdrs,
		},
		s,
		metrics.Noop,
		new(mockable.Clock),
	)

	expectedPrimaryNetworkWithAllValidators := validators.WarpSet{
		Validators: []*validators.Warp{
			{
				PublicKeyBytes: pk0Bytes,
				Weight:         1,
				NodeIDs:        []ids.NodeID{primaryStaker0.NodeID},
			},
			{
				PublicKeyBytes: pk1Bytes,
				Weight:         1,
				NodeIDs:        []ids.NodeID{primaryStaker1.NodeID},
			},
		},
		TotalWeight: genesistest.DefaultValidatorWeight*uint64(len(genesistest.DefaultNodeIDs)) + 2,
	}
	expectedValidators := []map[ids.ID]validators.WarpSet{
		{
			constants.PrimaryNetworkID: {
				Validators:  []*validators.Warp{},
				TotalWeight: genesistest.DefaultValidatorWeight * uint64(len(genesistest.DefaultNodeIDs)),
			},
		}, // Subnet didn't exist at genesis
		{
			constants.PrimaryNetworkID: {
				Validators: []*validators.Warp{
					{
						PublicKeyBytes: pk0Bytes,
						Weight:         1,
						NodeIDs:        []ids.NodeID{primaryStaker0.NodeID},
					},
				},
				TotalWeight: genesistest.DefaultValidatorWeight*uint64(len(genesistest.DefaultNodeIDs)) + 1,
			},
			subnetID: {
				Validators: []*validators.Warp{
					{
						PublicKeyBytes: pk0Bytes,
						Weight:         1,
						NodeIDs:        []ids.NodeID{subnetStaker0.NodeID},
					},
				},
				TotalWeight: 1,
			},
		}, // Subnet was added at height 1
		{
			constants.PrimaryNetworkID: expectedPrimaryNetworkWithAllValidators,
		}, // Subnet overflow occurred at height 1
		{
			constants.PrimaryNetworkID: expectedPrimaryNetworkWithAllValidators,
			subnetID: {
				Validators: []*validators.Warp{
					{
						PublicKeyBytes: pk0Bytes,
						Weight:         1,
						NodeIDs:        []ids.NodeID{subnetStaker0.NodeID},
					},
				},
				TotalWeight: 1,
			},
		}, // Subnet overflow was removed at height 2
		{
			constants.PrimaryNetworkID: expectedPrimaryNetworkWithAllValidators,
		}, // Subnet was removed at height 3
	}
	for height, expected := range expectedValidators {
		actual, err := m.GetWarpValidatorSets(t.Context(), uint64(height))
		require.NoError(err)
		require.Equal(expected, actual)

		actualPrimaryNetwork, err := m.GetWarpValidatorSet(t.Context(), uint64(height), constants.PrimaryNetworkID)
		require.NoError(err)
		require.Equal(expected[constants.PrimaryNetworkID], actualPrimaryNetwork)

		actualSubnet, err := m.GetWarpValidatorSet(t.Context(), uint64(height), subnetID)
		if err != nil {
			require.NotContains(expected, subnetID)
			continue
		}

		// Treat nil and empty slices as the same
		if len(actualSubnet.Validators) == 0 {
			actualSubnet.Validators = nil
		}
		require.Equal(expected[subnetID], actualSubnet)
	}
}

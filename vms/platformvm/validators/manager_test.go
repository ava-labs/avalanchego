// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
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

	sk, err := localsigner.New()
	require.NoError(err)
	var (
		subnetID      = ids.GenerateTestID()
		startTime     = genesistest.DefaultValidatorStartTime
		endTime       = startTime.Add(24 * time.Hour)
		pk            = sk.PublicKey()
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
		actual, err := m.GetValidatorSet(context.Background(), uint64(height), subnetID)
		require.NoError(err)
		require.Equal(expected, actual)
	}
}

func TestGetAllValidatorSets(t *testing.T) {
	require := require.New(t)

	vdrs := validators.NewManager()
	upgrades := upgradetest.GetConfig(upgradetest.Granite)
	upgradeTime := genesistest.DefaultValidatorStartTime.Add(2 * time.Second)
	s := statetest.New(t, statetest.Config{
		Validators: vdrs,
		Upgrades:   upgrades,
	})

	sk1, err := localsigner.New()
	require.NoError(err)
	sk2, err := localsigner.New()
	require.NoError(err)
	var (
		subnetID      = ids.GenerateTestID()
		startTime     = genesistest.DefaultValidatorStartTime
		endTime       = startTime.Add(24 * time.Hour)
		endTime2      = startTime.Add(48 * time.Hour)
		pk1           = sk1.PublicKey()
		pk2           = sk2.PublicKey()
		primaryStaker = &state.Staker{
			TxID:            ids.GenerateTestID(),
			NodeID:          ids.GenerateTestNodeID(),
			PublicKey:       pk1,
			SubnetID:        constants.PrimaryNetworkID,
			Weight:          100,
			StartTime:       startTime,
			EndTime:         endTime,
			PotentialReward: 1,
		}
		primaryStaker2 = &state.Staker{
			TxID:            ids.GenerateTestID(),
			NodeID:          ids.GenerateTestNodeID(),
			PublicKey:       pk2,
			SubnetID:        constants.PrimaryNetworkID,
			Weight:          100,
			StartTime:       startTime,
			EndTime:         endTime,
			PotentialReward: 1,
		}
		subnetStaker = &state.Staker{
			TxID:      ids.GenerateTestID(),
			NodeID:    primaryStaker.NodeID,
			PublicKey: nil,
			SubnetID:  subnetID,
			Weight:    50,
			StartTime: upgradeTime,
			EndTime:   endTime,
		}
		subnetStaker2 = &state.Staker{
			TxID:      ids.GenerateTestID(),
			NodeID:    primaryStaker2.NodeID,
			PublicKey: nil,
			SubnetID:  subnetID,
			Weight:    50,
			StartTime: upgradeTime,
			EndTime:   endTime2,
		}
	)

	s.AddSubnet(subnetID)

	m := NewManager(
		config.Internal{
			Validators: vdrs,
		},
		s,
		metrics.Noop,
		new(mockable.Clock),
	)

	type testCase struct {
		name           string
		height         uint64
		setup          func()
		expectedResult map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput
	}

	genesisValidators := make(map[ids.NodeID]*validators.GetValidatorOutput)
	for _, nodeID := range genesistest.DefaultNodeIDs {
		genesisValidators[nodeID] = &validators.GetValidatorOutput{
			NodeID:    nodeID,
			PublicKey: nil,
			Weight:    genesistest.DefaultValidatorWeight,
		}
	}

	appendGenesisVdrs := func(vdrs map[ids.NodeID]*validators.GetValidatorOutput) map[ids.NodeID]*validators.GetValidatorOutput {
		for nodeID, vdr := range genesisValidators {
			vdrs[nodeID] = vdr
		}
		return vdrs
	}

	testCases := []testCase{
		{
			name:   "height_0_genesis",
			height: 0,
			setup: func() {
				// No setup needed for genesis
			},
			expectedResult: map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput{
				constants.PrimaryNetworkID: genesisValidators,
				subnetID:                   {},
			},
		},
		{
			name:   "height_1_after_adding_validators",
			height: 1,
			setup: func() {
				blk, err := block.NewBanffStandardBlock(upgradeTime, s.GetLastAccepted(), 1, nil)
				require.NoError(err)

				s.SetHeight(blk.Height())
				s.SetTimestamp(blk.Timestamp())
				s.AddStatelessBlock(blk)
				s.SetLastAccepted(blk.ID())

				require.NoError(s.PutCurrentValidator(primaryStaker))
				require.NoError(s.PutCurrentValidator(primaryStaker2))
				require.NoError(s.PutCurrentValidator(subnetStaker))
				require.NoError(s.PutCurrentValidator(subnetStaker2))

				require.NoError(s.Commit())
			},
			expectedResult: map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput{
				constants.PrimaryNetworkID: appendGenesisVdrs(map[ids.NodeID]*validators.GetValidatorOutput{
					primaryStaker.NodeID: {
						NodeID:    primaryStaker.NodeID,
						PublicKey: pk1,
						Weight:    primaryStaker.Weight,
					},
					primaryStaker2.NodeID: {
						NodeID:    primaryStaker2.NodeID,
						PublicKey: pk2,
						Weight:    primaryStaker2.Weight,
					},
				},
				),
				subnetID: {
					subnetStaker.NodeID: {
						NodeID:    subnetStaker.NodeID,
						PublicKey: pk1,
						Weight:    subnetStaker.Weight,
					},
					subnetStaker2.NodeID: {
						NodeID:    subnetStaker2.NodeID,
						PublicKey: pk2,
						Weight:    subnetStaker2.Weight,
					},
				},
			},
		},
		{
			name:   "height_2_after_removing_subnet_staker",
			height: 2,
			setup: func() {
				blk, err := block.NewBanffStandardBlock(s.GetTimestamp(), s.GetLastAccepted(), 2, nil)
				require.NoError(err)

				s.SetHeight(blk.Height())
				s.SetTimestamp(blk.Timestamp())
				s.AddStatelessBlock(blk)
				s.SetLastAccepted(blk.ID())

				s.DeleteCurrentValidator(subnetStaker)

				require.NoError(s.Commit())
			},
			expectedResult: map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput{
				constants.PrimaryNetworkID: appendGenesisVdrs(map[ids.NodeID]*validators.GetValidatorOutput{
					primaryStaker.NodeID: {
						NodeID:    primaryStaker.NodeID,
						PublicKey: pk1,
						Weight:    primaryStaker.Weight,
					},
					primaryStaker2.NodeID: {
						NodeID:    primaryStaker2.NodeID,
						PublicKey: pk2,
						Weight:    primaryStaker2.Weight,
					},
				}),
				subnetID: {
					subnetStaker2.NodeID: {
						NodeID:    subnetStaker2.NodeID,
						PublicKey: pk2,
						Weight:    subnetStaker2.Weight,
					},
				},
			},
		},
		{
			name:   "height_3_after_removing_subnet_staker2",
			height: 3,
			setup: func() {
				blk, err := block.NewBanffStandardBlock(s.GetTimestamp(), s.GetLastAccepted(), 3, nil)
				require.NoError(err)

				s.SetHeight(blk.Height())
				s.SetTimestamp(blk.Timestamp())
				s.AddStatelessBlock(blk)
				s.SetLastAccepted(blk.ID())

				s.DeleteCurrentValidator(subnetStaker2)

				require.NoError(s.Commit())
			},
			expectedResult: map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput{
				constants.PrimaryNetworkID: appendGenesisVdrs(map[ids.NodeID]*validators.GetValidatorOutput{
					primaryStaker.NodeID: {
						NodeID:    primaryStaker.NodeID,
						PublicKey: pk1,
						Weight:    primaryStaker.Weight,
					},
					primaryStaker2.NodeID: {
						NodeID:    primaryStaker2.NodeID,
						PublicKey: pk2,
						Weight:    primaryStaker2.Weight,
					},
				}),
				subnetID: {},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(_ *testing.T) {
			tc.setup()

			allValidatorSets, err := m.GetAllValidatorSets(context.Background(), tc.height)
			require.NoError(err)

			require.Equal(tc.expectedResult, allValidatorSets)

			// Confirm that individual GetValidatorSet calls return the same results
			for subnetID := range tc.expectedResult {
				individualValidators, err := m.GetValidatorSet(context.Background(), tc.height, subnetID)
				require.NoError(err)
				require.Equal(allValidatorSets[subnetID], individualValidators)
			}
		})
	}
}

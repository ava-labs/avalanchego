// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

var defaultRewardConfig = reward.Config{
	MaxConsumptionRate: .12 * reward.PercentDenominator,
	MinConsumptionRate: .10 * reward.PercentDenominator,
	MintingPeriod:      365 * 24 * time.Hour,
	SupplyCap:          720 * units.MegaAvax,
}

func TestVM_GetValidatorSet(t *testing.T) {
	// Populate the validator set to use below
	var (
		numVdrs        = 4
		vdrBaseWeight  = uint64(1_000)
		testValidators []*validators.Validator
	)

	for i := 0; i < numVdrs; i++ {
		sk, err := bls.NewSecretKey()
		require.NoError(t, err)

		testValidators = append(testValidators, &validators.Validator{
			NodeID:    ids.GenerateTestNodeID(),
			PublicKey: bls.PublicFromSecretKey(sk),
			Weight:    vdrBaseWeight + uint64(i),
		})
	}

	type test struct {
		name string
		// Height we're getting the diff at
		height             uint64
		lastAcceptedHeight uint64
		subnetID           ids.ID
		// Validator sets at tip
		currentPrimaryNetworkValidators map[ids.NodeID]*validators.Validator
		currentSubnetValidators         []*validators.Validator

		// height --> nodeID --> weightDiff
		weightDiffs map[uint64]map[ids.NodeID]*state.ValidatorWeightDiff

		// height --> nodeID --> pkDiff
		pkDiffs        map[uint64]map[ids.NodeID]*bls.PublicKey
		expectedVdrSet map[ids.NodeID]*validators.GetValidatorOutput
		expectedErr    error
	}

	tests := []test{
		{
			name:               "after tip",
			height:             1,
			lastAcceptedHeight: 0,
			expectedVdrSet:     map[ids.NodeID]*validators.GetValidatorOutput{},
			expectedErr:        database.ErrNotFound,
		},
		{
			name:               "at tip",
			subnetID:           constants.PrimaryNetworkID,
			height:             1,
			lastAcceptedHeight: 1,
			currentPrimaryNetworkValidators: map[ids.NodeID]*validators.Validator{
				testValidators[0].NodeID: copyPrimaryValidator(testValidators[0]),
			},
			currentSubnetValidators: []*validators.Validator{
				copySubnetValidator(testValidators[0]),
			},
			expectedVdrSet: map[ids.NodeID]*validators.GetValidatorOutput{
				testValidators[0].NodeID: {
					NodeID:    testValidators[0].NodeID,
					PublicKey: testValidators[0].PublicKey,
					Weight:    testValidators[0].Weight,
				},
			},
			expectedErr: nil,
		},
		{
			name:               "1 before tip",
			height:             2,
			lastAcceptedHeight: 3,
			currentPrimaryNetworkValidators: map[ids.NodeID]*validators.Validator{
				testValidators[0].NodeID: copyPrimaryValidator(testValidators[0]),
				testValidators[1].NodeID: copyPrimaryValidator(testValidators[1]),
			},
			currentSubnetValidators: []*validators.Validator{
				// At tip we have these 2 validators
				copySubnetValidator(testValidators[0]),
				copySubnetValidator(testValidators[1]),
			},
			weightDiffs: map[uint64]map[ids.NodeID]*state.ValidatorWeightDiff{
				3: {
					// At the tip block vdrs[0] lost weight, vdrs[1] gained weight,
					// and vdrs[2] left
					testValidators[0].NodeID: {
						Decrease: true,
						Amount:   1,
					},
					testValidators[1].NodeID: {
						Decrease: false,
						Amount:   1,
					},
					testValidators[2].NodeID: {
						Decrease: true,
						Amount:   testValidators[2].Weight,
					},
				},
			},
			pkDiffs: map[uint64]map[ids.NodeID]*bls.PublicKey{
				3: {
					testValidators[2].NodeID: testValidators[2].PublicKey,
				},
			},
			expectedVdrSet: map[ids.NodeID]*validators.GetValidatorOutput{
				testValidators[0].NodeID: {
					NodeID:    testValidators[0].NodeID,
					PublicKey: testValidators[0].PublicKey,
					Weight:    testValidators[0].Weight + 1,
				},
				testValidators[1].NodeID: {
					NodeID:    testValidators[1].NodeID,
					PublicKey: testValidators[1].PublicKey,
					Weight:    testValidators[1].Weight - 1,
				},
				testValidators[2].NodeID: {
					NodeID:    testValidators[2].NodeID,
					PublicKey: testValidators[2].PublicKey,
					Weight:    testValidators[2].Weight,
				},
			},
			expectedErr: nil,
		},
		{
			name:               "2 before tip",
			height:             3,
			lastAcceptedHeight: 5,
			currentPrimaryNetworkValidators: map[ids.NodeID]*validators.Validator{
				testValidators[0].NodeID: copyPrimaryValidator(testValidators[0]),
				testValidators[1].NodeID: copyPrimaryValidator(testValidators[1]),
			},
			currentSubnetValidators: []*validators.Validator{
				// At tip we have these 2 validators
				copySubnetValidator(testValidators[0]),
				copySubnetValidator(testValidators[1]),
			},
			weightDiffs: map[uint64]map[ids.NodeID]*state.ValidatorWeightDiff{
				5: {
					// At the tip block vdrs[0] lost weight, vdrs[1] gained weight,
					// and vdrs[2] left
					testValidators[0].NodeID: {
						Decrease: true,
						Amount:   1,
					},
					testValidators[1].NodeID: {
						Decrease: false,
						Amount:   1,
					},
					testValidators[2].NodeID: {
						Decrease: true,
						Amount:   testValidators[2].Weight,
					},
				},
				4: {
					// At the block before tip vdrs[0] lost weight, vdrs[1] gained weight,
					// vdrs[2] joined
					testValidators[0].NodeID: {
						Decrease: true,
						Amount:   1,
					},
					testValidators[1].NodeID: {
						Decrease: false,
						Amount:   1,
					},
					testValidators[2].NodeID: {
						Decrease: false,
						Amount:   testValidators[2].Weight,
					},
				},
			},
			pkDiffs: map[uint64]map[ids.NodeID]*bls.PublicKey{
				5: {
					testValidators[2].NodeID: testValidators[2].PublicKey,
				},
				4: {},
			},
			expectedVdrSet: map[ids.NodeID]*validators.GetValidatorOutput{
				testValidators[0].NodeID: {
					NodeID:    testValidators[0].NodeID,
					PublicKey: testValidators[0].PublicKey,
					Weight:    testValidators[0].Weight + 2,
				},
				testValidators[1].NodeID: {
					NodeID:    testValidators[1].NodeID,
					PublicKey: testValidators[1].PublicKey,
					Weight:    testValidators[1].Weight - 2,
				},
			},
			expectedErr: nil,
		},
		{
			name:               "1 before tip; nil public key",
			height:             4,
			lastAcceptedHeight: 5,
			currentPrimaryNetworkValidators: map[ids.NodeID]*validators.Validator{
				testValidators[0].NodeID: copyPrimaryValidator(testValidators[0]),
				testValidators[1].NodeID: copyPrimaryValidator(testValidators[1]),
			},
			currentSubnetValidators: []*validators.Validator{
				// At tip we have these 2 validators
				copySubnetValidator(testValidators[0]),
				copySubnetValidator(testValidators[1]),
			},
			weightDiffs: map[uint64]map[ids.NodeID]*state.ValidatorWeightDiff{
				5: {
					// At the tip block vdrs[0] lost weight, vdrs[1] gained weight,
					// and vdrs[2] left
					testValidators[0].NodeID: {
						Decrease: true,
						Amount:   1,
					},
					testValidators[1].NodeID: {
						Decrease: false,
						Amount:   1,
					},
					testValidators[2].NodeID: {
						Decrease: true,
						Amount:   testValidators[2].Weight,
					},
				},
			},
			pkDiffs: map[uint64]map[ids.NodeID]*bls.PublicKey{
				5: {},
			},
			expectedVdrSet: map[ids.NodeID]*validators.GetValidatorOutput{
				testValidators[0].NodeID: {
					NodeID:    testValidators[0].NodeID,
					PublicKey: testValidators[0].PublicKey,
					Weight:    testValidators[0].Weight + 1,
				},
				testValidators[1].NodeID: {
					NodeID:    testValidators[1].NodeID,
					PublicKey: testValidators[1].PublicKey,
					Weight:    testValidators[1].Weight - 1,
				},
				testValidators[2].NodeID: {
					NodeID: testValidators[2].NodeID,
					Weight: testValidators[2].Weight,
				},
			},
			expectedErr: nil,
		},
		{
			name:               "1 before tip; subnet",
			height:             5,
			lastAcceptedHeight: 6,
			subnetID:           ids.GenerateTestID(),
			currentPrimaryNetworkValidators: map[ids.NodeID]*validators.Validator{
				testValidators[0].NodeID: copyPrimaryValidator(testValidators[0]),
				testValidators[1].NodeID: copyPrimaryValidator(testValidators[1]),
				testValidators[3].NodeID: copyPrimaryValidator(testValidators[3]),
			},
			currentSubnetValidators: []*validators.Validator{
				// At tip we have these 2 validators
				copySubnetValidator(testValidators[0]),
				copySubnetValidator(testValidators[1]),
			},
			weightDiffs: map[uint64]map[ids.NodeID]*state.ValidatorWeightDiff{
				6: {
					// At the tip block vdrs[0] lost weight, vdrs[1] gained weight,
					// and vdrs[2] left
					testValidators[0].NodeID: {
						Decrease: true,
						Amount:   1,
					},
					testValidators[1].NodeID: {
						Decrease: false,
						Amount:   1,
					},
					testValidators[2].NodeID: {
						Decrease: true,
						Amount:   testValidators[2].Weight,
					},
				},
			},
			pkDiffs: map[uint64]map[ids.NodeID]*bls.PublicKey{
				6: {},
			},
			expectedVdrSet: map[ids.NodeID]*validators.GetValidatorOutput{
				testValidators[0].NodeID: {
					NodeID:    testValidators[0].NodeID,
					PublicKey: testValidators[0].PublicKey,
					Weight:    testValidators[0].Weight + 1,
				},
				testValidators[1].NodeID: {
					NodeID:    testValidators[1].NodeID,
					PublicKey: testValidators[1].PublicKey,
					Weight:    testValidators[1].Weight - 1,
				},
				testValidators[2].NodeID: {
					NodeID: testValidators[2].NodeID,
					Weight: testValidators[2].Weight,
				},
			},
			expectedErr: nil,
		},
		{
			name:               "unrelated primary network key removal on subnet lookup",
			height:             4,
			lastAcceptedHeight: 5,
			subnetID:           ids.GenerateTestID(),
			currentPrimaryNetworkValidators: map[ids.NodeID]*validators.Validator{
				testValidators[0].NodeID: copyPrimaryValidator(testValidators[0]),
			},
			currentSubnetValidators: []*validators.Validator{
				copySubnetValidator(testValidators[0]),
			},
			weightDiffs: map[uint64]map[ids.NodeID]*state.ValidatorWeightDiff{
				5: {},
			},
			pkDiffs: map[uint64]map[ids.NodeID]*bls.PublicKey{
				5: {
					testValidators[1].NodeID: testValidators[1].PublicKey,
				},
			},
			expectedVdrSet: map[ids.NodeID]*validators.GetValidatorOutput{
				testValidators[0].NodeID: {
					NodeID:    testValidators[0].NodeID,
					PublicKey: testValidators[0].PublicKey,
					Weight:    testValidators[0].Weight,
				},
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// setup validators set
			vdrs := validators.NewMockManager(ctrl)
			cfg := config.Config{
				Chains:                 chains.TestManager,
				UptimePercentage:       .2,
				RewardConfig:           defaultRewardConfig,
				Validators:             vdrs,
				UptimeLockedCalculator: uptime.NewLockedCalculator(),
				BanffTime:              mockable.MaxTime,
			}
			mockState := state.NewMockState(ctrl)

			metrics, err := metrics.New("", prometheus.NewRegistry())
			r.NoError(err)

			clk := &mockable.Clock{}
			validatorSet := NewManager(logging.NoLog{}, cfg, mockState, metrics, clk)

			// Mock the VM's validators
			mockPrimaryVdrSet := validators.NewMockSet(ctrl)
			mockPrimaryVdrSet.EXPECT().List().Return(maps.Values(tt.currentPrimaryNetworkValidators)).AnyTimes()
			vdrs.EXPECT().Get(constants.PrimaryNetworkID).Return(mockPrimaryVdrSet, true).AnyTimes()

			mockSubnetVdrSet := mockPrimaryVdrSet
			if tt.subnetID != constants.PrimaryNetworkID {
				mockSubnetVdrSet = validators.NewMockSet(ctrl)
				mockSubnetVdrSet.EXPECT().List().Return(tt.currentSubnetValidators).AnyTimes()
			}
			vdrs.EXPECT().Get(tt.subnetID).Return(mockSubnetVdrSet, true).AnyTimes()

			for _, vdr := range testValidators {
				_, current := tt.currentPrimaryNetworkValidators[vdr.NodeID]
				if current {
					mockPrimaryVdrSet.EXPECT().Get(vdr.NodeID).Return(vdr, true).AnyTimes()
				} else {
					mockPrimaryVdrSet.EXPECT().Get(vdr.NodeID).Return(nil, false).AnyTimes()
				}
			}

			// Tell state what diffs to report
			for height, weightDiff := range tt.weightDiffs {
				mockState.EXPECT().GetValidatorWeightDiffs(height, gomock.Any()).Return(weightDiff, nil).AnyTimes()
			}

			for height, pkDiff := range tt.pkDiffs {
				mockState.EXPECT().GetValidatorPublicKeyDiffs(height).Return(pkDiff, nil)
			}

			// Tell state last accepted block to report
			mockTip := blocks.NewMockBlock(ctrl)
			mockTip.EXPECT().Height().Return(tt.lastAcceptedHeight)
			mockTipID := ids.GenerateTestID()
			mockState.EXPECT().GetLastAccepted().Return(mockTipID)
			mockState.EXPECT().GetStatelessBlock(mockTipID).Return(mockTip, choices.Accepted, nil)

			// Compute validator set at previous height
			gotVdrSet, err := validatorSet.GetValidatorSet(context.Background(), tt.height, tt.subnetID)
			r.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			r.Equal(tt.expectedVdrSet, gotVdrSet)
		})
	}
}

func copyPrimaryValidator(vdr *validators.Validator) *validators.Validator {
	newVdr := *vdr
	return &newVdr
}

func copySubnetValidator(vdr *validators.Validator) *validators.Validator {
	newVdr := *vdr
	newVdr.PublicKey = nil
	return &newVdr
}

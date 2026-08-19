// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestGetValidatorRules(t *testing.T) {
	type test struct {
		name      string
		subnetID  ids.ID
		backend   *Backend
		setup     func(*state.State)
		wantRules *addValidatorRules
		wantErr   error
	}

	var (
		minValidatorStake       uint64 = 1
		maxValidatorStake       uint64 = 2
		minStakeDuration               = 2 * time.Second
		heliconMinStakeDuration        = time.Second
		maxStakeDuration               = 3 * time.Second
		minDelegationFee        uint32 = 1337
		avaxAssetID                    = ids.GenerateTestID()
		customAssetID                  = ids.GenerateTestID()
		subnetID                       = ids.GenerateTestID()
	)

	tests := []test{
		{
			name:     "primary_network_pre_helicon",
			subnetID: constants.PrimaryNetworkID,
			backend: &Backend{
				Config: &config.Internal{
					MinValidatorStake:       minValidatorStake,
					MaxValidatorStake:       maxValidatorStake,
					MinStakeDuration:        minStakeDuration,
					HeliconMinStakeDuration: heliconMinStakeDuration,
					MaxStakeDuration:        maxStakeDuration,
					MinDelegationFee:        minDelegationFee,
					UpgradeConfig:           upgradetest.GetConfig(upgradetest.Granite),
				},
				Ctx: &snow.Context{
					AVAXAssetID: avaxAssetID,
				},
			},
			wantRules: &addValidatorRules{
				assetID:           avaxAssetID,
				minValidatorStake: minValidatorStake,
				maxValidatorStake: maxValidatorStake,
				minStakeDuration:  minStakeDuration,
				maxStakeDuration:  maxStakeDuration,
				minDelegationFee:  minDelegationFee,
			},
		},
		{
			name:     "primary_network_post_helicon",
			subnetID: constants.PrimaryNetworkID,
			backend: &Backend{
				Config: &config.Internal{
					MinValidatorStake:       minValidatorStake,
					MaxValidatorStake:       maxValidatorStake,
					MinStakeDuration:        minStakeDuration,
					HeliconMinStakeDuration: heliconMinStakeDuration,
					MaxStakeDuration:        maxStakeDuration,
					MinDelegationFee:        minDelegationFee,
					UpgradeConfig:           upgradetest.GetConfig(upgradetest.Helicon),
				},
				Ctx: &snow.Context{
					AVAXAssetID: avaxAssetID,
				},
			},
			wantRules: &addValidatorRules{
				assetID:           avaxAssetID,
				minValidatorStake: minValidatorStake,
				maxValidatorStake: maxValidatorStake,
				minStakeDuration:  heliconMinStakeDuration,
				maxStakeDuration:  maxStakeDuration,
				minDelegationFee:  minDelegationFee,
			},
		},
		{
			name:      "cannot_get_subnet_transformation",
			subnetID:  subnetID,
			backend:   nil,
			wantRules: &addValidatorRules{},
			wantErr:   database.ErrNotFound,
		},
		{
			name:     "subnet",
			subnetID: subnetID,
			backend:  nil,
			setup: func(s *state.State) {
				tx := &txs.Tx{
					Unsigned: &txs.TransformSubnetTx{
						AssetID:           customAssetID,
						InitialSupply:     10,
						MaximumSupply:     100,
						MinValidatorStake: minValidatorStake,
						MaxValidatorStake: maxValidatorStake,
						MinStakeDuration:  42,
						MaxStakeDuration:  1337,
						MinDelegationFee:  minDelegationFee,
						Subnet:            subnetID,
					},
				}
				s.AddSubnetTransformation(tx)
			},
			wantRules: &addValidatorRules{
				assetID:           customAssetID,
				minValidatorStake: minValidatorStake,
				maxValidatorStake: maxValidatorStake,
				minStakeDuration:  42 * time.Second,
				maxStakeDuration:  1337 * time.Second,
				minDelegationFee:  minDelegationFee,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			s := statetest.New(t, statetest.Config{})
			if tt.setup != nil {
				tt.setup(s)
			}

			gotRules, gotErr := getValidatorRules(tt.backend, s, tt.subnetID)
			if tt.wantErr != nil {
				require.ErrorIs(gotErr, tt.wantErr)
				return
			}
			require.NoError(gotErr)
			require.Equal(tt.wantRules, gotRules)
		})
	}
}

func TestGetDelegatorRules(t *testing.T) {
	type test struct {
		name      string
		subnetID  ids.ID
		backend   *Backend
		setup     func(*state.State)
		wantRules *addDelegatorRules
		wantErr   error
	}
	var (
		minDelegatorStake       uint64 = 1
		minValidatorStake       uint64 = 1
		maxValidatorStake       uint64 = 2
		minStakeDuration               = 2 * time.Second
		heliconMinStakeDuration        = time.Second
		maxStakeDuration               = 3 * time.Second
		minDelegationFee        uint32 = 0
		avaxAssetID                    = ids.GenerateTestID()
		customAssetID                  = ids.GenerateTestID()
		subnetID                       = ids.GenerateTestID()
	)
	tests := []test{
		{
			name:     "primary_network_pre_helicon",
			subnetID: constants.PrimaryNetworkID,
			backend: &Backend{
				Config: &config.Internal{
					MinDelegatorStake:       minDelegatorStake,
					MaxValidatorStake:       maxValidatorStake,
					MinStakeDuration:        minStakeDuration,
					HeliconMinStakeDuration: heliconMinStakeDuration,
					MaxStakeDuration:        maxStakeDuration,
					UpgradeConfig:           upgradetest.GetConfig(upgradetest.Granite),
				},
				Ctx: &snow.Context{
					AVAXAssetID: avaxAssetID,
				},
			},
			wantRules: &addDelegatorRules{
				assetID:                  avaxAssetID,
				minDelegatorStake:        minDelegatorStake,
				maxValidatorStake:        maxValidatorStake,
				minStakeDuration:         minStakeDuration,
				maxStakeDuration:         maxStakeDuration,
				maxValidatorWeightFactor: primaryNetworkMaxValidatorWeightFactor,
			},
		},
		{
			name:     "primary_network_post_helicon",
			subnetID: constants.PrimaryNetworkID,
			backend: &Backend{
				Config: &config.Internal{
					MinDelegatorStake:       minDelegatorStake,
					MaxValidatorStake:       maxValidatorStake,
					MinStakeDuration:        minStakeDuration,
					HeliconMinStakeDuration: heliconMinStakeDuration,
					MaxStakeDuration:        maxStakeDuration,
					UpgradeConfig:           upgradetest.GetConfig(upgradetest.Helicon),
				},
				Ctx: &snow.Context{
					AVAXAssetID: avaxAssetID,
				},
			},
			wantRules: &addDelegatorRules{
				assetID:                  avaxAssetID,
				minDelegatorStake:        minDelegatorStake,
				maxValidatorStake:        maxValidatorStake,
				minStakeDuration:         minStakeDuration,
				maxStakeDuration:         maxStakeDuration,
				maxValidatorWeightFactor: primaryNetworkMaxValidatorWeightFactor,
			},
		},
		{
			name:      "cannot_get_subnet_transformation",
			subnetID:  subnetID,
			backend:   nil,
			wantRules: &addDelegatorRules{},
			wantErr:   database.ErrNotFound,
		},
		{
			name:     "subnet",
			subnetID: subnetID,
			backend:  nil,
			setup: func(s *state.State) {
				tx := &txs.Tx{
					Unsigned: &txs.TransformSubnetTx{
						AssetID:                  customAssetID,
						InitialSupply:            10,
						MaximumSupply:            100,
						MinValidatorStake:        minValidatorStake,
						MaxValidatorStake:        maxValidatorStake,
						MinDelegatorStake:        minDelegatorStake,
						MinStakeDuration:         42,
						MaxStakeDuration:         1337,
						MinDelegationFee:         minDelegationFee,
						MaxValidatorWeightFactor: 21,
						Subnet:                   subnetID,
					},
				}
				s.AddSubnetTransformation(tx)
			},
			wantRules: &addDelegatorRules{
				assetID:                  customAssetID,
				minDelegatorStake:        minDelegatorStake,
				maxValidatorStake:        maxValidatorStake,
				minStakeDuration:         42 * time.Second,
				maxStakeDuration:         1337 * time.Second,
				maxValidatorWeightFactor: 21,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			s := statetest.New(t, statetest.Config{})
			if tt.setup != nil {
				tt.setup(s)
			}

			gotRules, gotErr := getDelegatorRules(tt.backend, s, tt.subnetID)
			if tt.wantErr != nil {
				require.ErrorIs(gotErr, tt.wantErr)
				return
			}
			require.NoError(gotErr)
			require.Equal(tt.wantRules, gotRules)
		})
	}
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/validators/fee"
)

func TestAdvanceTimeTo_UpdatesFeeState(t *testing.T) {
	const (
		secondsToAdvance  = 3
		durationToAdvance = secondsToAdvance * time.Second
	)

	feeConfig := gas.Config{
		MaxCapacity:     1000,
		MaxPerSecond:    100,
		TargetPerSecond: 50,
	}

	tests := []struct {
		name          string
		fork          upgradetest.Fork
		initialState  gas.State
		expectedState gas.State
	}{
		{
			name:          "Pre-Etna",
			fork:          upgradetest.Durango,
			initialState:  gas.State{},
			expectedState: gas.State{}, // Pre-Etna, fee state should not change
		},
		{
			name: "Etna with no usage",
			initialState: gas.State{
				Capacity: feeConfig.MaxCapacity,
				Excess:   0,
			},
			expectedState: gas.State{
				Capacity: feeConfig.MaxCapacity,
				Excess:   0,
			},
		},
		{
			name: "Etna with usage",
			fork: upgradetest.Etna,
			initialState: gas.State{
				Capacity: 1,
				Excess:   10_000,
			},
			expectedState: gas.State{
				Capacity: min(gas.Gas(1).AddOverTime(feeConfig.MaxPerSecond, secondsToAdvance), feeConfig.MaxCapacity),
				Excess:   gas.Gas(10_000).SubOverTime(feeConfig.TargetPerSecond, secondsToAdvance),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				require = require.New(t)

				s        = statetest.New(t, statetest.Config{})
				nextTime = s.GetTimestamp().Add(durationToAdvance)
			)

			// Ensure the invariant that [nextTime <= nextStakerChangeTime] on
			// AdvanceTimeTo is maintained.
			nextStakerChangeTime, err := state.GetNextStakerChangeTime(
				genesis.LocalParams.ValidatorFeeConfig,
				s,
				mockable.MaxTime,
			)
			require.NoError(err)
			require.False(nextTime.After(nextStakerChangeTime))

			s.SetFeeState(test.initialState)

			validatorsModified, err := AdvanceTimeTo(
				&Backend{
					Config: &config.Internal{
						DynamicFeeConfig: feeConfig,
						UpgradeConfig:    upgradetest.GetConfig(test.fork),
					},
				},
				s,
				nextTime,
			)
			require.NoError(err)
			require.False(validatorsModified)
			require.Equal(test.expectedState, s.GetFeeState())
			require.Equal(nextTime, s.GetTimestamp())
		})
	}
}

func TestAdvanceTimeTo_RemovesStaleExpiries(t *testing.T) {
	var (
		currentTime = genesistest.DefaultValidatorStartTime
		newTime     = currentTime.Add(3 * time.Second)
		newTimeUnix = uint64(newTime.Unix())

		unexpiredTime         = newTimeUnix + 1
		expiredTime           = newTimeUnix
		previouslyExpiredTime = newTimeUnix - 1
		validationID          = ids.GenerateTestID()
	)

	tests := []struct {
		name             string
		initialExpiries  []state.ExpiryEntry
		expectedExpiries []state.ExpiryEntry
	}{
		{
			name: "no expiries",
		},
		{
			name: "unexpired expiry",
			initialExpiries: []state.ExpiryEntry{
				{
					Timestamp:    unexpiredTime,
					ValidationID: validationID,
				},
			},
			expectedExpiries: []state.ExpiryEntry{
				{
					Timestamp:    unexpiredTime,
					ValidationID: validationID,
				},
			},
		},
		{
			name: "unexpired expiry at new time",
			initialExpiries: []state.ExpiryEntry{
				{
					Timestamp:    expiredTime,
					ValidationID: ids.GenerateTestID(),
				},
			},
		},
		{
			name: "unexpired expiry at previous time",
			initialExpiries: []state.ExpiryEntry{
				{
					Timestamp:    previouslyExpiredTime,
					ValidationID: ids.GenerateTestID(),
				},
			},
		},
		{
			name: "limit expiries removed",
			initialExpiries: []state.ExpiryEntry{
				{
					Timestamp:    expiredTime,
					ValidationID: ids.GenerateTestID(),
				},
				{
					Timestamp:    unexpiredTime,
					ValidationID: validationID,
				},
			},
			expectedExpiries: []state.ExpiryEntry{
				{
					Timestamp:    unexpiredTime,
					ValidationID: validationID,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				require = require.New(t)
				s       = statetest.New(t, statetest.Config{})
			)

			// Ensure the invariant that [newTime <= nextStakerChangeTime] on
			// AdvanceTimeTo is maintained.
			nextStakerChangeTime, err := state.GetNextStakerChangeTime(
				genesis.LocalParams.ValidatorFeeConfig,
				s,
				mockable.MaxTime,
			)
			require.NoError(err)
			require.False(newTime.After(nextStakerChangeTime))

			for _, expiry := range test.initialExpiries {
				s.PutExpiry(expiry)
			}

			validatorsModified, err := AdvanceTimeTo(
				&Backend{
					Config: &config.Internal{
						UpgradeConfig: upgradetest.GetConfig(upgradetest.Latest),
					},
				},
				s,
				newTime,
			)
			require.NoError(err)
			require.False(validatorsModified)

			expiryIterator, err := s.GetExpiryIterator()
			require.NoError(err)
			require.Equal(
				test.expectedExpiries,
				iterator.ToSlice(expiryIterator),
			)
		})
	}
}

func TestAdvanceTimeTo_UpdateL1Validators(t *testing.T) {
	sk, err := localsigner.New()
	require.NoError(t, err)

	const (
		secondsToAdvance = 3
		timeToAdvance    = secondsToAdvance * time.Second
	)

	var (
		pk      = sk.PublicKey()
		pkBytes = bls.PublicKeyToUncompressedBytes(pk)

		newL1Validator = func(endAccumulatedFee uint64) state.L1Validator {
			return state.L1Validator{
				ValidationID:      ids.GenerateTestID(),
				SubnetID:          ids.GenerateTestID(),
				NodeID:            ids.GenerateTestNodeID(),
				PublicKey:         pkBytes,
				Weight:            1,
				EndAccumulatedFee: endAccumulatedFee,
			}
		}
		l1ValidatorToEvict0 = newL1Validator(3 * units.NanoAvax) // lasts 3 seconds
		l1ValidatorToEvict1 = newL1Validator(3 * units.NanoAvax) // lasts 3 seconds
		l1ValidatorToKeep   = newL1Validator(units.Avax)

		currentTime = genesistest.DefaultValidatorStartTime
		newTime     = currentTime.Add(timeToAdvance)

		config = config.Internal{
			ValidatorFeeConfig: fee.Config{
				Capacity:                 genesis.LocalParams.ValidatorFeeConfig.Capacity,
				Target:                   1,
				MinPrice:                 genesis.LocalParams.ValidatorFeeConfig.MinPrice,
				ExcessConversionConstant: genesis.LocalParams.ValidatorFeeConfig.ExcessConversionConstant,
			},
			UpgradeConfig: upgradetest.GetConfig(upgradetest.Latest),
		}
	)

	tests := []struct {
		name                 string
		initialL1Validators  []state.L1Validator
		expectedModified     bool
		expectedL1Validators []state.L1Validator
		expectedExcess       gas.Gas
	}{
		{
			name:             "no L1 validators",
			expectedModified: false,
			expectedExcess:   0,
		},
		{
			name: "evicted one",
			initialL1Validators: []state.L1Validator{
				l1ValidatorToEvict0,
			},
			expectedModified: true,
			expectedExcess:   0,
		},
		{
			name: "evicted all",
			initialL1Validators: []state.L1Validator{
				l1ValidatorToEvict0,
				l1ValidatorToEvict1,
			},
			expectedModified: true,
			expectedExcess:   3,
		},
		{
			name: "evicted 2 of 3",
			initialL1Validators: []state.L1Validator{
				l1ValidatorToEvict0,
				l1ValidatorToEvict1,
				l1ValidatorToKeep,
			},
			expectedModified: true,
			expectedL1Validators: []state.L1Validator{
				l1ValidatorToKeep,
			},
			expectedExcess: 6,
		},
		{
			name: "no evictions",
			initialL1Validators: []state.L1Validator{
				l1ValidatorToKeep,
			},
			expectedModified: false,
			expectedL1Validators: []state.L1Validator{
				l1ValidatorToKeep,
			},
			expectedExcess: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				require = require.New(t)
				s       = statetest.New(t, statetest.Config{})
			)

			for _, l1Validator := range test.initialL1Validators {
				require.NoError(s.PutL1Validator(l1Validator))
			}

			// Ensure the invariant that [newTime <= nextStakerChangeTime] on
			// AdvanceTimeTo is maintained.
			nextStakerChangeTime, err := state.GetNextStakerChangeTime(
				genesis.LocalParams.ValidatorFeeConfig,
				s,
				mockable.MaxTime,
			)
			require.NoError(err)
			require.False(newTime.After(nextStakerChangeTime))

			validatorsModified, err := AdvanceTimeTo(
				&Backend{
					Config: &config,
				},
				s,
				newTime,
			)
			require.NoError(err)
			require.Equal(test.expectedModified, validatorsModified)

			activeL1Validators, err := s.GetActiveL1ValidatorsIterator()
			require.NoError(err)
			require.Equal(
				test.expectedL1Validators,
				iterator.ToSlice(activeL1Validators),
			)

			require.Equal(test.expectedExcess, s.GetL1ValidatorExcess())
			require.Equal(uint64(secondsToAdvance), s.GetAccruedFees())
		})
	}
}

// Regression test for a case where a pending delegator and validator are promoted to current stakers. Only Apricot can
// trip this regression, because after Apricot pending validators always sort before pending delegators according to
// txs.Priority.
func TestAdvanceTimeTo_PromotePendingDelegatorAndValidator(t *testing.T) {
	s := statetest.New(t, statetest.Config{})

	var (
		startTime = s.GetTimestamp().Add(time.Second)
		endTime   = startTime.Add(14 * 24 * time.Hour)
		nodeID    = ids.GenerateTestNodeID()
	)

	require.NoError(t, s.PutPendingValidator(&state.Staker{
		TxID:     ids.GenerateTestID(),
		NodeID:   nodeID,
		SubnetID: constants.PrimaryNetworkID,
		Weight:   units.MilliAvax,
		// Both the validator and delegator share a start time to have their iteration order broken by their priority
		StartTime: startTime,
		EndTime:   endTime,
		NextTime:  startTime,
		Priority:  txs.PrimaryNetworkValidatorPendingPriority,
	}))

	s.PutPendingDelegator(&state.Staker{
		TxID:     ids.GenerateTestID(),
		NodeID:   nodeID,
		SubnetID: constants.PrimaryNetworkID,
		Weight:   units.MilliAvax,
		// Both the validator and delegator share a start time to have their iteration order broken by their priority
		StartTime: startTime,
		EndTime:   endTime,
		NextTime:  startTime,
		Priority:  txs.PrimaryNetworkDelegatorApricotPendingPriority,
	})

	rewardConfig := reward.Config{
		MaxConsumptionRate: .12 * reward.PercentDenominator,
		MinConsumptionRate: .1 * reward.PercentDenominator,
		MintingPeriod:      365 * 24 * time.Hour,
		SupplyCap:          720 * units.MegaAvax,
	}
	updated, err := AdvanceTimeTo(
		&Backend{
			Config: &config.Internal{
				DynamicFeeConfig:   genesis.LocalParams.DynamicFeeConfig,
				ValidatorFeeConfig: genesis.LocalParams.ValidatorFeeConfig,
				RewardConfig:       rewardConfig,
				UpgradeConfig:      upgradetest.GetConfig(upgradetest.Latest),
			},
			Rewards: reward.NewCalculator(rewardConfig),
		},
		s,
		startTime,
	)
	require.NoError(t, err)
	require.True(t, updated)

	// Check that the stakers got promoted to current
	gotValidator, err := s.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(t, err)
	require.Equal(t, txs.PrimaryNetworkValidatorCurrentPriority, gotValidator.Priority)

	currentDelegatorItr, err := s.GetCurrentDelegatorIterator(constants.PrimaryNetworkID, nodeID)
	require.NoError(t, err)
	defer currentDelegatorItr.Release()

	require.True(t, currentDelegatorItr.Next())
	require.Equal(t, txs.PrimaryNetworkDelegatorCurrentPriority, currentDelegatorItr.Value().Priority)

	// Check that they are no longer pending
	_, err = s.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.Equal(t, database.ErrNotFound, err)

	pendingDelegatorItr, err := s.GetPendingDelegatorIterator(constants.PrimaryNetworkID, nodeID)
	require.NoError(t, err)
	defer pendingDelegatorItr.Release()

	require.False(t, pendingDelegatorItr.Next())
}

// TestAdvanceTimeTo_PromotePendingDelegatorAndValidator_PreservesRewardOrder tests that promoting pending stakers mints
// rewards in the historical pending staking iterator order.
func TestAdvanceTimeTo_PromotePendingDelegatorAndValidator_PreservesRewardOrder(t *testing.T) {
	s := statetest.New(t, statetest.Config{})

	var (
		startTime       = s.GetTimestamp().Add(time.Second)
		endTime         = startTime.Add(14 * 24 * time.Hour)
		nodeID          = ids.GenerateTestNodeID()
		validatorWeight = 2 * units.MegaAvax
		delegatorWeight = units.KiloAvax
	)

	require.NoError(t, s.PutPendingValidator(&state.Staker{
		TxID:      ids.GenerateTestID(),
		NodeID:    nodeID,
		SubnetID:  constants.PrimaryNetworkID,
		Weight:    validatorWeight,
		StartTime: startTime,
		EndTime:   endTime,
		NextTime:  startTime,
		Priority:  txs.PrimaryNetworkValidatorPendingPriority,
	}))

	s.PutPendingDelegator(&state.Staker{
		TxID:      ids.GenerateTestID(),
		NodeID:    nodeID,
		SubnetID:  constants.PrimaryNetworkID,
		Weight:    delegatorWeight,
		StartTime: startTime,
		EndTime:   endTime,
		NextTime:  startTime,
		Priority:  txs.PrimaryNetworkDelegatorApricotPendingPriority,
	})

	rewardConfig := reward.Config{
		MaxConsumptionRate: .12 * reward.PercentDenominator,
		MinConsumptionRate: .1 * reward.PercentDenominator,
		MintingPeriod:      365 * 24 * time.Hour,
		SupplyCap:          720 * units.MegaAvax,
	}
	rewards := reward.NewCalculator(rewardConfig)

	initialSupply, err := s.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(t, err)

	duration := endTime.Sub(startTime)
	wantDelegatorReward := rewards.Calculate(duration, delegatorWeight, initialSupply)
	wantValidatorReward := rewards.Calculate(duration, validatorWeight, initialSupply+wantDelegatorReward)

	_, err = AdvanceTimeTo(
		&Backend{
			Config: &config.Internal{
				DynamicFeeConfig:   genesis.LocalParams.DynamicFeeConfig,
				ValidatorFeeConfig: genesis.LocalParams.ValidatorFeeConfig,
				RewardConfig:       rewardConfig,
				UpgradeConfig:      upgradetest.GetConfig(upgradetest.Latest),
			},
			Rewards: rewards,
		},
		s,
		startTime,
	)
	require.NoError(t, err)

	gotValidator, err := s.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(t, err)
	require.Equal(t, wantValidatorReward, gotValidator.PotentialReward)

	delegatorItr, err := s.GetCurrentDelegatorIterator(constants.PrimaryNetworkID, nodeID)
	require.NoError(t, err)
	defer delegatorItr.Release()
	require.True(t, delegatorItr.Next())
	require.Equal(t, wantDelegatorReward, delegatorItr.Value().PotentialReward)

	gotSupply, err := s.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(t, err)
	require.Equal(t, initialSupply+wantDelegatorReward+wantValidatorReward, gotSupply)
}

func TestGetRewardConfigForStakeStartRamp(t *testing.T) {
	var (
		heliconTime   = time.Unix(1_000_000, 0)
		upgradeConfig = upgradetest.GetConfigWithUpgradeTime(upgradetest.Helicon, heliconTime)
	)

	tests := []struct {
		name              string
		configuredMinRate uint64
		offset            time.Duration
		want              uint64
		wantErr           error
	}{
		{
			name:   "before_helicon",
			offset: -time.Second,
			want:   100_000,
		},
		{
			name: "at_helicon",
			want: 100_000,
		},
		{
			name:   "one_third_ramp",
			offset: 30 * 24 * time.Hour,
			want:   91_667,
		},
		{
			name:   "mid_ramp",
			offset: 45 * 24 * time.Hour,
			want:   87_500,
		},
		{
			name:   "at_ramp_end",
			offset: heliconMinConsumptionRateReductionPeriod,
			want:   75_000,
		},
		{
			name:   "after_ramp",
			offset: heliconMinConsumptionRateReductionPeriod + time.Second,
			want:   75_000,
		},
		{
			name:              "min_consumption_rate_underflow",
			configuredMinRate: heliconMinConsumptionRateReduction - 1,
			offset:            heliconMinConsumptionRateReductionPeriod,
			wantErr:           math.ErrUnderflow,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := genesis.MainnetParams.StakingConfig.RewardConfig
			if tt.configuredMinRate != 0 {
				cfg.MinConsumptionRate = tt.configuredMinRate
			}

			got, err := GetRewardConfigForStakeStart(
				cfg,
				upgradeConfig,
				heliconTime.Add(tt.offset),
			)
			require.ErrorIs(t, err, tt.wantErr)
			if tt.wantErr != nil {
				return
			}
			require.Equal(t, tt.want, got.MinConsumptionRate)
		})
	}
}

func TestGetRewardsCalculatorTransformSubnetBypassesPrimaryNetworkRewardConfig(t *testing.T) {
	const (
		stakeDuration                 = 14 * 24 * time.Hour
		weight                        = units.MegaAvax
		supply                        = 5 * units.MegaAvax
		transformedMinConsumptionRate = 42_000
	)
	var (
		heliconTime       = time.Unix(1_000_000, 0)
		baseConfig        = genesis.MainnetParams.StakingConfig.RewardConfig
		wantConfig        = baseConfig
		transformedSubnet = ids.GenerateTestID()
		transformedState  = statetest.New(t, statetest.Config{})
	)
	wantConfig.MinConsumptionRate = transformedMinConsumptionRate

	transformedState.AddSubnetTransformation(&txs.Tx{Unsigned: &txs.TransformSubnetTx{
		Subnet:             transformedSubnet,
		MaxConsumptionRate: wantConfig.MaxConsumptionRate,
		MinConsumptionRate: wantConfig.MinConsumptionRate,
		MaximumSupply:      wantConfig.SupplyCap,
	}})

	rewards, err := GetRewardsCalculator(
		baseConfig,
		upgradetest.GetConfigWithUpgradeTime(upgradetest.Helicon, heliconTime),
		transformedState,
		transformedSubnet,
		heliconTime.Add(heliconMinConsumptionRateReductionPeriod),
	)
	require.NoError(t, err)

	want := reward.NewCalculator(wantConfig).Calculate(
		stakeDuration,
		weight,
		supply,
	)

	got := rewards.Calculate(stakeDuration, weight, supply)
	require.Equal(t, want, got)
}

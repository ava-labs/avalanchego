// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
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
				Capacity: min(gas.Gas(1).AddPerSecond(feeConfig.MaxPerSecond, secondsToAdvance), feeConfig.MaxCapacity),
				Excess:   gas.Gas(10_000).SubPerSecond(feeConfig.TargetPerSecond, secondsToAdvance),
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
					Config: &config.Config{
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
					Config: &config.Config{
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

func TestAdvanceTimeTo_UpdateSoVs(t *testing.T) {
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)

	const (
		secondsToAdvance = 3
		timeToAdvance    = secondsToAdvance * time.Second
	)

	var (
		pk      = bls.PublicFromSecretKey(sk)
		pkBytes = bls.PublicKeyToUncompressedBytes(pk)

		newSoV = func(endAccumulatedFee uint64) state.SubnetOnlyValidator {
			return state.SubnetOnlyValidator{
				ValidationID:      ids.GenerateTestID(),
				SubnetID:          ids.GenerateTestID(),
				NodeID:            ids.GenerateTestNodeID(),
				PublicKey:         pkBytes,
				Weight:            1,
				EndAccumulatedFee: endAccumulatedFee,
			}
		}
		sovToEvict0 = newSoV(3 * units.NanoAvax) // lasts 3 seconds
		sovToEvict1 = newSoV(3 * units.NanoAvax) // lasts 3 seconds
		sovToKeep   = newSoV(units.Avax)

		currentTime = genesistest.DefaultValidatorStartTime
		newTime     = currentTime.Add(timeToAdvance)

		config = config.Config{
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
		name             string
		initialSoVs      []state.SubnetOnlyValidator
		expectedModified bool
		expectedSoVs     []state.SubnetOnlyValidator
		expectedExcess   gas.Gas
	}{
		{
			name:             "no SoVs",
			expectedModified: false,
			expectedExcess:   0,
		},
		{
			name: "evicted one",
			initialSoVs: []state.SubnetOnlyValidator{
				sovToEvict0,
			},
			expectedModified: true,
			expectedExcess:   0,
		},
		{
			name: "evicted all",
			initialSoVs: []state.SubnetOnlyValidator{
				sovToEvict0,
				sovToEvict1,
			},
			expectedModified: true,
			expectedExcess:   3,
		},
		{
			name: "evicted 2 of 3",
			initialSoVs: []state.SubnetOnlyValidator{
				sovToEvict0,
				sovToEvict1,
				sovToKeep,
			},
			expectedModified: true,
			expectedSoVs: []state.SubnetOnlyValidator{
				sovToKeep,
			},
			expectedExcess: 6,
		},
		{
			name: "no evictions",
			initialSoVs: []state.SubnetOnlyValidator{
				sovToKeep,
			},
			expectedModified: false,
			expectedSoVs: []state.SubnetOnlyValidator{
				sovToKeep,
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

			for _, sov := range test.initialSoVs {
				require.NoError(s.PutSubnetOnlyValidator(sov))
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

			activeSoVs, err := s.GetActiveSubnetOnlyValidatorsIterator()
			require.NoError(err)
			require.Equal(
				test.expectedSoVs,
				iterator.ToSlice(activeSoVs),
			)

			require.Equal(test.expectedExcess, s.GetSoVExcess())
			require.Equal(uint64(secondsToAdvance), s.GetAccruedFees())
		})
	}
}

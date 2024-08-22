// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/gas"
)

func Test_ValidatorState(t *testing.T) {
	var (
		minute uint64 = 60
		hour          = 60 * minute
		day           = 24 * hour
		week          = 7 * day
	)

	tests := []struct {
		name          string
		initial       ValidatorState
		seconds       uint64
		expectedFee   uint64
		expectedState ValidatorState
	}{
		{
			name: "excess=0, current<target, minute",
			initial: ValidatorState{
				Current:                  10,
				Target:                   10_000,
				Capacity:                 20_000,
				Excess:                   0,
				MinFee:                   2_048,
				ExcessConversionConstant: 60_480_000_000,
			},
			seconds:     minute,
			expectedFee: 122_880,
			expectedState: ValidatorState{
				Current:                  10,
				Target:                   10_000,
				Capacity:                 20_000,
				Excess:                   0,
				MinFee:                   2_048,
				ExcessConversionConstant: 60_480_000_000,
			},
		},
		{
			name: "excess=0, current>target, minute",
			initial: ValidatorState{
				Current:                  15_000,
				Target:                   10_000,
				Capacity:                 20_000,
				Excess:                   0,
				MinFee:                   2_048,
				ExcessConversionConstant: 60_480_000_000,
			},
			seconds:     minute,
			expectedFee: 122_880,
			expectedState: ValidatorState{
				Current:                  15_000,
				Target:                   10_000,
				Capacity:                 20_000,
				Excess:                   300_000,
				MinFee:                   2_048,
				ExcessConversionConstant: 60_480_000_000,
			},
		},
		{
			name: "excess=K, current=target, minute",
			initial: ValidatorState{
				Current:                  10_000,
				Target:                   10_000,
				Capacity:                 20_000,
				Excess:                   60_480_000_000,
				MinFee:                   2_048,
				ExcessConversionConstant: 60_480_000_000,
			},
			seconds:     minute,
			expectedFee: 334_020,
			expectedState: ValidatorState{
				Current:                  10_000,
				Target:                   10_000,
				Capacity:                 20_000,
				Excess:                   60_480_000_000,
				MinFee:                   2_048,
				ExcessConversionConstant: 60_480_000_000,
			},
		},
		{
			name: "excess=0, current>target, day",
			initial: ValidatorState{
				Current:                  15_000,
				Target:                   10_000,
				Capacity:                 20_000,
				Excess:                   0,
				MinFee:                   2_048,
				ExcessConversionConstant: 60_480_000_000,
			},
			seconds:     day,
			expectedFee: 177_538_111,
			expectedState: ValidatorState{
				Current:                  15_000,
				Target:                   10_000,
				Capacity:                 20_000,
				Excess:                   432_000_000,
				MinFee:                   2_048,
				ExcessConversionConstant: 60_480_000_000,
			},
		},
		{
			name: "excess=K, current=target, day",
			initial: ValidatorState{
				Current:                  10_000,
				Target:                   10_000,
				Capacity:                 20_000,
				Excess:                   60_480_000_000,
				MinFee:                   2_048,
				ExcessConversionConstant: 60_480_000_000,
			},
			seconds:     day,
			expectedFee: 480_988_800,
			expectedState: ValidatorState{
				Current:                  10_000,
				Target:                   10_000,
				Capacity:                 20_000,
				Excess:                   60_480_000_000,
				MinFee:                   2_048,
				ExcessConversionConstant: 60_480_000_000,
			},
		},
		{
			name: "excess hits 0 during, current<target, day",
			initial: ValidatorState{
				Current:                  9_000,
				Target:                   10_000,
				Capacity:                 20_000,
				Excess:                   gas.Gas(6 * hour * 1_000),
				MinFee:                   2_048,
				ExcessConversionConstant: 60_480_000_000,
			},
			seconds:     day,
			expectedFee: 176_947_200,
			expectedState: ValidatorState{
				Current:                  9_000,
				Target:                   10_000,
				Capacity:                 20_000,
				Excess:                   0,
				MinFee:                   2_048,
				ExcessConversionConstant: 60_480_000_000,
			},
		},
		{
			name: "excess=0, current>target, week",
			initial: ValidatorState{
				Current:                  15_000,
				Target:                   10_000,
				Capacity:                 20_000,
				Excess:                   0,
				MinFee:                   2_048,
				ExcessConversionConstant: 60_480_000_000,
			},
			seconds:     week,
			expectedFee: 1_269_816_464,
			expectedState: ValidatorState{
				Current:                  15_000,
				Target:                   10_000,
				Capacity:                 20_000,
				Excess:                   3_024_000_000,
				MinFee:                   2_048,
				ExcessConversionConstant: 60_480_000_000,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actualFee := test.initial.CalculateContinuousFee(test.seconds)
			require.Equal(t, test.expectedFee, actualFee)
			seconds := test.initial.CalculateTimeTillContinuousFee(test.expectedFee)
			require.Equal(t, test.seconds, seconds)
			actualState := test.initial.AdvanceTime(seconds)
			require.Equal(t, test.expectedState, actualState)
		})
	}
}

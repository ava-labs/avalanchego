// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_ValidatorState_CalculateFee(t *testing.T) {
	var (
		minute uint64 = 60
		hour          = 60 * minute
		day           = 24 * hour
		week          = 7 * day
	)

	tests := []struct {
		name     string
		initial  ValidatorState
		seconds  uint64
		expected uint64
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
			seconds:  minute,
			expected: 122_880,
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
			seconds:  minute,
			expected: 122_880,
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
			seconds:  minute,
			expected: 334_020,
		},
		{
			name: "excess=0, current>target, day",
			initial: ValidatorState{
				Current:                  15_000,
				Target:                   10_000,
				Capacity:                 20_000,
				Excess:                   0,
				MinFee:                   2048,
				ExcessConversionConstant: 60_480_000_000,
			},
			seconds:  day,
			expected: 177_538_111,
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
			seconds:  day,
			expected: 480_988_800,
		},
		{
			name: "excess hits 0 during, current<target, day",
			initial: ValidatorState{
				Current:                  9_000,
				Target:                   10_000,
				Capacity:                 20_000,
				Excess:                   Gas(6 * hour * 1_000),
				MinFee:                   2048,
				ExcessConversionConstant: 60_480_000_000,
			},
			seconds:  day,
			expected: 176_947_200,
		},
		{
			name: "excess=0, current>target, week",
			initial: ValidatorState{
				Current:                  15_000,
				Target:                   10_000,
				Capacity:                 20_000,
				Excess:                   0,
				MinFee:                   2048,
				ExcessConversionConstant: 60_480_000_000,
			},
			seconds:  week,
			expected: 1_269_816_464,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := test.initial.CalculateContinuousFee(test.seconds)
			require.Equal(t, test.expected, actual)
			seconds := test.initial.CalculateTimeTillContinuousFee(test.expected)
			require.Equal(t, test.seconds, seconds)
		})
	}
}

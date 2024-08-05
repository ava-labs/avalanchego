// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_State_AdvanceTime(t *testing.T) {
	tests := []struct {
		name               string
		initial            State
		maxGasCapacity     Gas
		maxGasPerSecond    Gas
		targetGasPerSecond Gas
		duration           uint64
		expected           State
	}{
		{
			name: "cap capacity",
			initial: State{
				Capacity: 10,
				Excess:   0,
			},
			maxGasCapacity:     20,
			maxGasPerSecond:    10,
			targetGasPerSecond: 0,
			duration:           2,
			expected: State{
				Capacity: 20,
				Excess:   0,
			},
		},
		{
			name: "increase capacity",
			initial: State{
				Capacity: 10,
				Excess:   0,
			},
			maxGasCapacity:     30,
			maxGasPerSecond:    10,
			targetGasPerSecond: 0,
			duration:           1,
			expected: State{
				Capacity: 20,
				Excess:   0,
			},
		},
		{
			name: "avoid excess underflow",
			initial: State{
				Capacity: 10,
				Excess:   10,
			},
			maxGasCapacity:     20,
			maxGasPerSecond:    10,
			targetGasPerSecond: 10,
			duration:           2,
			expected: State{
				Capacity: 20,
				Excess:   0,
			},
		},
		{
			name: "reduce excess",
			initial: State{
				Capacity: 10,
				Excess:   10,
			},
			maxGasCapacity:     20,
			maxGasPerSecond:    10,
			targetGasPerSecond: 5,
			duration:           1,
			expected: State{
				Capacity: 20,
				Excess:   5,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := test.initial.AdvanceTime(test.maxGasCapacity, test.maxGasPerSecond, test.targetGasPerSecond, test.duration)
			require.Equal(t, test.expected, actual)
		})
	}
}

func Test_State_ConsumeGas(t *testing.T) {
	tests := []struct {
		name        string
		initial     State
		gas         Gas
		expected    State
		expectedErr error
	}{
		{
			name: "consume some gas",
			initial: State{
				Capacity: 10,
				Excess:   10,
			},
			gas: 5,
			expected: State{
				Capacity: 5,
				Excess:   15,
			},
			expectedErr: nil,
		},
		{
			name: "consume all gas",
			initial: State{
				Capacity: 10,
				Excess:   10,
			},
			gas: 10,
			expected: State{
				Capacity: 0,
				Excess:   20,
			},
			expectedErr: nil,
		},
		{
			name: "consume too much gas",
			initial: State{
				Capacity: 10,
				Excess:   10,
			},
			gas:         11,
			expected:    State{},
			expectedErr: ErrInsufficientCapacity,
		},
		{
			name: "maximum excess",
			initial: State{
				Capacity: 10,
				Excess:   math.MaxUint64,
			},
			gas: 1,
			expected: State{
				Capacity: 9,
				Excess:   math.MaxUint64,
			},
			expectedErr: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			actual, err := test.initial.ConsumeGas(test.gas)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, actual)
		})
	}
}

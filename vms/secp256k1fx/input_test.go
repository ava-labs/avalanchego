// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInputVerifyNil(t *testing.T) {
	tests := []struct {
		name        string
		in          *Input
		expectedErr error
	}{
		{
			name:        "nil input",
			in:          nil,
			expectedErr: ErrNilInput,
		},
		{
			name:        "not sorted",
			in:          &Input{SigIndices: []uint32{2, 1}},
			expectedErr: ErrInputIndicesNotSortedUnique,
		},
		{
			name:        "not unique",
			in:          &Input{SigIndices: []uint32{2, 2}},
			expectedErr: ErrInputIndicesNotSortedUnique,
		},
		{
			name:        "passes verification",
			in:          &Input{SigIndices: []uint32{1, 2}},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.in.Verify()
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestInputCost(t *testing.T) {
	tests := []struct {
		name         string
		in           *Input
		expectedCost uint64
	}{
		{
			name:         "2 sigs",
			in:           &Input{SigIndices: []uint32{1, 2}},
			expectedCost: 2 * CostPerSignature,
		},
		{
			name:         "3 sigs",
			in:           &Input{SigIndices: []uint32{1, 2, 3}},
			expectedCost: 3 * CostPerSignature,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			cost, err := tt.in.Cost()
			require.NoError(err)
			require.Equal(tt.expectedCost, cost)
		})
	}
}

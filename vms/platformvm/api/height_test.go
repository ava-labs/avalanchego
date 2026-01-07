// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHeightMarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		height   Height
		expected string
	}{
		{
			name:     "0",
			height:   0,
			expected: `"0"`,
		},
		{
			name:     "56",
			height:   56,
			expected: `"56"`,
		},
		{
			name:     "proposed",
			height:   ProposedHeight,
			expected: `"proposed"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			bytes, err := test.height.MarshalJSON()
			require.NoError(err)
			require.Equal(test.expected, string(bytes))
		})
	}
}

func TestHeightUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name        string
		initial     Height
		json        string
		expected    Height
		expectedErr error
	}{
		{
			name:     "null 56",
			initial:  56,
			json:     "null",
			expected: 56,
		},
		{
			name:     "null proposed",
			initial:  ProposedHeight,
			json:     "null",
			expected: ProposedHeight,
		},
		{
			name:     "proposed",
			json:     `"proposed"`,
			expected: ProposedHeight,
		},
		{
			name:        "not a number",
			json:        `"not a number"`,
			expectedErr: errInvalidHeight,
		},
		{
			name:     "56",
			json:     `56`,
			expected: 56,
		},
		{
			name:     `"56"`,
			json:     `"56"`,
			expected: 56,
		},
		{
			name:        "max uint64",
			json:        "18446744073709551615",
			expectedErr: errInvalidHeight,
		},
		{
			name:        `"max uint64"`,
			json:        `"18446744073709551615"`,
			expectedErr: errInvalidHeight,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			err := test.initial.UnmarshalJSON([]byte(test.json))
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, test.initial)
		})
	}
}

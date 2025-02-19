// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"testing"

	"github.com/ava-labs/subnet-evm/params"
	"github.com/stretchr/testify/require"
)

func TestVerifyExtra(t *testing.T) {
	tests := []struct {
		name     string
		rules    params.AvalancheRules
		extra    []byte
		expected error
	}{
		{
			name:     "initial_valid",
			rules:    params.AvalancheRules{},
			extra:    make([]byte, params.MaximumExtraDataSize),
			expected: nil,
		},
		{
			name:     "initial_invalid",
			rules:    params.AvalancheRules{},
			extra:    make([]byte, params.MaximumExtraDataSize+1),
			expected: errInvalidExtraLength,
		},
		{
			name: "subnet_evm_valid",
			rules: params.AvalancheRules{
				IsSubnetEVM: true,
			},
			extra:    make([]byte, FeeWindowSize),
			expected: nil,
		},
		{
			name: "subnet_evm_invalid_less",
			rules: params.AvalancheRules{
				IsSubnetEVM: true,
			},
			extra:    make([]byte, FeeWindowSize-1),
			expected: errInvalidExtraLength,
		},
		{
			name: "subnet_evm_invalid_more",
			rules: params.AvalancheRules{
				IsSubnetEVM: true,
			},
			extra:    make([]byte, FeeWindowSize+1),
			expected: errInvalidExtraLength,
		},
		{
			name: "durango_valid_min",
			rules: params.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, FeeWindowSize),
			expected: nil,
		},
		{
			name: "durango_valid_extra",
			rules: params.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, FeeWindowSize+1),
			expected: nil,
		},
		{
			name: "durango_invalid",
			rules: params.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, FeeWindowSize-1),
			expected: errInvalidExtraLength,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := VerifyExtra(test.rules, test.extra)
			require.ErrorIs(t, err, test.expected)
		})
	}
}

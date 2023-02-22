// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlist

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestVerifyAllowlistAllowList(t *testing.T) {
	admins := []common.Address{{1}}
	enableds := []common.Address{{2}}
	tests := []struct {
		name          string
		config        AllowListConfig
		expectedError string
	}{
		{
			name:          "invalid allow list config in allowlist",
			config:        AllowListConfig{admins, admins},
			expectedError: "cannot set address",
		},
		{
			name:          "nil member allow list config in allowlist",
			config:        AllowListConfig{nil, nil},
			expectedError: "",
		},
		{
			name:          "empty member allow list config in allowlist",
			config:        AllowListConfig{[]common.Address{}, []common.Address{}},
			expectedError: "",
		},
		{
			name:          "valid allow list config in allowlist",
			config:        AllowListConfig{admins, enableds},
			expectedError: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			err := tt.config.Verify()
			if tt.expectedError == "" {
				require.NoError(err)
			} else {
				require.ErrorContains(err, tt.expectedError)
			}
		})
	}
}

func TestEqualAllowListAllowList(t *testing.T) {
	admins := []common.Address{{1}}
	enableds := []common.Address{{2}}
	tests := []struct {
		name     string
		config   *AllowListConfig
		other    *AllowListConfig
		expected bool
	}{
		{
			name:     "non-nil config and nil other",
			config:   &AllowListConfig{admins, enableds},
			other:    nil,
			expected: false,
		},
		{
			name:     "different admin",
			config:   &AllowListConfig{admins, enableds},
			other:    &AllowListConfig{[]common.Address{{3}}, enableds},
			expected: false,
		},
		{
			name:     "different enabled",
			config:   &AllowListConfig{admins, enableds},
			other:    &AllowListConfig{admins, []common.Address{{3}}},
			expected: false,
		},
		{
			name:     "same config",
			config:   &AllowListConfig{admins, enableds},
			other:    &AllowListConfig{admins, enableds},
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(tt.expected, tt.config.Equal(tt.other))
		})
	}
}

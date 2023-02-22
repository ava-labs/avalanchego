// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nativeminter

import (
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/stretchr/testify/require"
)

func TestVerifyContractNativeMinterConfig(t *testing.T) {
	admins := []common.Address{{1}}
	enableds := []common.Address{{2}}
	tests := []struct {
		name          string
		config        precompileconfig.Config
		ExpectedError string
	}{
		{
			name:          "invalid allow list config in native minter allowlist",
			config:        NewConfig(big.NewInt(3), admins, admins, nil),
			ExpectedError: "cannot set address",
		},
		{
			name:          "duplicate admins in config in native minter allowlist",
			config:        NewConfig(big.NewInt(3), append(admins, admins[0]), enableds, nil),
			ExpectedError: "duplicate address",
		},
		{
			name:          "duplicate enableds in config in native minter allowlist",
			config:        NewConfig(big.NewInt(3), admins, append(enableds, enableds[0]), nil),
			ExpectedError: "duplicate address",
		},
		{
			name: "nil amount in native minter config",
			config: NewConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(123),
					common.HexToAddress("0x02"): nil,
				}),
			ExpectedError: "initial mint cannot contain nil",
		},
		{
			name: "negative amount in native minter config",
			config: NewConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(123),
					common.HexToAddress("0x02"): math.NewHexOrDecimal256(-1),
				}),
			ExpectedError: "initial mint cannot contain invalid amount",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			err := tt.config.Verify()
			if tt.ExpectedError == "" {
				require.NoError(err)
			} else {
				require.ErrorContains(err, tt.ExpectedError)
			}
		})
	}
}

func TestEqualContractNativeMinterConfig(t *testing.T) {
	admins := []common.Address{{1}}
	enableds := []common.Address{{2}}
	tests := []struct {
		name     string
		config   precompileconfig.Config
		other    precompileconfig.Config
		expected bool
	}{
		{
			name:     "non-nil config and nil other",
			config:   NewConfig(big.NewInt(3), admins, enableds, nil),
			other:    nil,
			expected: false,
		},
		{
			name:     "different type",
			config:   NewConfig(big.NewInt(3), admins, enableds, nil),
			other:    precompileconfig.NewNoopStatefulPrecompileConfig(),
			expected: false,
		},
		{
			name:     "different timestamps",
			config:   NewConfig(big.NewInt(3), admins, nil, nil),
			other:    NewConfig(big.NewInt(4), admins, nil, nil),
			expected: false,
		},
		{
			name:     "different enabled",
			config:   NewConfig(big.NewInt(3), admins, nil, nil),
			other:    NewConfig(big.NewInt(3), admins, enableds, nil),
			expected: false,
		},
		{
			name: "different initial mint amounts",
			config: NewConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(1),
				}),
			other: NewConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(2),
				}),
			expected: false,
		},
		{
			name: "different initial mint addresses",
			config: NewConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(1),
				}),
			other: NewConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x02"): math.NewHexOrDecimal256(1),
				}),
			expected: false,
		},
		{
			name: "same config",
			config: NewConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(1),
				}),
			other: NewConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(1),
				}),
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

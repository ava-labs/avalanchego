// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/acp176"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap1"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap5"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/cortina"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestGasLimit(t *testing.T) {
	tests := []struct {
		name      string
		upgrades  params.NetworkUpgrades
		parent    *types.Header
		timestamp uint64
		want      uint64
		wantErr   error
	}{
		{
			name:     "fortuna_invalid_parent_header",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			wantErr: acp176.ErrStateInsufficientLength,
		},
		{
			name:     "fortuna_initial_max_capacity",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			want: acp176.MinMaxCapacity,
		},
		{
			name:     "cortina",
			upgrades: params.TestCortinaChainConfig.NetworkUpgrades,
			want:     cortina.GasLimit,
		},
		{
			name:     "ap1",
			upgrades: params.TestApricotPhase1Config.NetworkUpgrades,
			want:     ap1.GasLimit,
		},
		{
			name:     "launch",
			upgrades: params.TestLaunchConfig.NetworkUpgrades,
			parent: &types.Header{
				GasLimit: 1,
			},
			want: 1, // Same as parent
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &params.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			got, err := GasLimit(config, test.parent, test.timestamp)
			require.ErrorIs(err, test.wantErr)
			require.Equal(test.want, got)
		})
	}
}

func TestVerifyGasUsed(t *testing.T) {
	tests := []struct {
		name     string
		upgrades params.NetworkUpgrades
		parent   *types.Header
		header   *types.Header
		want     error
	}{
		{
			name:     "fortuna_massive_extra_gas_used",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			header: &types.Header{
				ExtDataGasUsed: new(big.Int).Lsh(common.Big1, 64),
			},
			want: errInvalidExtraDataGasUsed,
		},
		{
			name:     "fortuna_gas_used_overflow",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			header: &types.Header{
				GasUsed:        math.MaxUint[uint64](),
				ExtDataGasUsed: common.Big1,
			},
			want: math.ErrOverflow,
		},
		{
			name:     "fortuna_invalid_capacity",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header: &types.Header{},
			want:   acp176.ErrStateInsufficientLength,
		},
		{
			name:     "fortuna_invalid_usage",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{
				Time: 1,
				// The maximum allowed gas used is:
				// (header.Time - parent.Time) * [acp176.MinMaxPerSecond]
				// which is equal to [acp176.MinMaxPerSecond].
				GasUsed: acp176.MinMaxPerSecond + 1,
			},
			want: errInvalidGasUsed,
		},
		{
			name:     "fortuna_max_consumption",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{
				Time:    1,
				GasUsed: acp176.MinMaxPerSecond,
			},
			want: nil,
		},
		{
			name:     "cortina_does_not_include_extra_gas_used",
			upgrades: params.TestCortinaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{
				GasUsed:        cortina.GasLimit,
				ExtDataGasUsed: common.Big1,
			},
			want: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := &params.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			err := VerifyGasUsed(config, test.parent, test.header)
			require.ErrorIs(t, err, test.want)
		})
	}
}

func TestVerifyGasLimit(t *testing.T) {
	tests := []struct {
		name     string
		upgrades params.NetworkUpgrades
		parent   *types.Header
		header   *types.Header
		want     error
	}{
		{
			name:     "fortuna_invalid_header",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header: &types.Header{},
			want:   acp176.ErrStateInsufficientLength,
		},
		{
			name:     "fortuna_invalid",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{
				GasLimit: acp176.MinMaxCapacity + 1,
			},
			want: errInvalidGasLimit,
		},
		{
			name:     "fortuna_valid",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{
				GasLimit: acp176.MinMaxCapacity,
			},
		},
		{
			name:     "cortina_valid",
			upgrades: params.TestCortinaChainConfig.NetworkUpgrades,
			header: &types.Header{
				GasLimit: cortina.GasLimit,
			},
		},
		{
			name:     "cortina_invalid",
			upgrades: params.TestCortinaChainConfig.NetworkUpgrades,
			header: &types.Header{
				GasLimit: cortina.GasLimit + 1,
			},
			want: errInvalidGasLimit,
		},
		{
			name:     "ap1_valid",
			upgrades: params.TestApricotPhase1Config.NetworkUpgrades,
			header: &types.Header{
				GasLimit: ap1.GasLimit,
			},
		},
		{
			name:     "ap1_invalid",
			upgrades: params.TestApricotPhase1Config.NetworkUpgrades,
			header: &types.Header{
				GasLimit: ap1.GasLimit + 1,
			},
			want: errInvalidGasLimit,
		},
		{
			name:     "launch_valid",
			upgrades: params.TestLaunchConfig.NetworkUpgrades,
			parent: &types.Header{
				GasLimit: 50_000,
			},
			header: &types.Header{
				GasLimit: 50_001, // Gas limit is allowed to change by 1/1024
			},
		},
		{
			name:     "launch_too_low",
			upgrades: params.TestLaunchConfig.NetworkUpgrades,
			parent: &types.Header{
				GasLimit: params.MinGasLimit,
			},
			header: &types.Header{
				GasLimit: params.MinGasLimit - 1,
			},
			want: errInvalidGasLimit,
		},
		{
			name:     "launch_too_high",
			upgrades: params.TestLaunchConfig.NetworkUpgrades,
			parent: &types.Header{
				GasLimit: params.MaxGasLimit,
			},
			header: &types.Header{
				GasLimit: params.MaxGasLimit + 1,
			},
			want: errInvalidGasLimit,
		},
		{
			name:     "change_too_large",
			upgrades: params.TestLaunchConfig.NetworkUpgrades,
			parent: &types.Header{
				GasLimit: params.MinGasLimit,
			},
			header: &types.Header{
				GasLimit: params.MaxGasLimit,
			},
			want: errInvalidGasLimit,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := &params.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			err := VerifyGasLimit(config, test.parent, test.header)
			require.ErrorIs(t, err, test.want)
		})
	}
}

func TestGasCapacity(t *testing.T) {
	tests := []struct {
		name      string
		upgrades  params.NetworkUpgrades
		parent    *types.Header
		timestamp uint64
		want      uint64
		wantErr   error
	}{
		{
			name:     "cortina",
			upgrades: params.TestCortinaChainConfig.NetworkUpgrades,
			want:     cortina.GasLimit,
		},
		{
			name:     "fortuna_invalid_header",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			wantErr: acp176.ErrStateInsufficientLength,
		},
		{
			name:     "fortuna_after_1s",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			timestamp: 1,
			want:      acp176.MinMaxPerSecond,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &params.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			got, err := GasCapacity(config, test.parent, test.timestamp)
			require.ErrorIs(err, test.wantErr)
			require.Equal(test.want, got)
		})
	}
}

func TestRemainingAtomicGasCapacity(t *testing.T) {
	tests := []struct {
		name     string
		upgrades params.NetworkUpgrades
		parent   *types.Header
		header   *types.Header
		want     uint64
		wantErr  error
	}{
		{
			name:     "ap5",
			upgrades: params.TestApricotPhase5Config.NetworkUpgrades,
			header:   &types.Header{},
			want:     ap5.AtomicGasLimit,
		},
		{
			name:     "fortuna_invalid_header",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header:  &types.Header{},
			wantErr: acp176.ErrStateInsufficientLength,
		},
		{
			name:     "fortuna_negative_capacity",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{
				GasUsed: 1,
			},
			wantErr: gas.ErrInsufficientCapacity,
		},
		{
			name:     "f",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{
				Time:    1,
				GasUsed: 1,
			},
			want: acp176.MinMaxPerSecond - 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &params.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			got, err := RemainingAtomicGasCapacity(config, test.parent, test.header)
			require.ErrorIs(err, test.wantErr)
			require.Equal(test.want, got)
		})
	}
}

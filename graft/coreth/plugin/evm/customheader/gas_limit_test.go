// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap1"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap5"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/cortina"
	"github.com/ava-labs/avalanchego/graft/coreth/utils"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
)

func TestGasLimit(t *testing.T) {
	tests := []struct {
		name     string
		upgrades extras.NetworkUpgrades
		parent   *types.Header
		want     uint64
		wantErr  error
	}{
		{
			name:     "fortuna_invalid_parent_header",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			wantErr: acp176.ErrStateInsufficientLength,
		},
		{
			name:     "fortuna_initial_max_capacity",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			want: acp176.MinMaxCapacity,
		},
		{
			name:     "cortina",
			upgrades: extras.TestCortinaChainConfig.NetworkUpgrades,
			want:     cortina.GasLimit,
		},
		{
			name:     "ap1",
			upgrades: extras.TestApricotPhase1Config.NetworkUpgrades,
			want:     ap1.GasLimit,
		},
		{
			name:     "launch",
			upgrades: extras.TestLaunchConfig.NetworkUpgrades,
			parent: &types.Header{
				GasLimit: 1,
			},
			want: 1, // Same as parent
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &extras.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			got, err := GasLimit(config, test.parent, 0)
			require.ErrorIs(err, test.wantErr)
			require.Equal(test.want, got)
		})
	}
}

func TestVerifyGasUsed(t *testing.T) {
	tests := []struct {
		name     string
		upgrades extras.NetworkUpgrades
		parent   *types.Header
		header   *types.Header
		want     error
	}{
		{
			name:     "granite_uses_milliseconds",
			upgrades: extras.TestGraniteChainConfig.NetworkUpgrades,
			parent: customtypes.WithHeaderExtra(&types.Header{
				Number: big.NewInt(0),
			}, &customtypes.HeaderExtra{
				TimeMilliseconds: utils.NewUint64(0),
			}),
			header: customtypes.WithHeaderExtra(&types.Header{
				Time:    1,
				GasUsed: acp176.MinMaxPerSecond + 500,
			}, &customtypes.HeaderExtra{
				TimeMilliseconds: utils.NewUint64(1500),
			}),
			want: nil,
		},
		{
			name:     "fortuna_massive_extra_gas_used",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			header: customtypes.WithHeaderExtra(
				&types.Header{},
				&customtypes.HeaderExtra{
					ExtDataGasUsed: new(big.Int).Lsh(common.Big1, 64),
				},
			),
			want: errInvalidExtraDataGasUsed,
		},
		{
			name:     "fortuna_gas_used_overflow",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			header: customtypes.WithHeaderExtra(
				&types.Header{
					GasUsed: math.MaxUint[uint64](),
				},
				&customtypes.HeaderExtra{
					ExtDataGasUsed: common.Big1,
				},
			),
			want: math.ErrOverflow,
		},
		{
			name:     "fortuna_invalid_capacity",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header: &types.Header{},
			want:   acp176.ErrStateInsufficientLength,
		},
		{
			name:     "fortuna_invalid_usage",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
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
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
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
			upgrades: extras.TestCortinaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: customtypes.WithHeaderExtra(
				&types.Header{
					GasUsed: cortina.GasLimit,
				},
				&customtypes.HeaderExtra{
					ExtDataGasUsed: common.Big1,
				},
			),
			want: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := &extras.ChainConfig{
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
		upgrades extras.NetworkUpgrades
		parent   *types.Header
		header   *types.Header
		want     error
	}{
		{
			name:     "granite_invalid",
			upgrades: extras.TestGraniteChainConfig.NetworkUpgrades,
			parent: customtypes.WithHeaderExtra(&types.Header{
				Number: big.NewInt(0),
			}, &customtypes.HeaderExtra{
				TimeMilliseconds: utils.NewUint64(0),
			}),
			header: customtypes.WithHeaderExtra(&types.Header{
				GasLimit: acp176.MinMaxPerSecond - 1,
			}, &customtypes.HeaderExtra{
				TimeMilliseconds: utils.NewUint64(0),
			}),
			want: errInvalidGasLimit,
		},
		{
			name:     "granite_valid",
			upgrades: extras.TestGraniteChainConfig.NetworkUpgrades,
			parent: customtypes.WithHeaderExtra(&types.Header{
				Number: big.NewInt(0),
			}, &customtypes.HeaderExtra{
				TimeMilliseconds: utils.NewUint64(0),
			}),
			header: customtypes.WithHeaderExtra(&types.Header{
				GasLimit: acp176.MinMaxCapacity,
			}, &customtypes.HeaderExtra{
				TimeMilliseconds: utils.NewUint64(0),
			}),
		},
		{
			name:     "fortuna_invalid_header",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header: &types.Header{},
			want:   acp176.ErrStateInsufficientLength,
		},
		{
			name:     "fortuna_invalid",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
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
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{
				GasLimit: acp176.MinMaxCapacity,
			},
		},
		{
			name:     "cortina_valid",
			upgrades: extras.TestCortinaChainConfig.NetworkUpgrades,
			header: &types.Header{
				GasLimit: cortina.GasLimit,
			},
		},
		{
			name:     "cortina_invalid",
			upgrades: extras.TestCortinaChainConfig.NetworkUpgrades,
			header: &types.Header{
				GasLimit: cortina.GasLimit + 1,
			},
			want: errInvalidGasLimit,
		},
		{
			name:     "ap1_valid",
			upgrades: extras.TestApricotPhase1Config.NetworkUpgrades,
			header: &types.Header{
				GasLimit: ap1.GasLimit,
			},
		},
		{
			name:     "ap1_invalid",
			upgrades: extras.TestApricotPhase1Config.NetworkUpgrades,
			header: &types.Header{
				GasLimit: ap1.GasLimit + 1,
			},
			want: errInvalidGasLimit,
		},
		{
			name:     "launch_valid",
			upgrades: extras.TestLaunchConfig.NetworkUpgrades,
			parent: &types.Header{
				GasLimit: 50_000,
			},
			header: &types.Header{
				GasLimit: 50_001, // Gas limit is allowed to change by 1/1024
			},
		},
		{
			name:     "launch_too_low",
			upgrades: extras.TestLaunchConfig.NetworkUpgrades,
			parent: &types.Header{
				GasLimit: ap0.MinGasLimit,
			},
			header: &types.Header{
				GasLimit: ap0.MinGasLimit - 1,
			},
			want: errInvalidGasLimit,
		},
		{
			name:     "launch_too_high",
			upgrades: extras.TestLaunchConfig.NetworkUpgrades,
			parent: &types.Header{
				GasLimit: ap0.MaxGasLimit,
			},
			header: &types.Header{
				GasLimit: ap0.MaxGasLimit + 1,
			},
			want: errInvalidGasLimit,
		},
		{
			name:     "change_too_large",
			upgrades: extras.TestLaunchConfig.NetworkUpgrades,
			parent: &types.Header{
				GasLimit: ap0.MinGasLimit,
			},
			header: &types.Header{
				GasLimit: ap0.MaxGasLimit,
			},
			want: errInvalidGasLimit,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := &extras.ChainConfig{
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
		upgrades  extras.NetworkUpgrades
		parent    *types.Header
		timestamp uint64
		want      uint64
		wantErr   error
	}{
		{
			name:     "cortina",
			upgrades: extras.TestCortinaChainConfig.NetworkUpgrades,
			want:     cortina.GasLimit,
		},
		{
			name:     "fortuna_invalid_header",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			wantErr: acp176.ErrStateInsufficientLength,
		},
		{
			name:     "fortuna_after_1s",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			timestamp: 1000,
			want:      acp176.MinMaxPerSecond,
		},
		{
			name:     "fortuna_after_1.5s",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			timestamp: 1500,
			want:      acp176.MinMaxPerSecond, // unchanged, since this should be rounded down
		},
		{
			name:     "granite_after_1s",
			upgrades: extras.TestGraniteChainConfig.NetworkUpgrades,
			parent: customtypes.WithHeaderExtra(&types.Header{
				Number: big.NewInt(0),
			}, &customtypes.HeaderExtra{
				TimeMilliseconds: utils.NewUint64(0),
			}),
			timestamp: 1000,
			want:      acp176.MinMaxPerSecond,
		},
		{
			name:     "granite_after_1.5s",
			upgrades: extras.TestGraniteChainConfig.NetworkUpgrades,
			parent: customtypes.WithHeaderExtra(&types.Header{
				Number: big.NewInt(0),
			}, &customtypes.HeaderExtra{
				TimeMilliseconds: utils.NewUint64(0),
			}),
			timestamp: 1500,
			want:      acp176.MinMaxPerSecond * 3 / 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &extras.ChainConfig{
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
		upgrades extras.NetworkUpgrades
		parent   *types.Header
		header   *types.Header
		want     uint64
		wantErr  error
	}{
		{
			name:     "ap5",
			upgrades: extras.TestApricotPhase5Config.NetworkUpgrades,
			header:   &types.Header{},
			want:     ap5.AtomicGasLimit,
		},
		{
			name:     "fortuna_invalid_header",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header:  &types.Header{},
			wantErr: acp176.ErrStateInsufficientLength,
		},
		{
			name:     "fortuna_negative_capacity",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{
				GasUsed: 1,
			},
			wantErr: gas.ErrInsufficientCapacity,
		},
		{
			name:     "fortuna_valid",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{
				Time:    1,
				GasUsed: 1,
			},
			want: acp176.MinMaxPerSecond - 1,
		},
		{
			name:     "granite_valid",
			upgrades: extras.TestGraniteChainConfig.NetworkUpgrades,
			parent: customtypes.WithHeaderExtra(&types.Header{
				Number: big.NewInt(0),
			}, &customtypes.HeaderExtra{
				TimeMilliseconds: utils.NewUint64(0),
			}),
			header: customtypes.WithHeaderExtra(&types.Header{
				Time:    1,
				GasUsed: 1,
			}, &customtypes.HeaderExtra{
				TimeMilliseconds: utils.NewUint64(1500),
			}),
			want: acp176.MinMaxPerSecond*3/2 - 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &extras.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			got, err := RemainingAtomicGasCapacity(config, test.parent, test.header)
			require.ErrorIs(err, test.wantErr)
			require.Equal(test.want, got)
		})
	}
}

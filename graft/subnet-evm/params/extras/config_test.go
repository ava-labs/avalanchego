// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
)

func pointer[T any](v T) *T { return &v }

func TestChainConfigDescription(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config    *ChainConfig
		wantRegex string
	}{
		"nil": {},
		"empty": {
			config: &ChainConfig{},
			wantRegex: `Avalanche Upgrades \(timestamp based\)\:
 - SubnetEVM Timestamp: ( )+@nil( )+\(https:\/\/github\.com\/ava-labs\/avalanchego\/releases\/tag\/v1\.10\.0\)
( - .+Timestamp: .+\n)+
Upgrade Config: {}
Fee Config: {}
Allow Fee Recipients: false
$`,
		},
		"set": {
			config: &ChainConfig{
				NetworkUpgrades: NetworkUpgrades{
					SubnetEVMTimestamp: pointer(uint64(1)),
					DurangoTimestamp:   pointer(uint64(2)),
					EtnaTimestamp:      pointer(uint64(3)),
					FortunaTimestamp:   pointer(uint64(4)),
				},
				FeeConfig: commontype.FeeConfig{
					GasLimit:                 big.NewInt(5),
					TargetBlockRate:          6,
					MinBaseFee:               big.NewInt(7),
					TargetGas:                big.NewInt(8),
					BaseFeeChangeDenominator: big.NewInt(9),
					MinBlockGasCost:          big.NewInt(10),
					MaxBlockGasCost:          big.NewInt(11),
					BlockGasCostStep:         big.NewInt(12),
				},
				AllowFeeRecipients: true,
				UpgradeConfig: UpgradeConfig{
					NetworkUpgradeOverrides: &NetworkUpgrades{
						SubnetEVMTimestamp: pointer(uint64(13)),
					},
					StateUpgrades: []StateUpgrade{
						{
							BlockTimestamp: pointer(uint64(14)),
							StateUpgradeAccounts: map[common.Address]StateUpgradeAccount{
								{15}: {
									Code: []byte{16},
								},
							},
						},
					},
				},
			},
			wantRegex: `Avalanche Upgrades \(timestamp based\)\:
 - SubnetEVM Timestamp: ( )+@1( )+\(https:\/\/github\.com\/ava-labs\/avalanchego\/releases\/tag\/v1\.10\.0\)
( - .+Timestamp: .+\n)+
Upgrade Config: {"networkUpgradeOverrides":{"subnetEVMTimestamp":13},"stateUpgrades":\[{"blockTimestamp":14,"accounts":{"0x0f00000000000000000000000000000000000000":{"code":"0x10"}}}\]}
Fee Config: {"gasLimit":5,"targetBlockRate":6,"minBaseFee":7,"targetGas":8,"baseFeeChangeDenominator":9,"minBlockGasCost":10,"maxBlockGasCost":11,"blockGasCostStep":12}
Allow Fee Recipients: true
$`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got := test.config.Description()
			require.Regexp(t, test.wantRegex, got, "config description mismatch")
		})
	}
}

func TestChainConfigVerify(t *testing.T) {
	t.Parallel()

	validFeeConfig := commontype.FeeConfig{
		GasLimit:                 big.NewInt(1),
		TargetBlockRate:          1,
		MinBaseFee:               big.NewInt(1),
		TargetGas:                big.NewInt(1),
		BaseFeeChangeDenominator: big.NewInt(1),
		MinBlockGasCost:          big.NewInt(1),
		MaxBlockGasCost:          big.NewInt(1),
		BlockGasCostStep:         big.NewInt(1),
	}

	tests := map[string]struct {
		config    ChainConfig
		wantError error
	}{
		"invalid_feeconfig": {
			config: ChainConfig{
				FeeConfig: commontype.FeeConfig{
					GasLimit: nil,
				},
			},
			wantError: commontype.ErrGasLimitNil,
		},
		"invalid_precompile_upgrades": {
			// Also see precompile_config_test.go TestVerifyWithChainConfig* tests
			config: ChainConfig{
				FeeConfig: validFeeConfig,
				UpgradeConfig: UpgradeConfig{
					PrecompileUpgrades: []PrecompileUpgrade{
						// same precompile cannot be configured twice for the same timestamp
						{Config: txallowlist.NewDisableConfig(pointer(uint64(1)))},
						{Config: txallowlist.NewDisableConfig(pointer(uint64(1)))},
					},
				},
			},
			wantError: errPrecompileUpgradeInvalidDisable,
		},
		"invalid_state_upgrades": {
			config: ChainConfig{
				FeeConfig: validFeeConfig,
				UpgradeConfig: UpgradeConfig{
					StateUpgrades: []StateUpgrade{
						{BlockTimestamp: nil},
					},
				},
			},
			wantError: errStateUpgradeNilTimestamp,
		},
		"invalid_network_upgrades": {
			config: ChainConfig{
				FeeConfig: validFeeConfig,
				NetworkUpgrades: NetworkUpgrades{
					SubnetEVMTimestamp: nil,
				},
				AvalancheContext: AvalancheContext{SnowCtx: &snow.Context{}},
			},
			wantError: errCannotBeNil,
		},
		"valid": {
			config: ChainConfig{
				FeeConfig: validFeeConfig,
				NetworkUpgrades: NetworkUpgrades{
					SubnetEVMTimestamp: pointer(uint64(1)),
					DurangoTimestamp:   pointer(uint64(2)),
					EtnaTimestamp:      pointer(uint64(3)),
					FortunaTimestamp:   pointer(uint64(4)),
				},
				AvalancheContext: AvalancheContext{SnowCtx: &snow.Context{
					NetworkUpgrades: upgrade.Config{
						DurangoTime: time.Unix(2, 0),
						EtnaTime:    time.Unix(3, 0),
						FortunaTime: time.Unix(4, 0),
					},
				}},
			},
			wantError: nil,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := test.config.Verify()
			require.ErrorIs(t, err, test.wantError)
		})
	}
}

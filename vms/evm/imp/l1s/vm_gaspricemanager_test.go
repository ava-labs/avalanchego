// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/gaspricemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/gastime"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
)

const (
	gasPriceManagerCallGas = 200_000
	// maxTargetExponentDiff is the ACP-176 cap on the per-block change of the
	// target exponent.
	maxTargetExponentDiff = 1 << 15
)

// withGasPriceManager enables the gas price manager at genesis, administered
// by admin and storing cfg.
func withGasPriceManager(admin common.Address, cfg commontype.GasPriceConfig) sutOption {
	return withGenesisPrecompiles(extras.Precompiles{
		gaspricemanager.ConfigKey: &gaspricemanager.Config{
			AllowListConfig: allowlist.AllowListConfig{
				AdminAddresses: []common.Address{admin},
			},
			Upgrade:               precompileconfig.Upgrade{BlockTimestamp: new(testGenesisTime)},
			InitialGasPriceConfig: &cfg,
		},
	})
}

// staticPricingConfig returns a config under which every block's base fee is
// minPrice.
func staticPricingConfig(minPrice uint64) commontype.GasPriceConfig {
	return commontype.GasPriceConfig{
		ValidatorTargetGas: true,
		StaticPricing:      true,
		MinGasPrice:        minPrice,
	}
}

// setGasPriceConfigTx returns a tx calling setGasPriceConfig with cfg.
func setGasPriceConfigTx(tb testing.TB, cfg commontype.GasPriceConfig) *types.DynamicFeeTx {
	tb.Helper()

	data, err := gaspricemanager.PackSetGasPriceConfig(cfg)
	require.NoErrorf(tb, err, "gaspricemanager.PackSetGasPriceConfig(%+v)", cfg)
	return callTx(gaspricemanager.ContractAddress, data, gasPriceManagerCallGas)
}

// headerTargetExponent returns the target exponent in b's header, which MUST
// be set.
func headerTargetExponent(tb testing.TB, b *blocks.Block) dynamic.TargetExponent {
	tb.Helper()

	q := customtypes.GetHeaderExtra(b.Header()).TargetExponent
	require.NotNilf(tb, q, "block %d TargetExponent", b.Height())
	return *q
}

// TestGasPriceManagerSetGasPriceConfigSettlementLag asserts that a
// setGasPriceConfig call MUST NOT change the base fee before its block
// settles.
func TestGasPriceManagerSetGasPriceConfigSettlementLag(t *testing.T) {
	const (
		oldMinPrice = 25_000_000_000
		newMinPrice = 100_000_000_000
	)
	wallet := newWallet(t, 1)
	admin := wallet.Addresses()[0]
	ctx, sut := newSUT(t,
		withMaxAllocFor(admin),
		withGasPriceManager(admin, staticPricingConfig(oldMinPrice)),
	)

	newConfig := staticPricingConfig(newMinPrice)
	steps := []struct {
		name        string
		advance     time.Duration
		setConfig   *commontype.GasPriceConfig
		wantBaseFee uint64
	}{
		{
			name:        "setting_block",
			advance:     blockBuildAdvance,
			setConfig:   &newConfig,
			wantBaseFee: oldMinPrice,
		},
		{
			// A block's base fee comes from the gas config in its parent's
			// header, which was built before anything settled.
			name:        "settling_block",
			advance:     settleAdvance,
			wantBaseFee: oldMinPrice,
		},
		{
			name:        "next_block",
			advance:     blockBuildAdvance,
			wantBaseFee: newMinPrice,
		},
	}
	for _, step := range steps {
		unsigned := transferTx(common.Address{1}, common.Big1)
		if step.setConfig != nil {
			unsigned = setGasPriceConfigTx(t, *step.setConfig)
		}
		blk := sut.buildBlock(ctx, t, step.advance, wallet.SetNonceAndSign(t, 0, unsigned))
		require.Equalf(t, uint256.NewInt(step.wantBaseFee), blk.ExecutedBaseFee(), "%s base fee", step.name)
	}
}

// TestGasPriceManagerValidatorTargetGas asserts that a stored TargetGas MUST
// pin the target exponent when ValidatorTargetGas is false, and that the
// exponent MUST follow the validators' preference when it is true.
func TestGasPriceManagerValidatorTargetGas(t *testing.T) {
	const pinnedTarget = 2 * dynamic.MinTarget
	// desiredTarget is more than 2 steps from the initial exponent, so neither
	// validator step is clamped by it.
	desiredTarget := dynamic.TargetExponent(5 * maxTargetExponentDiff).Target()
	pinnedExponent := dynamic.DesiredTargetExponent(pinnedTarget)

	tests := []struct {
		name   string
		config commontype.GasPriceConfig
		want   []dynamic.TargetExponent
	}{
		{
			name: "pinned",
			config: commontype.GasPriceConfig{
				ValidatorTargetGas: false,
				TargetGas:          pinnedTarget,
				MinGasPrice:        1,
				TimeToDouble:       60,
			},
			want: []dynamic.TargetExponent{
				pinnedExponent,
				pinnedExponent,
			},
		},
		{
			name: "validators",
			config: commontype.GasPriceConfig{
				ValidatorTargetGas: true,
				MinGasPrice:        1,
				TimeToDouble:       60,
			},
			want: []dynamic.TargetExponent{
				maxTargetExponentDiff,
				2 * maxTargetExponentDiff,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wallet := newWallet(t, 1)
			admin := wallet.Addresses()[0]
			ctx, sut := newSUT(t,
				withMaxAllocFor(admin),
				withGasTarget(desiredTarget),
				withGasPriceManager(admin, tt.config),
			)

			for _, want := range tt.want {
				blk := sut.buildBlock(ctx, t, blockBuildAdvance, wallet.SetNonceAndSign(t, 0, transferTx(common.Address{1}, common.Big1)))
				require.Equalf(t, want, headerTargetExponent(t, blk), "block %d target exponent", blk.Height())
			}
		})
	}
}

// TestGasPriceManagerTargetAuthorityTransitions asserts that a change of target
// authority MUST NOT apply before its block settles, and that validators MUST
// resume one bounded step from the pinned exponent.
func TestGasPriceManagerTargetAuthorityTransitions(t *testing.T) {
	const (
		initialPinnedTarget = 2 * dynamic.MinTarget
		finalPinnedTarget   = 3 * dynamic.MinTarget
	)
	desiredTarget := dynamic.TargetExponent(5 * maxTargetExponentDiff).Target()

	wallet := newWallet(t, 1)
	admin := wallet.Addresses()[0]
	ctx, sut := newSUT(t,
		withMaxAllocFor(admin),
		withGasTarget(desiredTarget),
		withGasPriceManager(admin, commontype.GasPriceConfig{
			TargetGas:    initialPinnedTarget,
			MinGasPrice:  1,
			TimeToDouble: 60,
		}),
	)

	validatorConfig := commontype.GasPriceConfig{
		ValidatorTargetGas: true,
		MinGasPrice:        1,
		TimeToDouble:       60,
	}
	finalPinConfig := commontype.GasPriceConfig{
		TargetGas:    finalPinnedTarget,
		MinGasPrice:  1,
		TimeToDouble: 60,
	}
	initialPin := dynamic.DesiredTargetExponent(initialPinnedTarget)
	steps := []struct {
		name               string
		advance            time.Duration
		setConfig          *commontype.GasPriceConfig
		wantTargetExponent dynamic.TargetExponent
	}{
		{
			name:               "set_validator_authority",
			advance:            blockBuildAdvance,
			setConfig:          &validatorConfig,
			wantTargetExponent: initialPin,
		},
		{
			name:               "validator_authority_settled",
			advance:            settleAdvance,
			wantTargetExponent: initialPin - maxTargetExponentDiff,
		},
		{
			name:               "set_final_pin",
			advance:            blockBuildAdvance,
			setConfig:          &finalPinConfig,
			wantTargetExponent: initialPin - 2*maxTargetExponentDiff,
		},
		{
			name:               "final_pin_settled",
			advance:            settleAdvance,
			wantTargetExponent: dynamic.DesiredTargetExponent(finalPinnedTarget),
		},
	}
	for _, step := range steps {
		unsigned := transferTx(common.Address{1}, common.Big1)
		if step.setConfig != nil {
			unsigned = setGasPriceConfigTx(t, *step.setConfig)
		}
		blk := sut.buildBlock(ctx, t, step.advance, wallet.SetNonceAndSign(t, 0, unsigned))
		require.Equalf(t, step.wantTargetExponent, headerTargetExponent(t, blk), "%s target exponent", step.name)
	}
}

// TestGasPriceManagerActivationTransitions asserts that enabling or disabling
// the gas price manager MUST change the gas price config once the change
// settles.
func TestGasPriceManagerActivationTransitions(t *testing.T) {
	const pinnedMinPrice = 100_000_000_000
	pinnedConfig := staticPricingConfig(pinnedMinPrice)
	wantPinnedConfig := gastime.GasPriceConfig{
		TargetToExcessScaling: gastime.DefaultTargetToExcessScaling,
		MinPrice:              pinnedMinPrice,
		StaticPricing:         true,
	}
	// The admin never sends a tx.
	admin := common.Address{'a', 'd', 'm', 'i', 'n'}
	midChain := uint64(testStartTime.Add(2 * blockBuildAdvance).Unix()) //#nosec G115 -- Known non-negative

	tests := []struct {
		name               string
		genesisPrecompiles extras.Precompiles
		upgrade            extras.PrecompileUpgrade
		wantConfigBefore   gastime.GasPriceConfig
		wantConfigAfter    gastime.GasPriceConfig
	}{
		{
			name: "enable",
			upgrade: extras.PrecompileUpgrade{
				Config: &gaspricemanager.Config{
					AllowListConfig: allowlist.AllowListConfig{
						AdminAddresses: []common.Address{admin},
					},
					Upgrade:               precompileconfig.Upgrade{BlockTimestamp: &midChain},
					InitialGasPriceConfig: &pinnedConfig,
				},
			},
			wantConfigBefore: gastime.DefaultGasPriceConfig(),
			wantConfigAfter:  wantPinnedConfig,
		},
		{
			name: "disable",
			genesisPrecompiles: extras.Precompiles{
				gaspricemanager.ConfigKey: &gaspricemanager.Config{
					AllowListConfig: allowlist.AllowListConfig{
						AdminAddresses: []common.Address{admin},
					},
					Upgrade:               precompileconfig.Upgrade{BlockTimestamp: new(testGenesisTime)},
					InitialGasPriceConfig: &pinnedConfig,
				},
			},
			upgrade: extras.PrecompileUpgrade{
				Config: &gaspricemanager.Config{
					Upgrade: precompileconfig.Upgrade{
						BlockTimestamp: &midChain,
						Disable:        true,
					},
				},
			},
			wantConfigBefore: wantPinnedConfig,
			wantConfigAfter:  gastime.DefaultGasPriceConfig(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wallet := newWallet(t, 1)
			ctx, sut := newSUT(t,
				withMaxAllocFor(wallet.Addresses()...),
				withGenesisPrecompiles(tt.genesisPrecompiles),
				withUpgrades(extras.UpgradeConfig{
					PrecompileUpgrades: []extras.PrecompileUpgrade{tt.upgrade},
				}),
			)

			before := sut.buildBlock(ctx, t, blockBuildAdvance, wallet.SetNonceAndSign(t, 0, transferTx(common.Address{1}, common.Big1)))
			_, gotBefore := new(hooks).GasConfigAfter(before.Header())
			require.Equal(t, tt.wantConfigBefore, gotBefore, "gas price config before the upgrade")

			sut.buildBlock(ctx, t, blockBuildAdvance, wallet.SetNonceAndSign(t, 0, transferTx(common.Address{1}, common.Big1))) // activates the upgrade
			settling := sut.buildBlock(ctx, t, settleAdvance, wallet.SetNonceAndSign(t, 0, transferTx(common.Address{1}, common.Big1)))
			_, gotAfter := new(hooks).GasConfigAfter(settling.Header())
			require.Equal(t, tt.wantConfigAfter, gotAfter, "gas price config once the upgrade settles")
		})
	}
}

// TestGasPriceManagerPinnedConfigPersistsAcrossRestart asserts that the gas
// price config MUST survive a restart.
func TestGasPriceManagerPinnedConfigPersistsAcrossRestart(t *testing.T) {
	const minPrice = 25_000_000_000
	wallet := newWallet(t, 1)
	admin := wallet.Addresses()[0]
	// A shared clock keeps the restarted VM's blocks after its last accepted
	// block.
	timeOpt, _ := withVMTime(testStartTime)
	opts := []sutOption{
		withMaxAllocFor(admin),
		withGasPriceManager(admin, staticPricingConfig(minPrice)),
		withDB(memdb.New()),
		withChainDataDir(t.TempDir()),
		timeOpt,
	}

	ctx, sut := newSUT(t, opts...)
	sut.buildBlock(ctx, t, blockBuildAdvance, wallet.SetNonceAndSign(t, 0, transferTx(common.Address{1}, common.Big1)))
	require.NoErrorf(t, sut.Shutdown(ctx), "%T.Shutdown()", sut.VM)

	ctx, sut = newSUT(t, opts...)
	blk := sut.buildBlock(ctx, t, blockBuildAdvance, wallet.SetNonceAndSign(t, 0, transferTx(common.Address{1}, common.Big1)))
	require.Equal(t, uint256.NewInt(minPrice), blk.ExecutedBaseFee(), "base fee after restart")
}

// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"context"
	"testing"

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
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"
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
		TargetGas:          0,
		StaticPricing:      true,
		MinGasPrice:        minPrice,
		TimeToDouble:       0,
	}
}

// buildBlock advances the clock by [blockBuildAdvance] and builds a block
// holding one transfer from account 0 of w.
func (s *SUT) buildBlock(ctx context.Context, tb testing.TB, w *saetest.Wallet) *blocks.Block {
	tb.Helper()

	s.clock.Advance(blockBuildAdvance)
	s.send(ctx, tb, w.SetNonceAndSign(tb, 0, transferTx(common.Address{1}, common.Big1)))
	return s.RunConsensusLoop(ctx, tb)
}

// buildSettlingBlock advances the clock by [settleAdvance] and builds a block,
// holding one transfer from account 0 of w, that settles its parent.
func (s *SUT) buildSettlingBlock(ctx context.Context, tb testing.TB, w *saetest.Wallet) *blocks.Block {
	tb.Helper()

	s.clock.Advance(settleAdvance)
	s.send(ctx, tb, w.SetNonceAndSign(tb, 0, transferTx(common.Address{1}, common.Big1)))
	return s.RunConsensusLoop(ctx, tb)
}

// setGasPriceConfig advances the clock by [blockBuildAdvance] and builds a
// block in which account 0 of w, a gas price manager admin, sets cfg.
func (s *SUT) setGasPriceConfig(ctx context.Context, tb testing.TB, w *saetest.Wallet, cfg commontype.GasPriceConfig) *blocks.Block {
	tb.Helper()

	data, err := gaspricemanager.PackSetGasPriceConfig(cfg)
	require.NoErrorf(tb, err, "gaspricemanager.PackSetGasPriceConfig(%+v)", cfg)
	tx := w.SetNonceAndSign(tb, 0, callTx(gaspricemanager.ContractAddress, data, gasPriceManagerCallGas))

	s.clock.Advance(blockBuildAdvance)
	s.send(ctx, tb, tx)
	b := s.RunConsensusLoop(ctx, tb)
	s.requireReceiptStatus(ctx, tb, tx, types.ReceiptStatusSuccessful)
	return b
}

// headerTargetExponent returns the target exponent in b's header, which MUST
// be set.
func headerTargetExponent(tb testing.TB, b *blocks.Block) dynamic.TargetExponent {
	tb.Helper()

	q := customtypes.GetHeaderExtra(b.Header()).TargetExponent
	require.NotNilf(tb, q, "block %d TargetExponent", b.Height())
	return *q
}

// TestGasPriceManagerStaticPricing asserts that a static pricing config enabled
// at genesis MUST pin every block's base fee, before and after settlement.
func TestGasPriceManagerStaticPricing(t *testing.T) {
	const minPrice = 25_000_000_000
	wallet := newWallet(t, 1)
	admin := wallet.Addresses()[0]
	ctx, sut := newSUT(t,
		withMaxAllocFor(admin),
		withGasPriceManager(admin, staticPricingConfig(minPrice)),
	)

	blks := []*blocks.Block{
		sut.buildBlock(ctx, t, wallet),
		sut.buildSettlingBlock(ctx, t, wallet),
		sut.buildBlock(ctx, t, wallet),
	}
	for _, b := range blks {
		require.Equalf(t, uint256.NewInt(minPrice), b.ExecutedBaseFee(), "block %d base fee", b.Height())
	}
}

// TestGasPriceManagerSetGasPriceConfigSettlementLag asserts that a
// setGasPriceConfig call MUST NOT change the base fee before its block
// settles.
func TestGasPriceManagerSetGasPriceConfigSettlementLag(t *testing.T) {
	const (
		oldMinPrice = 1
		newMinPrice = 100_000_000_000
	)
	wallet := newWallet(t, 1)
	admin := wallet.Addresses()[0]
	ctx, sut := newSUT(t,
		withMaxAllocFor(admin),
		withGasPriceManager(admin, staticPricingConfig(oldMinPrice)),
	)

	setting := sut.setGasPriceConfig(ctx, t, wallet, staticPricingConfig(newMinPrice))
	require.Equal(t, uint256.NewInt(oldMinPrice), setting.ExecutedBaseFee(), "base fee of the setting block")

	// The settling block's base fee comes from the gas clock after its parent,
	// which was built before anything settled.
	settling := sut.buildSettlingBlock(ctx, t, wallet)
	require.Equal(t, uint256.NewInt(oldMinPrice), settling.ExecutedBaseFee(), "base fee of the settling block")

	next := sut.buildBlock(ctx, t, wallet)
	require.Equal(t, uint256.NewInt(newMinPrice), next.ExecutedBaseFee(), "base fee after settlement")
}

// TestGasPriceManagerValidatorTargetGas asserts that a stored TargetGas MUST
// pin the target when ValidatorTargetGas is false, and that the target MUST
// converge to the validators' preference at the ACP-176 rate when it is true.
func TestGasPriceManagerValidatorTargetGas(t *testing.T) {
	// numBlocks MUST cover the rate-limited climb to the desired target plus 2
	// plateau blocks.
	const numBlocks = 8
	// desiredTarget is below pinnedTarget, so the pinned case visibly ignores
	// validators.
	const pinnedTarget = 2 * dynamic.MinTarget
	desiredTarget := dynamic.TargetExponent(5 * maxTargetExponentDiff).Target()

	t.Run("pinned", func(t *testing.T) {
		blks := buildChainWithGasPriceManager(t, desiredTarget, numBlocks, commontype.GasPriceConfig{
			ValidatorTargetGas: false,
			TargetGas:          pinnedTarget,
			MinGasPrice:        1,
			TimeToDouble:       60,
		})

		want := dynamic.DesiredTargetExponent(pinnedTarget)
		for _, b := range blks {
			require.Equalf(t, want, headerTargetExponent(t, b), "block %d target exponent", b.Height())
		}
		last := blks[len(blks)-1]
		require.Equal(t, gas.Gas(pinnedTarget), last.ExecutedByGasTime().Target(), "target after execution")
	})

	t.Run("validators", func(t *testing.T) {
		blks := buildChainWithGasPriceManager(t, desiredTarget, numBlocks, commontype.GasPriceConfig{
			ValidatorTargetGas: true,
			MinGasPrice:        1,
			TimeToDouble:       60,
		})

		desired := dynamic.DesiredTargetExponent(desiredTarget)
		for i, b := range blks {
			want := min(dynamic.TargetExponent(i+1)*maxTargetExponentDiff, desired)
			require.Equalf(t, want, headerTargetExponent(t, b), "block %d target exponent", b.Height())
		}
		last := blks[len(blks)-1]
		require.Equal(t, desiredTarget, last.ExecutedByGasTime().Target(), "target after execution")
	})
}

// buildChainWithGasPriceManager returns n blocks built on a new SUT whose node
// prefers gasTarget, and whose gas price manager stores cfg from genesis.
func buildChainWithGasPriceManager(t *testing.T, gasTarget gas.Gas, n int, cfg commontype.GasPriceConfig) []*blocks.Block {
	t.Helper()

	wallet := newWallet(t, 1)
	admin := wallet.Addresses()[0]
	ctx, sut := newSUT(t,
		withMaxAllocFor(admin),
		withGasTarget(gasTarget),
		withGasPriceManager(admin, cfg),
	)

	blks := make([]*blocks.Block, n)
	for i := range blks {
		blks[i] = sut.buildBlock(ctx, t, wallet)
	}
	return blks
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

	initialExponent := dynamic.DesiredTargetExponent(initialPinnedTarget)
	initial := sut.buildBlock(ctx, t, wallet)
	require.Equal(t, initialExponent, headerTargetExponent(t, initial), "target exponent under the initial pin")

	toValidators := sut.setGasPriceConfig(ctx, t, wallet, commontype.GasPriceConfig{
		ValidatorTargetGas: true,
		MinGasPrice:        1,
		TimeToDouble:       60,
	})
	require.Equal(t, initialExponent, headerTargetExponent(t, toValidators), "target exponent before validator authority settles")

	validatorExponent := initialExponent - maxTargetExponentDiff
	validators := sut.buildSettlingBlock(ctx, t, wallet)
	require.Equal(t, validatorExponent, headerTargetExponent(t, validators), "target exponent once validator authority settles")

	toPinned := sut.setGasPriceConfig(ctx, t, wallet, commontype.GasPriceConfig{
		TargetGas:    finalPinnedTarget,
		MinGasPrice:  1,
		TimeToDouble: 60,
	})
	require.Equal(t, validatorExponent-maxTargetExponentDiff, headerTargetExponent(t, toPinned), "target exponent before the final pin settles")

	pinned := sut.buildSettlingBlock(ctx, t, wallet)
	require.Equal(t, dynamic.DesiredTargetExponent(finalPinnedTarget), headerTargetExponent(t, pinned), "target exponent once the final pin settles")
}

// TestGasPriceManagerActivationTransitions asserts that enabling or disabling
// the gas price manager MUST change the base fee once the change settles.
func TestGasPriceManagerActivationTransitions(t *testing.T) {
	// pinnedMinPrice exceeds the genesis base fee, from which the unpinned base
	// fee decays.
	const pinnedMinPrice = 100_000_000_000
	pinnedCfg := staticPricingConfig(pinnedMinPrice)
	// The admin never sends a tx.
	admin := common.Address{'a', 'd', 'm', 'i', 'n'}
	midChain := testStartTime.Add(2 * blockBuildAdvance)

	tests := []struct {
		name             string
		pinnedAtGenesis  bool
		upgrade          extras.PrecompileUpgrade
		wantPinnedBefore bool
		wantPinnedAfter  bool
	}{
		{
			name: "enable_at_first_block",
			upgrade: extras.PrecompileUpgrade{
				Config: &gaspricemanager.Config{
					AllowListConfig: allowlist.AllowListConfig{
						AdminAddresses: []common.Address{admin},
					},
					Upgrade:               precompileconfig.Upgrade{BlockTimestamp: new(uint64(testStartTime.Unix()))}, //#nosec G115 -- Known non-negative
					InitialGasPriceConfig: &pinnedCfg,
				},
			},
			wantPinnedAfter: true,
		},
		{
			name: "enable_mid_chain",
			upgrade: extras.PrecompileUpgrade{
				Config: &gaspricemanager.Config{
					AllowListConfig: allowlist.AllowListConfig{
						AdminAddresses: []common.Address{admin},
					},
					Upgrade:               precompileconfig.Upgrade{BlockTimestamp: new(uint64(midChain.Unix()))}, //#nosec G115 -- Known non-negative
					InitialGasPriceConfig: &pinnedCfg,
				},
			},
			wantPinnedBefore: false,
			wantPinnedAfter:  true,
		},
		{
			name:            "disable_mid_chain",
			pinnedAtGenesis: true,
			upgrade: extras.PrecompileUpgrade{
				Config: &gaspricemanager.Config{
					Upgrade: precompileconfig.Upgrade{
						BlockTimestamp: new(uint64(midChain.Unix())), //#nosec G115 -- Known non-negative
						Disable:        true,
					},
				},
			},
			wantPinnedBefore: true,
			wantPinnedAfter:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wallet := newWallet(t, 1)
			opts := []sutOption{
				withMaxAllocFor(wallet.Addresses()...),
				withUpgrades(extras.UpgradeConfig{
					PrecompileUpgrades: []extras.PrecompileUpgrade{tt.upgrade},
				}),
			}
			if tt.pinnedAtGenesis {
				opts = append(opts, withGasPriceManager(admin, pinnedCfg))
			}
			ctx, sut := newSUT(t, opts...)

			pinnedBaseFee := uint256.NewInt(pinnedMinPrice)
			// No block precedes an upgrade at the first block.
			if *tt.upgrade.Timestamp() > uint64(testStartTime.Unix()) { //#nosec G115 -- Known non-negative
				before := sut.buildBlock(ctx, t, wallet)
				require.Equalf(t, tt.wantPinnedBefore, before.ExecutedBaseFee().Eq(pinnedBaseFee), "base fee %s pinned before the upgrade", before.ExecutedBaseFee())
			}

			sut.buildBlock(ctx, t, wallet) // activates the upgrade
			sut.buildSettlingBlock(ctx, t, wallet)
			after := sut.buildBlock(ctx, t, wallet)
			require.Equalf(t, tt.wantPinnedAfter, after.ExecutedBaseFee().Eq(pinnedBaseFee), "base fee %s pinned after the upgrade settles", after.ExecutedBaseFee())
		})
	}
}

// TestGasPriceManagerPinnedConfigPersistsAcrossRestart asserts that the gas
// config MUST survive a restart, since it is read only from headers.
func TestGasPriceManagerPinnedConfigPersistsAcrossRestart(t *testing.T) {
	const minPrice = 25_000_000_000
	wallet := newWallet(t, 1)
	admin := wallet.Addresses()[0]
	timeOpt, _ := withVMTime(testStartTime)
	opts := []sutOption{
		withMaxAllocFor(admin),
		withGasPriceManager(admin, staticPricingConfig(minPrice)),
		withDB(memdb.New()),
		withChainDataDir(t.TempDir()),
		timeOpt,
	}

	ctx, sut := newSUT(t, opts...)
	before := sut.buildBlock(ctx, t, wallet)
	require.Equal(t, uint256.NewInt(minPrice), before.ExecutedBaseFee(), "base fee before restart")

	require.NoErrorf(t, sut.Shutdown(ctx), "%T.Shutdown()", sut.VM)
	ctx, sut = newSUT(t, opts...)
	after := sut.buildBlock(ctx, t, wallet)
	require.Equal(t, uint256.NewInt(minPrice), after.ExecutedBaseFee(), "base fee after restart")
}

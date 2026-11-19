// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"context"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rpc"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/gaspricemanager"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"

	saeparams "github.com/ava-labs/avalanchego/vms/evm/params"
)

const (
	gasPriceManagerCallGas = 200_000
	// maxTargetExponentDiff is the ACP-176 cap on the per-block change of the
	// target exponent.
	maxTargetExponentDiff = 1 << 15
)

// fetchStoredGasPriceConfig reads storage directly, so storage assertions don't
// depend on [hooks.GasConfigAfter].
func fetchStoredGasPriceConfig(ctx context.Context, t *testing.T, sut *SUT, blockNumber rpc.BlockNumber) commontype.GasPriceConfig {
	t.Helper()
	stateDB, _, err := sut.GethRPCBackends().StateAndHeaderByNumber(ctx, blockNumber)
	require.NoError(t, err)
	return gaspricemanager.GetStoredGasPriceConfig(stateDB, gaspricemanager.ContractAddress)
}

// settleGasPriceManagerMutation builds and returns a block after the previous
// block has settled.
func settleGasPriceManagerMutation(ctx context.Context, t *testing.T, sut *SUT, wallet *saetest.Wallet) *blocks.Block {
	t.Helper()
	const fromIdx = 1
	sut.clock.Advance(settleAdvance)
	sut.send(ctx, t, wallet, fromIdx, transferTx(wallet.Addresses()[fromIdx], common.Big1))
	return sut.RunConsensusLoop(ctx, t)
}

// withGasPriceManager enables gaspricemanager at genesis, administered by
// admin and storing cfg.
func withGasPriceManager(admin common.Address, cfg *commontype.GasPriceConfig) sutOption {
	return withGenesisPrecompiles(extras.Precompiles{
		gaspricemanager.ConfigKey: gaspricemanager.NewConfig(
			new(testGenesisTime),
			[]common.Address{admin},
			nil,
			nil,
			cfg,
		),
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

// TestGasPriceManagerStaticPricingRuntimeBaseFeeSAE asserts that a static
// pricing config enabled at genesis MUST pin every block's base fee, before and
// after settlement.
func TestGasPriceManagerStaticPricingRuntimeBaseFeeSAE(t *testing.T) {
	const (
		adminIdx = 0
		fromIdx  = 1
		toIdx    = 1
	)

	const pinnedMinPrice uint64 = 25_000_000_000
	cfg := staticPricingConfig(pinnedMinPrice)

	wallet := newWallet(t, 2)
	ctx, sut := newSUT(
		t,
		withMaxAllocFor(wallet.Addresses()...),
		withGasPriceManager(wallet.Addresses()[adminIdx], &cfg),
	)

	require.Equal(t, cfg, fetchStoredGasPriceConfig(ctx, t, sut, rpc.LatestBlockNumber),
		"genesis-stored gaspricemanager config must equal InitialGasPriceConfig")

	wantPinned := uint256.NewInt(pinnedMinPrice)
	for i := range 3 {
		sut.clock.Advance(blockBuildAdvance)
		sut.send(ctx, t, wallet, fromIdx, transferTx(wallet.Addresses()[toIdx], common.Big1))
		built := sut.RunConsensusLoop(ctx, t)
		require.Equal(t, wantPinned, built.ExecutedBaseFee(),
			"static-pricing block %d ExecutedBaseFee", i)
	}

	_ = settleGasPriceManagerMutation(ctx, t, sut, wallet)
	sut.clock.Advance(blockBuildAdvance)
	sut.send(ctx, t, wallet, fromIdx, transferTx(wallet.Addresses()[toIdx], common.Big1))
	post := sut.RunConsensusLoop(ctx, t)
	require.Equal(t, wantPinned, post.ExecutedBaseFee(),
		"static-pricing ExecutedBaseFee must survive Tau settlement")
}

// TestGasPriceManagerSetGasPriceConfigSettlementLagSAE asserts that a
// setGasPriceConfig mutation MUST NOT change the base fee before its block
// settles.
func TestGasPriceManagerSetGasPriceConfigSettlementLagSAE(t *testing.T) {
	const (
		adminIdx = 0
		fromIdx  = 1
	)

	const (
		oldMinPrice uint64 = 1
		newMinPrice uint64 = 100_000_000_000
	)
	initialCfg := staticPricingConfig(oldMinPrice)

	wallet := newWallet(t, 2)
	ctx, sut := newSUT(
		t,
		withMaxAllocFor(wallet.Addresses()...),
		withGasPriceManager(wallet.Addresses()[adminIdx], &initialCfg),
	)

	// Block M mutates the config.
	sut.clock.Advance(blockBuildAdvance)
	newCfg := staticPricingConfig(newMinPrice)
	mutationCalldata, err := gaspricemanager.PackSetGasPriceConfig(newCfg)
	require.NoError(t, err)
	mutationTx := sut.send(ctx, t, wallet, adminIdx, callTx(gaspricemanager.ContractAddress, mutationCalldata, gasPriceManagerCallGas))
	mutationBlock := sut.RunConsensusLoop(ctx, t)
	sut.requireReceiptStatus(ctx, t, mutationTx, types.ReceiptStatusSuccessful)
	require.Equal(t, uint256.NewInt(oldMinPrice), mutationBlock.ExecutedBaseFee(),
		"mutation block ExecutedBaseFee must use pre-mutation floor")

	// M+1 is built after M settles, but its base fee comes from the clock
	// after M, which M built before anything had settled.
	next := settleGasPriceManagerMutation(ctx, t, sut, wallet)
	require.Equal(t, uint256.NewInt(oldMinPrice), next.ExecutedBaseFee(),
		"settle-next block ExecutedBaseFee must still use pre-mutation floor")

	// M+2's base fee comes from the clock after M+1, which M+1 built against
	// M's settled state.
	sut.clock.Advance(blockBuildAdvance)
	sut.send(ctx, t, wallet, fromIdx, transferTx(wallet.Addresses()[fromIdx], common.Big1))
	delivery := sut.RunConsensusLoop(ctx, t)
	require.Equal(t, uint256.NewInt(newMinPrice), delivery.ExecutedBaseFee(),
		"delivery block ExecutedBaseFee must reflect post-settlement new floor")
}

// TestGasPriceManagerValidatorTargetGasSAE asserts that a stored TargetGas
// MUST pin the target when ValidatorTargetGas is false, and that the target
// MUST converge to the validators' preference at the ACP-176 rate when it is
// true.
func TestGasPriceManagerValidatorTargetGasSAE(t *testing.T) {
	const (
		adminIdx = 0
		fromIdx  = 1
		// numBlocks MUST cover the rate-limited climb to desiredTargetExponent
		// plus 2 plateau blocks.
		numBlocks uint64 = 8
	)

	// desiredGasTarget is below overrideTargetGas, so the pinned case visibly
	// ignores validators.
	const overrideTargetGas uint64 = 2 * dynamic.MinTarget
	desiredGasTarget := dynamic.TargetExponent(gas.Gas(5 * maxTargetExponentDiff)).Target()
	desiredTargetExponent := dynamic.DesiredTargetExponent(desiredGasTarget)

	buildChain := func(t *testing.T, cfg commontype.GasPriceConfig) []*blocks.Block {
		t.Helper()
		wallet := newWallet(t, 2)
		ctx, sut := newSUT(
			t,
			withMaxAllocFor(wallet.Addresses()...),
			withGasTarget(desiredGasTarget),
			withGasPriceManager(wallet.Addresses()[adminIdx], &cfg),
		)

		built := make([]*blocks.Block, 0, numBlocks)
		for range numBlocks {
			sut.clock.Advance(blockBuildAdvance)
			sut.send(ctx, t, wallet, fromIdx, transferTx(wallet.Addresses()[fromIdx], common.Big1))
			built = append(built, sut.RunConsensusLoop(ctx, t))
		}
		return built
	}

	gasLimit := func(b *blocks.Block) uint64 { return b.EthBlock().GasLimit() }

	t.Run("precompile_target_pinned", func(t *testing.T) {
		chain := buildChain(t, commontype.GasPriceConfig{
			ValidatorTargetGas: false,
			TargetGas:          overrideTargetGas,
			MinGasPrice:        1,
			TimeToDouble:       60,
		})

		wantTargetExponent := dynamic.DesiredTargetExponent(gas.Gas(overrideTargetGas))
		for i := range numBlocks {
			headerTargetExponent := customtypes.GetHeaderExtra(chain[i].EthBlock().Header()).TargetExponent
			require.NotNil(t, headerTargetExponent, "block %d TargetExponent must be present", i+1)
			require.Equal(t, wantTargetExponent, *headerTargetExponent,
				"block %d TargetExponent must encode the precompile target", i+1)
			if i == 0 {
				continue
			}
			require.Equalf(t, gasLimit(chain[0]), gasLimit(chain[i]),
				"block %d GasLimit must equal block 1 (precompile target pinned)", i+1)
		}
		require.Equal(t, gas.Gas(overrideTargetGas), chain[numBlocks-1].ExecutedByGasTime().Target(),
			"final post-execution gas-time Target must equal precompile-pinned overrideTargetGas")
	})

	t.Run("validator_target_converges", func(t *testing.T) {
		chain := buildChain(t, commontype.GasPriceConfig{
			ValidatorTargetGas: true,
			MinGasPrice:        1,
			TimeToDouble:       60,
		})

		for i := range numBlocks {
			wantTargetExponent := min(
				dynamic.TargetExponent((i+1)*maxTargetExponentDiff),
				desiredTargetExponent,
			)
			headerTargetExponent := customtypes.GetHeaderExtra(chain[i].EthBlock().Header()).TargetExponent
			require.NotNil(t, headerTargetExponent, "block %d TargetExponent must be present", i+1)
			require.Equal(t, wantTargetExponent, *headerTargetExponent,
				"block %d TargetExponent must follow the bounded validator vote", i+1)
			if i == 0 {
				continue
			}
			require.GreaterOrEqualf(t, gasLimit(chain[i]), gasLimit(chain[i-1]),
				"block %d GasLimit must be >= block %d (monotonic toward desired)", i+1, i)
		}
		require.Greater(t, gasLimit(chain[numBlocks-1]), gasLimit(chain[0]),
			"last block GasLimit must exceed block 1 (chain evolved toward desired)")
		require.Equal(t, desiredGasTarget, chain[numBlocks-1].ExecutedByGasTime().Target(),
			"final post-execution gas-time Target must equal desiredGasTarget")
	})
}

// TestGasPriceManagerTargetAuthorityTransitionsSAE asserts that a change of
// target authority MUST NOT apply before its block settles, and that
// validators MUST resume one bounded step from the pinned exponent.
func TestGasPriceManagerTargetAuthorityTransitionsSAE(t *testing.T) {
	const (
		adminIdx = 0
		fromIdx  = 1
	)
	const (
		initialPinnedTarget uint64 = 2 * dynamic.MinTarget
		finalPinnedTarget   uint64 = 3 * dynamic.MinTarget
	)

	desiredTarget := dynamic.TargetExponent(gas.Gas(5 * maxTargetExponentDiff)).Target()
	initialConfig := commontype.GasPriceConfig{
		TargetGas:    initialPinnedTarget,
		MinGasPrice:  1,
		TimeToDouble: 60,
	}

	wallet := newWallet(t, 2)
	ctx, sut := newSUT(
		t,
		withMaxAllocFor(wallet.Addresses()...),
		withGasTarget(desiredTarget),
		withGasPriceManager(wallet.Addresses()[adminIdx], &initialConfig),
	)

	blockTargetExponent := func(t *testing.T, b *blocks.Block) dynamic.TargetExponent {
		t.Helper()
		q := customtypes.GetHeaderExtra(b.EthBlock().Header()).TargetExponent
		require.NotNil(t, q, "block %d TargetExponent must be present", b.Height())
		return *q
	}
	mutate := func(t *testing.T, config commontype.GasPriceConfig) *blocks.Block {
		t.Helper()
		calldata, err := gaspricemanager.PackSetGasPriceConfig(config)
		require.NoError(t, err, "gaspricemanager.PackSetGasPriceConfig(%+v)", config)
		tx := sut.send(ctx, t, wallet, adminIdx, callTx(gaspricemanager.ContractAddress, calldata, gasPriceManagerCallGas))
		b := sut.RunConsensusLoop(ctx, t)
		sut.requireReceiptStatus(ctx, t, tx, types.ReceiptStatusSuccessful)
		return b
	}

	sut.clock.Advance(blockBuildAdvance)
	sut.send(ctx, t, wallet, fromIdx, transferTx(wallet.Addresses()[fromIdx], common.Big1))
	baseline := sut.RunConsensusLoop(ctx, t)
	initialPinnedExponent := dynamic.DesiredTargetExponent(gas.Gas(initialPinnedTarget))
	require.Equal(t, initialPinnedExponent, blockTargetExponent(t, baseline),
		"initial pinned config must set TargetExponent")

	validatorConfig := commontype.GasPriceConfig{
		ValidatorTargetGas: true,
		MinGasPrice:        1,
		TimeToDouble:       60,
	}
	sut.clock.Advance(blockBuildAdvance)
	validatorMutation := mutate(t, validatorConfig)
	require.Equal(t, initialPinnedExponent, blockTargetExponent(t, validatorMutation),
		"validator authority must not apply before the mutation settles")

	validatorActive := settleGasPriceManagerMutation(ctx, t, sut, wallet)
	wantValidatorExponent := initialPinnedExponent - maxTargetExponentDiff
	require.Equal(t, wantValidatorExponent, blockTargetExponent(t, validatorActive),
		"validator authority must resume one bounded step from the pinned parent")

	finalConfig := commontype.GasPriceConfig{
		TargetGas:    finalPinnedTarget,
		MinGasPrice:  1,
		TimeToDouble: 60,
	}
	sut.clock.Advance(blockBuildAdvance)
	pinnedMutation := mutate(t, finalConfig)
	require.Equal(t, wantValidatorExponent-maxTargetExponentDiff, blockTargetExponent(t, pinnedMutation),
		"pinned authority must not apply before the mutation settles")

	pinnedActive := settleGasPriceManagerMutation(ctx, t, sut, wallet)
	finalPinnedExponent := dynamic.DesiredTargetExponent(gas.Gas(finalPinnedTarget))
	require.Equal(t, finalPinnedExponent, blockTargetExponent(t, pinnedActive),
		"pinned authority must replace TargetExponent after the mutation settles")
}

// TestGasPriceManagerActivationTransitionsSAE asserts that enabling or
// disabling gaspricemanager MUST change the base fee once the change settles.
func TestGasPriceManagerActivationTransitionsSAE(t *testing.T) {
	const (
		adminIdx = 0
		fromIdx  = 1
	)
	// pinnedMinPrice exceeds the genesis base fee, from which the unpinned
	// base fee decays.
	const pinnedMinPrice uint64 = 100_000_000_000
	var (
		pinnedCfg     = staticPricingConfig(pinnedMinPrice)
		pinnedBaseFee = uint256.NewInt(pinnedMinPrice)
	)

	now := testStartTime
	midChainActivation := now.Add(saeparams.Tau)

	enableUpgrades := func(admin common.Address, activationTime time.Time) []extras.PrecompileUpgrade {
		return []extras.PrecompileUpgrade{{
			Config: gaspricemanager.NewConfig(
				new(uint64(activationTime.Unix())), // #nosec G115 -- known positive test timestamp
				[]common.Address{admin},
				nil, nil, &pinnedCfg,
			),
		}}
	}
	disableUpgrades := func(_ common.Address, activationTime time.Time) []extras.PrecompileUpgrade {
		return []extras.PrecompileUpgrade{{
			Config: gaspricemanager.NewDisableConfig(new(uint64(activationTime.Unix()))), // #nosec G115 -- known positive test timestamp
		}}
	}

	// baseFeeExpectation sets at most one field. The zero value skips the
	// block.
	type baseFeeExpectation struct {
		equal *uint256.Int
		below *uint256.Int
	}
	check := func(t *testing.T, label string, e baseFeeExpectation, b *blocks.Block) {
		t.Helper()
		switch {
		case e.equal != nil:
			require.Equal(t, e.equal, b.ExecutedBaseFee(), "%s ExecutedBaseFee must equal", label)
		case e.below != nil:
			require.Less(t, b.ExecutedBaseFee().Uint64(), e.below.Uint64(),
				"%s ExecutedBaseFee must be below the ceiling", label)
		}
	}

	cases := []struct {
		name            string
		pinnedAtGenesis bool
		upgrades        func(admin common.Address, activationTime time.Time) []extras.PrecompileUpgrade
		activationTime  time.Time
		preTransition   baseFeeExpectation
		postTransition  baseFeeExpectation
	}{
		{
			name:           "enable_first_async_block",
			upgrades:       enableUpgrades,
			activationTime: now,
			postTransition: baseFeeExpectation{equal: pinnedBaseFee},
		},
		{
			name:           "enable_mid_chain",
			upgrades:       enableUpgrades,
			activationTime: midChainActivation,
			preTransition:  baseFeeExpectation{below: pinnedBaseFee},
			postTransition: baseFeeExpectation{equal: pinnedBaseFee},
		},
		{
			name:            "disable_mid_chain",
			pinnedAtGenesis: true,
			upgrades:        disableUpgrades,
			activationTime:  midChainActivation,
			preTransition:   baseFeeExpectation{equal: pinnedBaseFee},
			// The base fee starts decaying from the pin. Its exact value is
			// gastime's concern.
			postTransition: baseFeeExpectation{below: pinnedBaseFee},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wallet := newWallet(t, 2)
			admin := wallet.Addresses()[adminIdx]
			opts := []sutOption{
				withMaxAllocFor(wallet.Addresses()...),
				withUpgrades(extras.UpgradeConfig{
					PrecompileUpgrades: tc.upgrades(admin, tc.activationTime),
				}),
			}
			if tc.pinnedAtGenesis {
				opts = append(opts, withGasPriceManager(admin, &pinnedCfg))
			}
			ctx, sut := newSUT(t, opts...)

			if tc.preTransition != (baseFeeExpectation{}) {
				require.True(t, tc.activationTime.After(now),
					"test setup: pre-transition assertion requires activationTime > now")
				sut.clock.Advance(blockBuildAdvance)
				sut.send(ctx, t, wallet, fromIdx, transferTx(wallet.Addresses()[fromIdx], common.Big1))
				preTransition := sut.RunConsensusLoop(ctx, t)
				check(t, "pre-transition", tc.preTransition, preTransition)
			}

			// Any tx triggers the activation block.
			sut.clock.Set(tc.activationTime)
			activationTx := sut.send(ctx, t, wallet, adminIdx, deployTx())
			_ = sut.RunConsensusLoop(ctx, t)
			sut.requireReceiptStatus(ctx, t, activationTx, types.ReceiptStatusSuccessful)

			_ = settleGasPriceManagerMutation(ctx, t, sut, wallet)

			sut.clock.Advance(blockBuildAdvance)
			sut.send(ctx, t, wallet, fromIdx, transferTx(wallet.Addresses()[fromIdx], common.Big1))
			delivery := sut.RunConsensusLoop(ctx, t)
			check(t, "delivery", tc.postTransition, delivery)
		})
	}
}

// TestGasPriceManagerPinnedConfigPersistsAcrossRestartSAE asserts that the gas
// config MUST survive a restart, since it is read only from headers.
func TestGasPriceManagerPinnedConfigPersistsAcrossRestartSAE(t *testing.T) {
	const (
		adminIdx = 0
		fromIdx  = 1
	)

	const pinnedMinPrice uint64 = 25_000_000_000
	cfg := staticPricingConfig(pinnedMinPrice)

	wallet := newWallet(t, 2)
	timeOpt, _ := withVMTime(testStartTime)
	opts := []sutOption{
		withMaxAllocFor(wallet.Addresses()...),
		withGasPriceManager(wallet.Addresses()[adminIdx], &cfg),
		withDB(memdb.New()),
		withChainDataDir(t.TempDir()),
		timeOpt,
	}
	ctx, sut := newSUT(t, opts...)

	wantPinned := uint256.NewInt(pinnedMinPrice)

	sut.clock.Advance(blockBuildAdvance)
	sut.send(ctx, t, wallet, fromIdx, transferTx(wallet.Addresses()[fromIdx], common.Big1))
	preRestart := sut.RunConsensusLoop(ctx, t)
	require.Equal(t, wantPinned, preRestart.ExecutedBaseFee(),
		"pre-restart ExecutedBaseFee must reflect pinned MinGasPrice")

	require.NoErrorf(t, sut.Shutdown(ctx), "%T.Shutdown()", sut.VM)
	ctx, sut = newSUT(t, opts...)

	sut.clock.Advance(blockBuildAdvance)
	sut.send(ctx, t, wallet, fromIdx, transferTx(wallet.Addresses()[fromIdx], common.Big1))
	postRestart := sut.RunConsensusLoop(ctx, t)
	require.Equal(t, wantPinned, postRestart.ExecutedBaseFee(),
		"post-restart ExecutedBaseFee must still reflect pinned MinGasPrice (header roundtrip)")
}

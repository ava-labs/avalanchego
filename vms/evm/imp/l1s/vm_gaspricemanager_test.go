// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/rpc"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/gaspricemanager"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/gastime"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
	"github.com/ava-labs/avalanchego/vms/evm/txgossip/txgossiptest"

	legacyparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
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
func fetchStoredGasPriceConfig(t *testing.T, sut *l1SUT, blockNumber rpc.BlockNumber) commontype.GasPriceConfig {
	t.Helper()
	stateDB, _, err := sut.vm.GethRPCBackends().StateAndHeaderByNumber(sut.ctx, blockNumber)
	require.NoError(t, err)
	return gaspricemanager.GetStoredGasPriceConfig(stateDB, gaspricemanager.ContractAddress)
}

// settleGasPriceManagerMutation builds and returns a block after the previous
// block has settled.
func settleGasPriceManagerMutation(t *testing.T, sut *l1SUT) *blocks.Block {
	t.Helper()
	const fromIdx = 1
	sut.advanceTime(t, settleAdvance)
	tx := sut.sendTransferTx(t, fromIdx, fromIdx, common.Big1)
	txgossiptest.WaitUntilPending(t, sut.ctx, sut.vm.GethRPCBackends(), tx)
	return sut.buildAcceptExecuteBlock(t)
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

	now := postIglooStartTime(t)
	sut := newL1SUT(
		t,
		withFork(upgrade.Igloo),
		withNumAccounts(2),
		withNow(now),
		withIglooGenesis(),
		withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
			legacyparams.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
				gaspricemanager.ConfigKey: gaspricemanager.NewConfig(
					new(uint64(0)),
					[]common.Address{addresses[adminIdx]},
					nil,
					nil,
					&cfg,
				),
			}
		}),
	)

	require.Equal(t, cfg, fetchStoredGasPriceConfig(t, sut, rpc.LatestBlockNumber),
		"genesis-stored gaspricemanager config must equal InitialGasPriceConfig")

	wantPinned := uint256.NewInt(pinnedMinPrice)
	for i := 0; i < 3; i++ {
		sut.advanceTime(t, blockBuildAdvance)
		sut.sendTransferTx(t, fromIdx, toIdx, common.Big1)
		built := sut.buildAcceptExecuteBlock(t)
		require.Equal(t, wantPinned, built.ExecutedBaseFee(),
			"static-pricing block %d ExecutedBaseFee", i)
	}

	_ = settleGasPriceManagerMutation(t, sut)
	sut.advanceTime(t, blockBuildAdvance)
	sut.sendTransferTx(t, fromIdx, toIdx, common.Big1)
	post := sut.buildAcceptExecuteBlock(t)
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

	now := postIglooStartTime(t)
	sut := newL1SUT(
		t,
		withFork(upgrade.Igloo),
		withNumAccounts(2),
		withNow(now),
		withIglooGenesis(),
		withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
			legacyparams.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
				gaspricemanager.ConfigKey: gaspricemanager.NewConfig(
					new(uint64(0)),
					[]common.Address{addresses[adminIdx]},
					nil,
					nil,
					&initialCfg,
				),
			}
		}),
	)

	// Block M mutates the config.
	sut.advanceTime(t, blockBuildAdvance)
	newCfg := staticPricingConfig(newMinPrice)
	mutationCalldata, err := gaspricemanager.PackSetGasPriceConfig(newCfg)
	require.NoError(t, err)
	mutationTx := sut.sendCallTx(t, adminIdx, gaspricemanager.ContractAddress, mutationCalldata, gasPriceManagerCallGas)
	mutationBlock := sut.buildAcceptExecuteBlock(t)
	sut.requireTxSucceeded(t, mutationTx)
	require.Equal(t, uint256.NewInt(oldMinPrice), mutationBlock.ExecutedBaseFee(),
		"mutation block ExecutedBaseFee must use pre-mutation floor")

	// M+1 is built after M settles, but its base fee comes from the clock
	// after M, which M built before anything had settled.
	next := settleGasPriceManagerMutation(t, sut)
	require.Equal(t, uint256.NewInt(oldMinPrice), next.ExecutedBaseFee(),
		"settle-next block ExecutedBaseFee must still use pre-mutation floor")

	// M+2's base fee comes from the clock after M+1, which M+1 built against
	// M's settled state.
	sut.advanceTime(t, blockBuildAdvance)
	sut.sendTransferTx(t, fromIdx, fromIdx, common.Big1)
	delivery := sut.buildAcceptExecuteBlock(t)
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
		numBlocks = 8
	)

	// desiredGasTarget is below overrideTargetGas, so the pinned case visibly
	// ignores validators.
	const overrideTargetGas uint64 = 2 * dynamic.MinTarget
	desiredGasTarget := dynamic.TargetExponent(gas.Gas(5 * maxTargetExponentDiff)).Target()
	desiredTargetExponent := dynamic.DesiredTargetExponent(desiredGasTarget)

	buildChain := func(t *testing.T, cfg commontype.GasPriceConfig) []*blocks.Block {
		t.Helper()
		now := postIglooStartTime(t)
		sut := newL1SUT(
			t,
			withFork(upgrade.Igloo),
			withNumAccounts(2),
			withNow(now),
			withGasTarget(desiredGasTarget),
			withIglooGenesis(),
			withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
				legacyparams.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
					gaspricemanager.ConfigKey: gaspricemanager.NewConfig(
						new(uint64(0)),
						[]common.Address{addresses[adminIdx]},
						nil, nil, &cfg,
					),
				}
			}),
		)

		built := make([]*blocks.Block, 0, numBlocks)
		for i := 0; i < numBlocks; i++ {
			sut.advanceTime(t, blockBuildAdvance)
			sut.sendTransferTx(t, fromIdx, fromIdx, common.Big1)
			built = append(built, sut.buildAcceptExecuteBlock(t))
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

		for i := uint64(0); i < numBlocks; i++ {
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

	now := postIglooStartTime(t)
	sut := newL1SUT(
		t,
		withFork(upgrade.Igloo),
		withNumAccounts(2),
		withNow(now),
		withGasTarget(desiredTarget),
		withIglooGenesis(),
		withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
			legacyparams.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
				gaspricemanager.ConfigKey: gaspricemanager.NewConfig(
					new(uint64(0)),
					[]common.Address{addresses[adminIdx]},
					nil, nil, &initialConfig,
				),
			}
		}),
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
		tx := sut.sendCallTx(t, adminIdx, gaspricemanager.ContractAddress, calldata, gasPriceManagerCallGas)
		b := sut.buildAcceptExecuteBlock(t)
		sut.requireTxSucceeded(t, tx)
		return b
	}

	sut.advanceTime(t, blockBuildAdvance)
	sut.sendTransferTx(t, fromIdx, fromIdx, common.Big1)
	baseline := sut.buildAcceptExecuteBlock(t)
	initialPinnedExponent := dynamic.DesiredTargetExponent(gas.Gas(initialPinnedTarget))
	require.Equal(t, initialPinnedExponent, blockTargetExponent(t, baseline),
		"initial pinned config must set TargetExponent")

	validatorConfig := commontype.GasPriceConfig{
		ValidatorTargetGas: true,
		MinGasPrice:        1,
		TimeToDouble:       60,
	}
	sut.advanceTime(t, blockBuildAdvance)
	validatorMutation := mutate(t, validatorConfig)
	require.Equal(t, initialPinnedExponent, blockTargetExponent(t, validatorMutation),
		"validator authority must not apply before the mutation settles")

	validatorActive := settleGasPriceManagerMutation(t, sut)
	wantValidatorExponent := initialPinnedExponent - maxTargetExponentDiff
	require.Equal(t, wantValidatorExponent, blockTargetExponent(t, validatorActive),
		"validator authority must resume one bounded step from the pinned parent")

	finalConfig := commontype.GasPriceConfig{
		TargetGas:    finalPinnedTarget,
		MinGasPrice:  1,
		TimeToDouble: 60,
	}
	sut.advanceTime(t, blockBuildAdvance)
	pinnedMutation := mutate(t, finalConfig)
	require.Equal(t, wantValidatorExponent-maxTargetExponentDiff, blockTargetExponent(t, pinnedMutation),
		"pinned authority must not apply before the mutation settles")

	pinnedActive := settleGasPriceManagerMutation(t, sut)
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
	const (
		pinnedMinPrice uint64 = 25_000_000_000
	)
	var (
		pinnedCfg       = staticPricingConfig(pinnedMinPrice)
		pinnedBaseFee   = uint256.NewInt(pinnedMinPrice)
		defaultBaseFeeU = uint256.NewInt(uint64(gastime.DefaultMinPrice))
	)

	now := postIglooStartTime(t)
	midChainActivation := now.Add(saeparams.Tau)

	pinnedGenesisPrecompiles := func(addresses []common.Address) extras.Precompiles {
		return extras.Precompiles{
			gaspricemanager.ConfigKey: gaspricemanager.NewConfig(
				new(uint64(0)),
				[]common.Address{addresses[adminIdx]},
				nil, nil, &pinnedCfg,
			),
		}
	}
	enableUpgrades := func(addresses []common.Address, activationTime time.Time) []extras.PrecompileUpgrade {
		return []extras.PrecompileUpgrade{{
			Config: gaspricemanager.NewConfig(
				new(uint64(activationTime.Unix())), // #nosec G115 -- known positive test timestamp
				[]common.Address{addresses[adminIdx]},
				nil, nil, &pinnedCfg,
			),
		}}
	}
	disableUpgrades := func(_ []common.Address, activationTime time.Time) []extras.PrecompileUpgrade {
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
		name               string
		genesisPrecompiles func(addresses []common.Address) extras.Precompiles
		upgrades           func(addresses []common.Address, activationTime time.Time) []extras.PrecompileUpgrade
		activationTime     time.Time
		preTransition      baseFeeExpectation
		postTransition     baseFeeExpectation
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
			preTransition:  baseFeeExpectation{equal: defaultBaseFeeU},
			postTransition: baseFeeExpectation{equal: pinnedBaseFee},
		},
		{
			name:               "disable_mid_chain",
			genesisPrecompiles: pinnedGenesisPrecompiles,
			upgrades:           disableUpgrades,
			activationTime:     midChainActivation,
			preTransition:      baseFeeExpectation{equal: pinnedBaseFee},
			// The base fee starts decaying from the pin. Its exact value is
			// gastime's concern.
			postTransition: baseFeeExpectation{below: pinnedBaseFee},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := []l1SUTOption{
				withFork(upgrade.Igloo),
				withNumAccounts(2),
				withNow(now),
				withUpgradeConfig(func(addresses []common.Address) []byte {
					return marshalJSON(t, &extras.UpgradeConfig{
						PrecompileUpgrades: tc.upgrades(addresses, tc.activationTime),
					})
				}),
			}
			if tc.genesisPrecompiles != nil {
				opts = append(opts,
					withIglooGenesis(),
					withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
						legacyparams.GetExtra(genesis.Config).GenesisPrecompiles = tc.genesisPrecompiles(addresses)
					}),
				)
			}
			sut := newL1SUT(t, opts...)

			if tc.preTransition != (baseFeeExpectation{}) {
				require.True(t, tc.activationTime.After(now),
					"test setup: pre-transition assertion requires activationTime > now")
				sut.advanceTime(t, blockBuildAdvance)
				sut.sendTransferTx(t, fromIdx, fromIdx, common.Big1)
				preTransition := sut.buildAcceptExecuteBlock(t)
				check(t, "pre-transition", tc.preTransition, preTransition)
			}

			// Any tx triggers the activation block.
			sut.setTime(t, tc.activationTime)
			activationTx := sut.sendDeployTx(t, adminIdx)
			_ = sut.buildAcceptExecuteBlock(t)
			sut.requireTxSucceeded(t, activationTx)

			_ = settleGasPriceManagerMutation(t, sut)

			sut.advanceTime(t, blockBuildAdvance)
			sut.sendTransferTx(t, fromIdx, fromIdx, common.Big1)
			delivery := sut.buildAcceptExecuteBlock(t)
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

	now := postIglooStartTime(t)
	sut := newL1SUT(
		t,
		withFork(upgrade.Igloo),
		withNumAccounts(2),
		withNow(now),
		withIglooGenesis(),
		withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
			legacyparams.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
				gaspricemanager.ConfigKey: gaspricemanager.NewConfig(
					new(uint64(0)),
					[]common.Address{addresses[adminIdx]},
					nil,
					nil,
					&cfg,
				),
			}
		}),
	)

	wantPinned := uint256.NewInt(pinnedMinPrice)

	sut.advanceTime(t, blockBuildAdvance)
	sut.sendTransferTx(t, fromIdx, fromIdx, common.Big1)
	preRestart := sut.buildAcceptExecuteBlock(t)
	require.Equal(t, wantPinned, preRestart.ExecutedBaseFee(),
		"pre-restart ExecutedBaseFee must reflect pinned MinGasPrice")

	sut.restart(t)

	sut.advanceTime(t, blockBuildAdvance)
	sut.sendTransferTx(t, fromIdx, fromIdx, common.Big1)
	postRestart := sut.buildAcceptExecuteBlock(t)
	require.Equal(t, wantPinned, postRestart.ExecutedBaseFee(),
		"post-restart ExecutedBaseFee must still reflect pinned MinGasPrice (header roundtrip)")
}

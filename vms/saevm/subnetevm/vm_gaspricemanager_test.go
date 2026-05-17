// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

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
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/gaspricemanager"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/subnetevm/hook/acp176"

	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

const gasPriceManagerCallGas = 200_000

// fetchStoredGasPriceConfig reads the precompile's stored config at
// `blockNumber` directly from the StateDB. `eth_call` would also work via the
// runtime gas-config read path, but going through the StateDB keeps these
// assertions independent of `Points.GasConfigAfter` so a regression in the
// runtime path doesn't mask a storage-write regression.
func fetchStoredGasPriceConfig(t *testing.T, sut *SUT, blockNumber rpc.BlockNumber) commontype.GasPriceConfig {
	t.Helper()
	stateDB, _, err := sut.vm.GethRPCBackends().StateAndHeaderByNumber(sut.ctx, blockNumber)
	require.NoError(t, err)
	return gaspricemanager.GetStoredGasPriceConfig(stateDB, gaspricemanager.ContractAddress)
}

// settleGasPriceManagerMutation drives the chain forward by Tau-plus-a-second
// of wall-clock time and builds the next block, so the mutation in the
// previously-built block becomes visible via subsequent builds' `parent.Root`.
// Returns the next block so callers can assert [blocks.Block.ExecutedBaseFee]
// on the settlement block itself. `settleAdvance` is the shared Tau+1s cushion
// defined in [vm_rewardmanager_test.go].
func settleGasPriceManagerMutation(t *testing.T, sut *SUT, fromIdx int) *blocks.Block {
	t.Helper()
	sut.advanceTime(t, settleAdvance)
	sut.sendTransferTx(t, fromIdx, fromIdx, common.Big1)
	return sut.buildAcceptExecuteBlock(t)
}

// staticPricingConfig returns a [commontype.GasPriceConfig] with [StaticPricing]
// pinned to [minPrice]. Used by the runtime-path tests so that
// `header.BaseFee == minPrice` is a deterministic, settlement-aware assertion
// (under static pricing the gastime excess is zeroed each block, and the
// `MinPrice` floor in [gastime.Time.Price] is the only contributor).
func staticPricingConfig(minPrice uint64) commontype.GasPriceConfig {
	return commontype.GasPriceConfig{
		ValidatorTargetGas: true, // header-controlled target is irrelevant under static pricing
		TargetGas:          0,
		StaticPricing:      true,
		MinGasPrice:        minPrice,
		TimeToDouble:       0,
	}
}

// TestGasPriceManagerStaticPricingRuntimeBaseFeeSAE genesis-enables
// `gaspricemanager` with a static-pricing config pinning `MinGasPrice` and
// asserts that every subsequent block's `header.BaseFee` equals the pinned
// value. This locks in the end-to-end runtime path: MarkSynchronous reads
// genesis state for the bootstrap clock, and per-block GasConfigAfter calls
// read the settled state at `header.Root` to keep the clock pinned.
func TestGasPriceManagerStaticPricingRuntimeBaseFeeSAE(t *testing.T) {
	const (
		adminIdx = 0
		fromIdx  = 1
		toIdx    = 1
	)

	const pinnedMinPrice uint64 = 25_000_000_000
	cfg := staticPricingConfig(pinnedMinPrice)

	now := postHeliconStartTime(t)
	sut := newSUT(
		t,
		withFork(upgradetest.Helicon),
		withNumAccounts(2),
		withNow(now),
		withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
			subnetevmparams.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
				gaspricemanager.ConfigKey: gaspricemanager.NewConfig(
					utils.PointerTo[uint64](0),
					[]common.Address{addresses[adminIdx]},
					nil,
					nil,
					&cfg,
				),
			}
		}),
	)

	// Sanity-check that `InitialGasPriceConfig` from `GenesisPrecompiles`
	// has been seeded into precompile storage exactly as configured. This
	// is the only place this assertion lives now that the storage-CRUD
	// tests have been folded into runtime-pricing tests.
	require.Equal(t, cfg, fetchStoredGasPriceConfig(t, sut, rpc.LatestBlockNumber),
		"genesis-stored gaspricemanager config must equal InitialGasPriceConfig")

	// Build several blocks separated by `blockBuildAdvance` and assert that
	// every one of them carries the pinned `BaseFee`. We deliberately include
	// blocks both before and after a Tau-sized settlement window to confirm
	// that settled-state reads don't accidentally flip the chain off the
	// static-pricing floor.
	wantPinned := uint256.NewInt(pinnedMinPrice)
	for i := 0; i < 3; i++ {
		sut.advanceTime(t, blockBuildAdvance)
		sut.sendTransferTx(t, fromIdx, toIdx, common.Big1)
		built := sut.buildAcceptExecuteBlock(t)
		require.Equal(t, wantPinned, built.ExecutedBaseFee(),
			"static-pricing block %d ExecutedBaseFee", i)
	}

	// Cross a settlement boundary and rebuild; the pin must still hold.
	_ = settleGasPriceManagerMutation(t, sut, fromIdx)
	sut.advanceTime(t, blockBuildAdvance)
	sut.sendTransferTx(t, fromIdx, toIdx, common.Big1)
	post := sut.buildAcceptExecuteBlock(t)
	require.Equal(t, wantPinned, post.ExecutedBaseFee(),
		"static-pricing ExecutedBaseFee must survive Tau settlement")
}

// TestGasPriceManagerSetGasPriceConfigSettlementLagSAE asserts that a
// `setGasPriceConfig` mutation lands in the gas clock only AFTER the
// mutating block has settled. Concretely: genesis seeds a permissive static
// config (`MinGasPrice=1`); admin then sets a new static config with a
// much larger `MinGasPrice` in block M; blocks before M settles continue to
// see the old floor; the first build after the settle helper observes the
// new floor. The exact block where the change first appears is M+2 (M+1 is
// the next built by the settle helper using gas-clock state inherited
// from M, whose own AfterBlock read state at genesis.Root).
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

	now := postHeliconStartTime(t)
	sut := newSUT(
		t,
		withFork(upgradetest.Helicon),
		withNumAccounts(2),
		withNow(now),
		withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
			subnetevmparams.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
				gaspricemanager.ConfigKey: gaspricemanager.NewConfig(
					utils.PointerTo[uint64](0),
					[]common.Address{addresses[adminIdx]},
					nil,
					nil,
					&initialCfg,
				),
			}
		}),
	)

	// Block M: admin tx that mutates the precompile storage. The block
	// itself is built with BaseFee derived from the pre-mutation clock.
	sut.advanceTime(t, blockBuildAdvance)
	newCfg := staticPricingConfig(newMinPrice)
	mutationCalldata, err := gaspricemanager.PackSetGasPriceConfig(newCfg)
	require.NoError(t, err)
	mutationTx := sut.sendCallTx(t, adminIdx, gaspricemanager.ContractAddress, mutationCalldata, gasPriceManagerCallGas)
	mutationBlock := sut.buildAcceptExecuteBlock(t)
	sut.requireTxSucceeded(t, mutationTx)
	require.Equal(t, uint256.NewInt(oldMinPrice), mutationBlock.ExecutedBaseFee(),
		"mutation block ExecutedBaseFee must use pre-mutation floor")

	// Settle helper builds the next block (M+1). Its BaseFee was
	// computed at build time from the gas clock that ran AfterBlock(M)
	// against settled state at genesis.Root (since at M's build, the
	// chain's lastSettled was still genesis). So M+1 still sees the OLD
	// floor.
	next := settleGasPriceManagerMutation(t, sut, fromIdx)
	require.Equal(t, uint256.NewInt(oldMinPrice), next.ExecutedBaseFee(),
		"settle-next block ExecutedBaseFee must still use pre-mutation floor")

	// Delivery build (M+2): this block's BaseFee was computed from gas
	// clock state after `AfterBlock(M+1)`. M+1 was built when lastSettled
	// had advanced to M (the settle helper waited Tau+1s); its header.Root
	// is M's post-execution state -- which has the NEW precompile config.
	// So AfterBlock(M+1) installs the new config, and M+2's BaseFee
	// reflects the new floor.
	sut.advanceTime(t, blockBuildAdvance)
	sut.sendTransferTx(t, fromIdx, fromIdx, common.Big1)
	delivery := sut.buildAcceptExecuteBlock(t)
	require.Equal(t, uint256.NewInt(newMinPrice), delivery.ExecutedBaseFee(),
		"delivery block ExecutedBaseFee must reflect post-settlement new floor")
}

// TestGasPriceManagerValidatorTargetGasSAE pins the precompile's
// `ValidatorTargetGas` flag and asserts that the gas-target resolution
// path differs as documented, via two subtests:
//
//   - `precompile_target_pinned`: `ValidatorTargetGas=false` makes the
//     chain ignore `header.TargetExcess` evolution; every block's GasLimit
//     is fixed to the precompile-stored `TargetGas`, and the post-execution
//     gas-time Target equals the precompile pin exactly.
//   - `validator_target_converges`: `ValidatorTargetGas=true` makes the
//     chain follow `header.TargetExcess.Target()`, which is rate-limited
//     toward `DesiredTargetExcess(Config.GasTarget)` per
//     [acp176.MaxTargetExcessDiff] per block. GasLimit climbs monotonically
//     and the chain converges to `desiredGasTarget` exactly.
//
// Both subtests use the validator's `Config.GasTarget` as the convergence
// pull; the override case must visibly ignore it.
func TestGasPriceManagerValidatorTargetGasSAE(t *testing.T) {
	const (
		adminIdx = 0
		fromIdx  = 1
		// numBlocks must be enough for the VTG=true chain's
		// `header.TargetExcess` to reach `desiredGasTarget`'s desired
		// excess under the `MaxTargetExcessDiff`/block rate limit AND leave
		// at least 2 plateau blocks at the end to assert convergence.
		numBlocks = 8
	)

	// `overrideTargetGas` is 2x `MinTargetPerSecond`; `desiredGasTarget` is
	// the gas target produced by `5 * MaxTargetExcessDiff` of excess
	// (~1.005x `MinTargetPerSecond`). It is well below `overrideTargetGas`
	// so the override case must visibly ignore the validator's pull.
	const overrideTargetGas uint64 = 2 * acp176.MinTargetPerSecond
	desiredGasTarget := acp176.TargetExcess(5 * acp176.MaxTargetExcessDiff).Target()

	buildChain := func(t *testing.T, cfg commontype.GasPriceConfig) []*blocks.Block {
		t.Helper()
		now := postHeliconStartTime(t)
		sut := newSUT(
			t,
			withFork(upgradetest.Helicon),
			withNumAccounts(2),
			withNow(now),
			withGasTarget(desiredGasTarget),
			withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
				subnetevmparams.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
					gaspricemanager.ConfigKey: gaspricemanager.NewConfig(
						utils.PointerTo[uint64](0),
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

		for i := 1; i < numBlocks; i++ {
			require.Equalf(t, gasLimit(chain[0]), gasLimit(chain[i]),
				"block %d GasLimit must equal block 1 (precompile target pinned)", i+1)
		}
		require.Equal(t, gas.Gas(overrideTargetGas), chain[numBlocks-1].ExecutedByGasTime().Target(),
			"final post-execution gas-time Target must equal precompile-pinned overrideTargetGas")
	})

	t.Run("validator_target_converges", func(t *testing.T) {
		chain := buildChain(t, commontype.GasPriceConfig{
			ValidatorTargetGas: true,
			TargetGas:          0, // required to be 0 when ValidatorTargetGas=true
			MinGasPrice:        1,
			TimeToDouble:       60,
		})

		// GasLimit climbs monotonically toward `desiredGasTarget` (rate-limited
		// per block) and plateaus once header.TargetExcess catches up.
		for i := 1; i < numBlocks; i++ {
			require.GreaterOrEqualf(t, gasLimit(chain[i]), gasLimit(chain[i-1]),
				"block %d GasLimit must be >= block %d (monotonic toward desired)", i+1, i)
		}
		require.Greater(t, gasLimit(chain[numBlocks-1]), gasLimit(chain[0]),
			"last block GasLimit must exceed block 1 (chain evolved toward desired)")
		// Final convergence: the post-execution gas-time Target equals
		// `desiredGasTarget` (the canonical "where did the chain converge to"
		// signal, independent of any GasLimit-to-Target multiplier).
		require.Equal(t, desiredGasTarget, chain[numBlocks-1].ExecutedByGasTime().Target(),
			"final post-execution gas-time Target must equal desiredGasTarget")
	})
}

// TestGasPriceManagerActivationTransitionsSAE walks the runtime-pricing
// effect of a precompile activation across three timings, asserting
// `ExecutedBaseFee` on both the pre-transition block (when present) and the
// post-settle delivery block. Each case explicitly configures
// `GenesisPrecompiles` and `PrecompileUpgrades`; the runner derives the
// activation timestamp from `activationOffset` so the chain can be driven
// past the activation deterministically.
//
//   - `enable_first_async_block`: chain starts without gaspricemanager;
//     `PrecompileUpgrades` activates it with `pinnedCfg` at the timestamp
//     of the first async block. Locks in the artifact-writeback path when
//     the activation height is the very first SAE-executed block.
//   - `enable_mid_chain`: chain starts without gaspricemanager; activation
//     fires `Tau` after genesis. Locks in the same path for `h > 0`.
//   - `disable_mid_chain`: chain starts with `pinnedCfg` at genesis;
//     `PrecompileUpgrades` disables it `Tau` after genesis. Locks in the
//     `SelfDestruct + Finalise` storage-wipe path: post-settlement
//     `GasConfigAfter` observes zero-valued storage and the chain begins
//     decaying back toward ACP-176 defaults.
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
		defaultBaseFeeU = uint256.NewInt(acp176.MinPrice)
	)

	now := postHeliconStartTime(t)
	midChainActivation := now.Add(saeparams.Tau)

	pinnedGenesisPrecompiles := func(addresses []common.Address) extras.Precompiles {
		return extras.Precompiles{
			gaspricemanager.ConfigKey: gaspricemanager.NewConfig(
				utils.PointerTo[uint64](0),
				[]common.Address{addresses[adminIdx]},
				nil, nil, &pinnedCfg,
			),
		}
	}
	enableUpgrades := func(addresses []common.Address, activationTime time.Time) []extras.PrecompileUpgrade {
		return []extras.PrecompileUpgrade{{
			Config: gaspricemanager.NewConfig(
				utils.PointerTo(uint64(activationTime.Unix())),
				[]common.Address{addresses[adminIdx]},
				nil, nil, &pinnedCfg,
			),
		}}
	}
	disableUpgrades := func(_ []common.Address, activationTime time.Time) []extras.PrecompileUpgrade {
		return []extras.PrecompileUpgrade{{
			Config: gaspricemanager.NewDisableConfig(utils.PointerTo(uint64(activationTime.Unix()))),
		}}
	}

	// baseFeeExpectation declares what to assert against a block's
	// `ExecutedBaseFee`. Exactly one of `equal` / `below` is set per
	// non-zero expectation; the zero value skips the block entirely.
	type baseFeeExpectation struct {
		equal *uint256.Int // ExecutedBaseFee == equal
		below *uint256.Int // ExecutedBaseFee.Uint64() < below.Uint64()
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
		name string
		// genesisPrecompiles installs the genesis precompile set (may be nil).
		genesisPrecompiles func(addresses []common.Address) extras.Precompiles
		// upgrades returns the `PrecompileUpgrades` slice for the case,
		// given the addresses and the case's `activationTime`.
		upgrades func(addresses []common.Address, activationTime time.Time) []extras.PrecompileUpgrade
		// activationTime is the upgrade activation timestamp AND the clock
		// the runner advances to before triggering the activation block.
		activationTime time.Time
		// preTransition asserts the pre-transition block's `ExecutedBaseFee`.
		// Zero value skips the pre-transition block entirely.
		preTransition baseFeeExpectation
		// postTransition asserts the post-settle block's `ExecutedBaseFee`.
		postTransition baseFeeExpectation
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
			// Post-disable the chain transitions back to ACP-176 dynamic
			// pricing and `ExecutedBaseFee` begins decaying from the pinned
			// floor. The exact one-block decay is gas-time math-sensitive,
			// so we assert the strictly-less-than the prior pin.
			postTransition: baseFeeExpectation{below: pinnedBaseFee},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sut := newSUT(
				t,
				withFork(upgradetest.Helicon),
				withNumAccounts(2),
				withNow(now),
				withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
					if tc.genesisPrecompiles == nil {
						return
					}
					subnetevmparams.GetExtra(genesis.Config).GenesisPrecompiles = tc.genesisPrecompiles(addresses)
				}),
				withUpgradeConfig(func(addresses []common.Address) []byte {
					return mustMarshalJSON(t, &extras.UpgradeConfig{
						PrecompileUpgrades: tc.upgrades(addresses, tc.activationTime),
					})
				}),
			)

			// Pre-transition block: built only when the case sets a
			// pre-transition expectation. Cases whose activation fires at
			// the first async block have nothing to assert pre-transition,
			// so they leave `preTransition` as the zero expectation.
			if tc.preTransition != (baseFeeExpectation{}) {
				require.True(t, tc.activationTime.After(now),
					"test setup: pre-transition assertion requires activationTime > now")
				sut.advanceTime(t, blockBuildAdvance)
				sut.sendTransferTx(t, fromIdx, fromIdx, common.Big1)
				preTransition := sut.buildAcceptExecuteBlock(t)
				check(t, "pre-transition", tc.preTransition, preTransition)
			}

			// Activation block. The deploy tx is just a vehicle to trigger
			// the SAE block-build path; the activator runs in
			// `BeforeExecutingBlock`, so the activation block's BaseFee is
			// sealed against the pre-activation gas clock.
			sut.setTime(t, tc.activationTime)
			activationTx := sut.sendDeployTx(t, adminIdx)
			_ = sut.buildAcceptExecuteBlock(t)
			sut.requireTxSucceeded(t, activationTx)

			// Settle past the activation block so subsequent builds see
			// `xdb[activationHeight].hookArtifact` (or its absence) at
			// their `SettledHeight`-keyed lookup.
			_ = settleGasPriceManagerMutation(t, sut, fromIdx)

			// Delivery: built after settlement has crossed the activation
			// block. Pricing now reflects the new precompile state.
			sut.advanceTime(t, blockBuildAdvance)
			sut.sendTransferTx(t, fromIdx, fromIdx, common.Big1)
			delivery := sut.buildAcceptExecuteBlock(t)
			check(t, "delivery", tc.postTransition, delivery)
		})
	}
}

// TestGasPriceManagerPinnedConfigPersistsAcrossRestartSAE pins a static
// gaspricemanager config at genesis, builds a settled block, then restarts
// the VM against the same backing database and asserts that the next block
// still carries the pinned `BaseFee`. Locks in the artifact-persistence
// roundtrip across `MarkSynchronous` -> `markExecuted` -> restart ->
// `GasConfigAfter` -> `blocks.HookArtifact`.
func TestGasPriceManagerPinnedConfigPersistsAcrossRestartSAE(t *testing.T) {
	const (
		adminIdx = 0
		fromIdx  = 1
	)

	const pinnedMinPrice uint64 = 25_000_000_000
	cfg := staticPricingConfig(pinnedMinPrice)

	now := postHeliconStartTime(t)
	sut := newSUT(
		t,
		withFork(upgradetest.Helicon),
		withNumAccounts(2),
		withNow(now),
		withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
			subnetevmparams.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
				gaspricemanager.ConfigKey: gaspricemanager.NewConfig(
					utils.PointerTo[uint64](0),
					[]common.Address{addresses[adminIdx]},
					nil,
					nil,
					&cfg,
				),
			}
		}),
	)

	wantPinned := uint256.NewInt(pinnedMinPrice)

	// Build a block pre-restart and confirm the pinned floor.
	sut.advanceTime(t, blockBuildAdvance)
	sut.sendTransferTx(t, fromIdx, fromIdx, common.Big1)
	preRestart := sut.buildAcceptExecuteBlock(t)
	require.Equal(t, wantPinned, preRestart.ExecutedBaseFee(),
		"pre-restart ExecutedBaseFee must reflect pinned MinGasPrice")

	// Restart: shuts down the current VM, then reinitialises a fresh VM
	// against the same `baseDB`. On the new boot, `MarkSynchronous` reads
	// the genesis state to seed the bootstrap gas clock, and subsequent
	// builds rely on the previously-persisted hook artifact in `xdb`.
	sut.restart(t)

	sut.advanceTime(t, blockBuildAdvance)
	sut.sendTransferTx(t, fromIdx, fromIdx, common.Big1)
	postRestart := sut.buildAcceptExecuteBlock(t)
	require.Equal(t, wantPinned, postRestart.ExecutedBaseFee(),
		"post-restart ExecutedBaseFee must still reflect pinned MinGasPrice (artifact roundtrip)")
}

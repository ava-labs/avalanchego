// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/constants"
	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/rewardmanager"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

// blockBuildAdvance must exceed ACP-226's minDelay (~2s at initial delay excess).
var blockBuildAdvance = 3 * time.Second

// settleAdvance must exceed `Tau` so the previous block becomes
// last-settled and its state is visible via `parent.Root` of a future build.
var settleAdvance = saeparams.Tau + time.Second

const rewardManagerCallGas = 100_000

// tipPerGas: only the priority fee is credited to `header.Coinbase`; base
// fee is burned independently. Pick non-zero so credits are observable.
var tipPerGas = big.NewInt(1 * subnetevmparams.GWei)

// expectedTipCredit is the credit a fee recipient gains per tipped 21_000-gas transfer.
var expectedTipCredit = new(big.Int).Mul(big.NewInt(21_000), tipPerGas)

// signTippedTransferTxTo signs an EIP-1559 transfer with `GasTipCap =
// tipPerGas`. `GasFeeCap` is set well above `BaseFee` so `effectiveTip
// = GasTipCap` and the credit to `header.Coinbase` is exactly
// `21_000 * tipPerGas`.
func signTippedTransferTxTo(t *testing.T, sut *SUT, from int, to common.Address, value *big.Int) *types.Transaction {
	t.Helper()

	return sut.ethWallet.SetNonceAndSign(t, from, &types.DynamicFeeTx{
		To:        &to,
		Gas:       21_000,
		GasFeeCap: big.NewInt(225 * subnetevmparams.GWei),
		GasTipCap: tipPerGas,
		Value:     value,
	})
}

// sendTippedTransferTxTo signs and broadcasts. `to` MUST NOT be a keychain
// account: those are pre-funded with `MaxUint256`, and `AddBalance` wraps
// mod 2^256, which would silently break later admission checks.
func sendTippedTransferTxTo(t *testing.T, sut *SUT, from int, to common.Address, value *big.Int) *types.Transaction {
	t.Helper()

	tx := signTippedTransferTxTo(t, sut, from, to, value)
	require.NoError(t, sut.ethClient.SendTransaction(sut.ctx, tx))
	return tx
}

// dustSink receives tipped transfers (see [sendTippedTransferTxTo]). Its
// balance is irrelevant; only fee-routing deltas on validator / rewardAddr
// / BlackholeAddr are asserted.
var dustSink = common.HexToAddress("0x0000000000000000000000000000000000d05ea1")

func sendRewardManagerTx(t *testing.T, sut *SUT, from int, calldata []byte) *types.Transaction {
	t.Helper()
	return sut.sendCallTx(t, from, rewardmanager.ContractAddress, calldata, rewardManagerCallGas)
}

func balanceOf(t *testing.T, sut *SUT, addr common.Address) *big.Int {
	t.Helper()
	bal, err := sut.ethClient.BalanceAt(sut.ctx, addr, nil)
	require.NoError(t, err)
	return bal
}

// settleRewardManagerMutation makes a previously-included rewardmanager
// state mutation visible to subsequent builds. SAE's parent.Root is the
// post-execution root of the LAST-SETTLED block at parent build time, not
// of the parent itself, so a mutation in block N is only readable once N
// has settled (≥ Tau later) and another block has built atop it.
func settleRewardManagerMutation(t *testing.T, sut *SUT, fromIdx int) {
	t.Helper()
	sut.advanceTime(t, settleAdvance)
	sendTippedTransferTxTo(t, sut, fromIdx, dustSink, common.Big1)
	_ = sut.buildAndAcceptBlock(t)
}

// TestRewardManagerDefaultBurnSAE: no rewardmanager precompile,
// `AllowFeeRecipients=false`. Tip MUST burn (route to BlackholeAddr).
func TestRewardManagerDefaultBurnSAE(t *testing.T) {
	const fromIdx = 0

	now := postHeliconStartTime(t)
	sut := newSUT(
		t,
		withFork(upgradetest.Helicon),
		withNumAccounts(1),
		withNow(now),
	)

	preBurn := balanceOf(t, sut, constants.BlackholeAddr)

	tx := sendTippedTransferTxTo(t, sut, fromIdx, dustSink, common.Big1)
	block := sut.buildAndAcceptBlock(t)
	requireBlockContainsTxs(t, block, tx.Hash())
	sut.requireTxSucceeded(t, tx)

	postBurn := balanceOf(t, sut, constants.BlackholeAddr)
	delta := new(big.Int).Sub(postBurn, preBurn)
	require.Zerof(t, delta.Cmp(expectedTipCredit),
		"BlackholeAddr must receive exactly tip*gasUsed (want=%s got=%s)",
		expectedTipCredit, delta)
}

// TestRewardManagerGenesisAllowFeeRecipientsSAE: `AllowFeeRecipients=true`,
// no rewardmanager precompile. Tip MUST route to `Config.FeeRecipient`.
func TestRewardManagerGenesisAllowFeeRecipientsSAE(t *testing.T) {
	const fromIdx = 0

	validator := common.HexToAddress("0x00000000000000000000000000000000ABcDef01")
	now := postHeliconStartTime(t)

	sut := newSUT(
		t,
		withFork(upgradetest.Helicon),
		withNumAccounts(1),
		withNow(now),
		withFeeRecipient(validator),
		withGenesisConfig(func(genesis *core.Genesis, _ []common.Address) {
			subnetevmparams.GetExtra(genesis.Config).AllowFeeRecipients = true
		}),
	)

	preValidator := balanceOf(t, sut, validator)
	preBurn := balanceOf(t, sut, constants.BlackholeAddr)

	tx := sendTippedTransferTxTo(t, sut, fromIdx, dustSink, common.Big1)
	block := sut.buildAndAcceptBlock(t)
	requireBlockContainsTxs(t, block, tx.Hash())
	sut.requireTxSucceeded(t, tx)

	postValidator := balanceOf(t, sut, validator)
	postBurn := balanceOf(t, sut, constants.BlackholeAddr)

	gain := new(big.Int).Sub(postValidator, preValidator)
	require.Zerof(t, gain.Cmp(expectedTipCredit),
		"validator must receive tip*gasUsed (want=%s got=%s)",
		expectedTipCredit, gain)
	require.Zerof(t, postBurn.Cmp(preBurn),
		"BlackholeAddr balance must be unchanged when fee recipients are allowed")
}

// TestRewardManagerPrecompileUpgradesSAE: walks rewardmanager state
// transitions and asserts each one re-routes fees correctly.
//
//   - setRewardAddress(rewardAddr) => fees route to rewardAddr.
//   - disableRewards()             => fees burn.
//   - allowFeeRecipients()         => fees route to `Config.FeeRecipient`.
//
// Every intermediate state-mutation block must be settled (see
// [settleRewardManagerMutation]) before the next "delivery" build can
// observe the new routing.
//
// Implicitly checks worst-case-vs-execution parity: build + verify use
// `BuildHeader`'s prediction and `core.ApplyTransaction`'s actual coinbase
// respectively; a mismatch would fail [SUT.buildAndAcceptBlock] before
// the balance asserts run.
func TestRewardManagerPrecompileUpgradesSAE(t *testing.T) {
	const (
		adminIdx    = 0
		nonAdminIdx = 1
	)

	validator := common.HexToAddress("0x00000000000000000000000000000000ABcDef01")
	rewardAddr := common.HexToAddress("0x00000000000000000000000000000000DeadBeef")
	now := postHeliconStartTime(t)

	sut := newSUT(
		t,
		withFork(upgradetest.Helicon),
		withNumAccounts(2),
		withNow(now),
		withFeeRecipient(validator),
		withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
			subnetevmparams.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
				rewardmanager.ConfigKey: rewardmanager.NewConfig(
					utils.PointerTo[uint64](0),
					[]common.Address{addresses[adminIdx]},
					nil,
					nil,
					nil,
				),
			}
		}),
	)

	require.True(t, sut.isPrecompileEnabledAtLatest(rewardmanager.ContractAddress),
		"rewardmanager must be enabled at genesis")

	// Each step (a) submits an admin rewardmanager mutation and waits for
	// it to settle, then (b) sends a tipped delivery tx whose tip MUST
	// land on `wantRecipient` and leave the other two recipients
	// unchanged. Steps run in order; state persists.
	type step struct {
		name          string
		mutationFn    func() ([]byte, error)
		wantRecipient common.Address
	}
	steps := []step{
		{
			name:          "setRewardAddress_routes_to_rewardAddr",
			mutationFn:    func() ([]byte, error) { return rewardmanager.PackSetRewardAddress(rewardAddr) },
			wantRecipient: rewardAddr,
		},
		{
			name:          "disableRewards_burns",
			mutationFn:    rewardmanager.PackDisableRewards,
			wantRecipient: constants.BlackholeAddr,
		},
		{
			name:          "allowFeeRecipients_routes_to_validator",
			mutationFn:    rewardmanager.PackAllowFeeRecipients,
			wantRecipient: validator,
		},
	}

	recipients := []common.Address{rewardAddr, validator, constants.BlackholeAddr}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			sut.advanceTime(t, blockBuildAdvance)
			mutation, err := s.mutationFn()
			require.NoError(t, err)
			mutationTx := sendRewardManagerTx(t, sut, adminIdx, mutation)
			_ = sut.buildAndAcceptBlock(t)
			sut.requireTxSucceeded(t, mutationTx)

			settleRewardManagerMutation(t, sut, adminIdx)

			pre := make(map[common.Address]*big.Int, len(recipients))
			for _, r := range recipients {
				pre[r] = balanceOf(t, sut, r)
			}

			sut.advanceTime(t, blockBuildAdvance)
			deliverTx := sendTippedTransferTxTo(t, sut, adminIdx, dustSink, common.Big1)
			_ = sut.buildAndAcceptBlock(t)
			sut.requireTxSucceeded(t, deliverTx)

			for _, r := range recipients {
				delta := new(big.Int).Sub(balanceOf(t, sut, r), pre[r])
				want := new(big.Int)
				if r == s.wantRecipient {
					want = expectedTipCredit
				}
				require.Zerof(t, delta.Cmp(want),
					"recipient=%s want delta=%s got delta=%s", r, want, delta)
			}
		})
	}

	t.Run("non_admin_mutation_rejected", func(t *testing.T) {
		sut.advanceTime(t, blockBuildAdvance)
		data, err := rewardmanager.PackSetRewardAddress(common.HexToAddress("0xdead"))
		require.NoError(t, err)
		rejectTx := sendRewardManagerTx(t, sut, nonAdminIdx, data)
		_ = sut.buildAndAcceptBlock(t)
		sut.requireTxFailed(t, rejectTx)
	})
}

// TestRewardManagerForgedCoinbaseFailsVerifyBlock asserts SAE's
// rebuild-and-compare path in [VM.VerifyBlock] rejects blocks whose
// `Coinbase` deviates from the value the rebuilder would deterministically
// stamp. This is what makes the deterministic branches of `resolveCoinbase`
// enforceable -- without it a malicious builder could redirect fees to an
// arbitrary address in the pinned arms.
//
// Two pinned arms exercised here:
//   - vanilla Helicon (`AllowFeeRecipients=false`, no rewardmanager) =>
//     deterministic [BlackholeAddr]; any other Coinbase is forged.
//   - rewardmanager active with stored `allowFeeRecipients == false` =>
//     deterministic stored reward address; any other Coinbase is forged.
func TestRewardManagerForgedCoinbaseFailsVerifyBlock(t *testing.T) {
	const adminIdx = 0
	rewardAddr := common.HexToAddress("0x00000000000000000000000000000000DeadBeef")

	tests := []struct {
		name string
		opts []sutOption
		// forgedCoinbase MUST differ from what `resolveCoinbase` would
		// deterministically stamp at build time, so the rebuilt block hashes
		// differently and `VerifyBlock` returns a hash-mismatch error.
		forgedCoinbase common.Address
	}{
		{
			name:           "no_precompile_no_AllowFeeRecipients_must_be_blackhole",
			forgedCoinbase: common.HexToAddress("0xfeedfacefeedfacefeedfacefeedfacefeedface"),
		},
		{
			// Pre-populate the precompile's reward-address slot at genesis
			// via `InitialRewardConfig.RewardAddress`, so the deterministic
			// branch produces `rewardAddr` from the very first block (no
			// admin-tx + settle dance needed). We then forge with
			// `BlackholeAddr` (a different but plausible value) to show
			// the verifier doesn't accept it.
			name: "rewardmanager_active_must_be_stored_reward_address",
			opts: []sutOption{
				withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
					subnetevmparams.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
						rewardmanager.ConfigKey: rewardmanager.NewConfig(
							utils.PointerTo[uint64](0),
							[]common.Address{addresses[adminIdx]},
							nil,
							nil,
							&rewardmanager.InitialRewardConfig{RewardAddress: rewardAddr},
						),
					}
				}),
			},
			forgedCoinbase: constants.BlackholeAddr,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := postHeliconStartTime(t)
			opts := append([]sutOption{
				withFork(upgradetest.Helicon),
				withNumAccounts(1),
				withNow(now),
			}, test.opts...)
			sut := newSUT(t, opts...)

			// Build an honest block (verify but DO NOT accept; we want the
			// forged twin to be admissible at the same height), then forge
			// by replacing its header's Coinbase. We re-use the body via
			// [types.Block.WithSeal] so the only divergence is the header
			// (and thus the hash).
			sut.advanceTime(t, blockBuildAdvance)
			_ = sendTippedTransferTxTo(t, sut, adminIdx, dustSink, common.Big1)
			honest := sut.buildAndVerifyBlock(t, nil)

			honestBlk := honest.EthBlock()
			honestHdr := honestBlk.Header()
			require.NotEqual(t, test.forgedCoinbase, honestHdr.Coinbase,
				"sanity: forged Coinbase must actually differ from the honest one")

			forgedHdr := types.CopyHeader(honestHdr)
			forgedHdr.Coinbase = test.forgedCoinbase
			forgedBlk := honestBlk.WithSeal(forgedHdr)

			forged, err := blocks.New(forgedBlk, nil, nil, saetest.NewTBLogger(t, logging.Warn))
			require.NoError(t, err)

			err = sut.vm.VerifyBlock(sut.ctx, nil, forged)
			require.Error(t, err, "VerifyBlock MUST reject a block with a forged Coinbase")
			require.Contains(t, err.Error(), "hash mismatch",
				"forged Coinbase must trip SAE's rebuild-and-compare hash check")
		})
	}
}

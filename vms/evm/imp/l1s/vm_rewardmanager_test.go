// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"context"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/rewardmanager"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/imp/blackhole"
	"github.com/ava-labs/avalanchego/vms/evm/sae"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"

	ethparams "github.com/ava-labs/libevm/params"
)

const rewardManagerCallGas = 100_000

// tipPerGas: only the priority fee is credited to `header.Coinbase`; base
// fee is burned independently. Pick non-zero so credits are observable.
var tipPerGas = big.NewInt(1 * ethparams.GWei)

// expectedTipCredit is the credit a fee recipient gains per tipped 21_000-gas transfer.
var expectedTipCredit = new(big.Int).Mul(big.NewInt(21_000), tipPerGas)

// tippedTransferTx returns a transfer with `GasTipCap = tipPerGas`. Its
// `GasFeeCap` is well above `BaseFee`, so `effectiveTip = GasTipCap` and the
// credit to `header.Coinbase` is exactly `21_000 * tipPerGas`. `to` MUST NOT
// be a wallet account: those are pre-funded with `MaxUint256`, and
// `AddBalance` wraps mod 2^256, which would silently break later admission
// checks.
func tippedTransferTx(to common.Address, value *big.Int) *types.DynamicFeeTx {
	tx := transferTx(to, value)
	tx.GasTipCap = tipPerGas
	return tx
}

// dustSink receives tipped transfers (see [tippedTransferTx]). Its balance is
// irrelevant; only fee-routing deltas on validator / rewardAddr /
// BlackholeAddr are asserted.
var dustSink = common.HexToAddress("0x0000000000000000000000000000000000d05ea1")

func balanceOf(ctx context.Context, t *testing.T, sut *SUT, addr common.Address) *big.Int {
	t.Helper()
	bal, err := sut.EthClient().BalanceAt(ctx, addr, nil)
	require.NoError(t, err)
	return bal
}

// settleRewardManagerMutation makes a previously-included rewardmanager
// state mutation visible to subsequent builds. SAE's parent.Root is the
// post-execution root of the LAST-SETTLED block at parent build time, not
// of the parent itself, so a mutation in block N is only readable once N
// has settled (≥ Tau later) and another block has built atop it.
func settleRewardManagerMutation(ctx context.Context, t *testing.T, sut *SUT, wallet *saetest.Wallet, fromIdx int) {
	t.Helper()
	sut.clock.Advance(settleAdvance)
	sut.send(ctx, t, wallet.SetNonceAndSign(t, fromIdx, tippedTransferTx(dustSink, common.Big1)))
	_ = sut.RunConsensusLoop(ctx, t)
}

// requireBlockContainsTxs asserts that block contains exactly want, in any
// order.
func requireBlockContainsTxs(t *testing.T, block *blocks.Block, want ...common.Hash) {
	t.Helper()

	got := make(map[common.Hash]struct{}, len(block.Transactions()))
	for _, tx := range block.Transactions() {
		got[tx.Hash()] = struct{}{}
	}
	require.Lenf(t, got, len(want), "block must contain exactly %d txs, got %d", len(want), len(got))
	for _, h := range want {
		_, ok := got[h]
		require.Truef(t, ok, "block must contain tx %#x", h)
	}
}

// TestRewardManagerDefaultBurnSAE: no rewardmanager precompile,
// `AllowFeeRecipients=false`. Tip MUST burn (route to BlackholeAddr).
func TestRewardManagerDefaultBurnSAE(t *testing.T) {
	const fromIdx = 0

	wallet := newWallet(t, 1)
	ctx, sut := newSUT(t, withMaxAllocFor(wallet.Addresses()...))

	preBurn := balanceOf(ctx, t, sut, blackhole.Address)

	tx := wallet.SetNonceAndSign(t, fromIdx, tippedTransferTx(dustSink, common.Big1))
	sut.send(ctx, t, tx)
	block := sut.RunConsensusLoop(ctx, t)
	requireBlockContainsTxs(t, block, tx.Hash())
	sut.requireReceiptStatus(ctx, t, tx, types.ReceiptStatusSuccessful)

	postBurn := balanceOf(ctx, t, sut, blackhole.Address)
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

	wallet := newWallet(t, 1)
	ctx, sut := newSUT(
		t,
		withMaxAllocFor(wallet.Addresses()...),
		withFeeRecipient(validator),
		withAllowFeeRecipients(),
	)

	preValidator := balanceOf(ctx, t, sut, validator)
	preBurn := balanceOf(ctx, t, sut, blackhole.Address)

	tx := wallet.SetNonceAndSign(t, fromIdx, tippedTransferTx(dustSink, common.Big1))
	sut.send(ctx, t, tx)
	block := sut.RunConsensusLoop(ctx, t)
	requireBlockContainsTxs(t, block, tx.Hash())
	sut.requireReceiptStatus(ctx, t, tx, types.ReceiptStatusSuccessful)

	postValidator := balanceOf(ctx, t, sut, validator)
	postBurn := balanceOf(ctx, t, sut, blackhole.Address)

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
// respectively; a mismatch would fail block verification before the balance
// asserts run.
func TestRewardManagerPrecompileUpgradesSAE(t *testing.T) {
	const (
		adminIdx    = 0
		nonAdminIdx = 1
	)

	validator := common.HexToAddress("0x00000000000000000000000000000000ABcDef01")
	rewardAddr := common.HexToAddress("0x00000000000000000000000000000000DeadBeef")

	wallet := newWallet(t, 2)
	ctx, sut := newSUT(
		t,
		withMaxAllocFor(wallet.Addresses()...),
		withFeeRecipient(validator),
		withGenesisPrecompiles(extras.Precompiles{
			rewardmanager.ConfigKey: rewardmanager.NewConfig(
				new(uint64(0)),
				[]common.Address{wallet.Addresses()[adminIdx]},
				nil,
				nil,
				nil,
			),
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
			wantRecipient: blackhole.Address,
		},
		{
			name:          "allowFeeRecipients_routes_to_validator",
			mutationFn:    rewardmanager.PackAllowFeeRecipients,
			wantRecipient: validator,
		},
	}

	recipients := []common.Address{rewardAddr, validator, blackhole.Address}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			sut.clock.Advance(blockBuildAdvance)
			mutation, err := s.mutationFn()
			require.NoError(t, err)
			mutationTx := wallet.SetNonceAndSign(t, adminIdx, callTx(rewardmanager.ContractAddress, mutation, rewardManagerCallGas))
			sut.send(ctx, t, mutationTx)
			_ = sut.RunConsensusLoop(ctx, t)
			sut.requireReceiptStatus(ctx, t, mutationTx, types.ReceiptStatusSuccessful)

			settleRewardManagerMutation(ctx, t, sut, wallet, adminIdx)

			pre := make(map[common.Address]*big.Int, len(recipients))
			for _, r := range recipients {
				pre[r] = balanceOf(ctx, t, sut, r)
			}

			sut.clock.Advance(blockBuildAdvance)
			deliverTx := wallet.SetNonceAndSign(t, adminIdx, tippedTransferTx(dustSink, common.Big1))
			sut.send(ctx, t, deliverTx)
			_ = sut.RunConsensusLoop(ctx, t)
			sut.requireReceiptStatus(ctx, t, deliverTx, types.ReceiptStatusSuccessful)

			for _, r := range recipients {
				delta := new(big.Int).Sub(balanceOf(ctx, t, sut, r), pre[r])
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
		sut.clock.Advance(blockBuildAdvance)
		data, err := rewardmanager.PackSetRewardAddress(common.HexToAddress("0xdead"))
		require.NoError(t, err)
		rejectTx := wallet.SetNonceAndSign(t, nonAdminIdx, callTx(rewardmanager.ContractAddress, data, rewardManagerCallGas))
		sut.send(ctx, t, rejectTx)
		_ = sut.RunConsensusLoop(ctx, t)
		sut.requireReceiptStatus(ctx, t, rejectTx, types.ReceiptStatusFailed)
	})
}

// TestRewardManagerCoinbaseVerifyBlock asserts that [VM.VerifyBlock] MUST
// reject a block whose Coinbase differs from the one the chain determines, and
// MUST accept the received Coinbase when builders choose it.
//
// The chain determines the Coinbase when:
//   - AllowFeeRecipients is false and rewardmanager is inactive, so it is
//     [blackhole.Address].
//   - rewardmanager is active with allowFeeRecipients false, so it is the
//     stored reward address.
func TestRewardManagerCoinbaseVerifyBlock(t *testing.T) {
	const adminIdx = 0
	rewardAddr := common.HexToAddress("0x00000000000000000000000000000000DeadBeef")
	// Each subtest starts a new chain, so it needs a wallet with fresh nonces
	// for the same accounts.
	keychain := saetest.NewUNSAFEKeyChain(t, 1)
	admin := keychain.Addresses()[adminIdx]

	tests := []struct {
		name string
		opts []sutOption
		// coinbase MUST differ from the one this node builds with, so the
		// received block is not the one this node would build.
		coinbase common.Address
		wantErr  error
	}{
		{
			name:     "no_precompile_no_AllowFeeRecipients_must_be_blackhole",
			coinbase: common.HexToAddress("0xfeedfacefeedfacefeedfacefeedfacefeedface"),
			wantErr:  sae.ErrHashMismatch,
		},
		{
			// Seeding the reward address at genesis makes the first block
			// pay it, without an admin tx that must settle first.
			name: "rewardmanager_active_must_be_stored_reward_address",
			opts: []sutOption{
				withGenesisPrecompiles(extras.Precompiles{
					rewardmanager.ConfigKey: rewardmanager.NewConfig(
						new(uint64(0)),
						[]common.Address{admin},
						nil,
						nil,
						&rewardmanager.InitialRewardConfig{RewardAddress: rewardAddr},
					),
				}),
			},
			coinbase: blackhole.Address,
			wantErr:  sae.ErrHashMismatch,
		},
		{
			name: "AllowFeeRecipients_accepts_received_coinbase",
			opts: []sutOption{
				withFeeRecipient(common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")),
				withAllowFeeRecipients(),
			},
			coinbase: common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wallet := saetest.NewWalletWithKeyChain(keychain, types.LatestSignerForChainID(big.NewInt(testChainID)))
			opts := append([]sutOption{withMaxAllocFor(admin)}, test.opts...)
			ctx, sut := newSUT(t, opts...)

			// Verify but don't accept the honest block, so its twin is
			// admissible at the same height. [types.Block.WithSeal] keeps the
			// body, so the twin differs only in its header.
			sut.clock.Advance(blockBuildAdvance)
			sut.send(ctx, t, wallet.SetNonceAndSign(t, adminIdx, tippedTransferTx(dustSink, common.Big1)))
			honest := sut.BuildVerify(ctx, t, sut.LastAcceptedID(ctx, t))

			honestBlk := honest.EthBlock()
			honestHdr := honestBlk.Header()
			require.NotEqual(t, test.coinbase, honestHdr.Coinbase,
				"sanity: received Coinbase must differ from the honest one")

			receivedHdr := types.CopyHeader(honestHdr)
			receivedHdr.Coinbase = test.coinbase
			receivedBlk := honestBlk.WithSeal(receivedHdr)

			// ParseBlock builds the block with the VM's own hooks, as for a
			// peer-delivered block.
			receivedBytes, err := rlp.EncodeToBytes(receivedBlk)
			require.NoError(t, err, "rlp.EncodeToBytes(received block)")
			received, err := sut.ParseBlock(ctx, receivedBytes)
			require.NoError(t, err, "sut.ParseBlock(received block)")

			err = sut.VerifyBlock(ctx, nil, received)
			require.ErrorIs(t, err, test.wantErr, "sut.VerifyBlock(received block)")
		})
	}
}

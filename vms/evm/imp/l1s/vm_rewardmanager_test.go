// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/rewardmanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/vms/evm/imp/blackhole"
	"github.com/ava-labs/avalanchego/vms/evm/sae"
)

const rewardManagerCallGas = 100_000

// TestCoinbaseWithoutRewardManager asserts that, without the reward manager,
// blocks MUST pay the blackhole unless AllowFeeRecipients lets them pay the
// node's fee recipient.
func TestCoinbaseWithoutRewardManager(t *testing.T) {
	feeRecipient := common.Address{'f', 'e', 'e'}

	tests := []struct {
		name               string
		allowFeeRecipients bool
		wantCoinbase       common.Address
	}{
		{
			name:         "burn_by_default",
			wantCoinbase: blackhole.Address,
		},
		{
			name:               "allow_fee_recipients",
			allowFeeRecipients: true,
			wantCoinbase:       feeRecipient,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wallet := newWallet(t, 1)
			opts := []sutOption{
				withMaxAllocFor(wallet.Addresses()...),
				withFeeRecipient(feeRecipient),
			}
			if tt.allowFeeRecipients {
				opts = append(opts, withAllowFeeRecipients())
			}
			ctx, sut := newSUT(t, opts...)

			blk := sut.buildBlock(ctx, t, blockBuildAdvance, wallet.SetNonceAndSign(t, 0, transferTx(common.Address{1}, common.Big1)))
			require.Equal(t, tt.wantCoinbase, blk.Header().Coinbase, "coinbase")
		})
	}
}

// TestRewardManagerCallsSetCoinbase asserts that each reward manager call MUST
// set the Coinbase of blocks built once the call settles.
func TestRewardManagerCallsSetCoinbase(t *testing.T) {
	feeRecipient := common.Address{'f', 'e', 'e'}
	rewardAddr := common.Address{'r', 'e', 'w', 'a', 'r', 'd'}

	wallet := newWallet(t, 1)
	admin := wallet.Addresses()[0]
	ctx, sut := newSUT(t,
		withMaxAllocFor(admin),
		withFeeRecipient(feeRecipient),
		withGenesisPrecompiles(extras.Precompiles{
			rewardmanager.ConfigKey: &rewardmanager.Config{
				AllowListConfig: allowlist.AllowListConfig{
					AdminAddresses: []common.Address{admin},
				},
				Upgrade: precompileconfig.Upgrade{
					BlockTimestamp: new(uint64(0)),
				},
			},
		}),
	)

	setRewardAddress, err := rewardmanager.PackSetRewardAddress(rewardAddr)
	require.NoErrorf(t, err, "rewardmanager.PackSetRewardAddress(%s)", rewardAddr)
	disableRewards, err := rewardmanager.PackDisableRewards()
	require.NoError(t, err, "rewardmanager.PackDisableRewards()")
	allowFeeRecipients, err := rewardmanager.PackAllowFeeRecipients()
	require.NoError(t, err, "rewardmanager.PackAllowFeeRecipients()")

	steps := []struct {
		name         string
		call         []byte
		wantCoinbase common.Address
	}{
		{
			name:         "setRewardAddress",
			call:         setRewardAddress,
			wantCoinbase: rewardAddr,
		},
		{
			name:         "disableRewards",
			call:         disableRewards,
			wantCoinbase: blackhole.Address,
		},
		{
			name:         "allowFeeRecipients",
			call:         allowFeeRecipients,
			wantCoinbase: feeRecipient,
		},
	}
	for _, s := range steps {
		sut.buildBlock(ctx, t, blockBuildAdvance, wallet.SetNonceAndSign(t, 0, callTx(rewardmanager.ContractAddress, s.call, rewardManagerCallGas)))
		settling := sut.buildBlock(ctx, t, settleAdvance, wallet.SetNonceAndSign(t, 0, transferTx(common.Address{1}, common.Big1)))
		require.Equalf(t, s.wantCoinbase, settling.Header().Coinbase, "%s coinbase", s.name)
	}
}

// TestRewardManagerCoinbaseVerifyBlock asserts that [VM.VerifyBlock] MUST
// reject a Coinbase other than the one the chain determines, and MUST accept
// any Coinbase when AllowFeeRecipients lets builders choose it.
func TestRewardManagerCoinbaseVerifyBlock(t *testing.T) {
	admin := common.Address{'a', 'd', 'm', 'i', 'n'}
	receivedCoinbase := common.Address{'r', 'e', 'c', 'e', 'i', 'v', 'e', 'd'}

	tests := []struct {
		name               string
		genesisPrecompiles extras.Precompiles
		allowFeeRecipients bool
		wantErr            error
	}{
		{
			name:    "burn_by_default",
			wantErr: sae.ErrHashMismatch,
		},
		{
			name: "stored_reward_address",
			genesisPrecompiles: extras.Precompiles{
				rewardmanager.ConfigKey: &rewardmanager.Config{
					AllowListConfig: allowlist.AllowListConfig{
						AdminAddresses: []common.Address{admin},
					},
					Upgrade: precompileconfig.Upgrade{
						BlockTimestamp: new(uint64(0)),
					},
					InitialRewardConfig: &rewardmanager.InitialRewardConfig{
						RewardAddress: common.Address{'r', 'e', 'w', 'a', 'r', 'd'},
					},
				},
			},
			wantErr: sae.ErrHashMismatch,
		},
		{
			name:               "allow_fee_recipients",
			allowFeeRecipients: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wallet := newWallet(t, 1)
			opts := []sutOption{
				withMaxAllocFor(wallet.Addresses()...),
				// Building with the blackhole fee recipient logs a warning,
				// which fails the test.
				withFeeRecipient(common.Address{'f', 'e', 'e'}),
				withGenesisPrecompiles(tt.genesisPrecompiles),
			}
			if tt.allowFeeRecipients {
				opts = append(opts, withAllowFeeRecipients())
			}
			ctx, sut := newSUT(t, opts...)

			// Verify but don't accept the honest block, so its twin is
			// admissible at the same height.
			sut.clock.Advance(blockBuildAdvance)
			sut.send(ctx, t, wallet.SetNonceAndSign(t, 0, transferTx(common.Address{1}, common.Big1)))
			honest := sut.BuildVerify(ctx, t, sut.LastAcceptedID(ctx, t))

			honestBlk := honest.EthBlock()
			honestHdr := honestBlk.Header()
			require.NotEqual(t, receivedCoinbase, honestHdr.Coinbase, "honest coinbase")

			receivedHdr := types.CopyHeader(honestHdr)
			receivedHdr.Coinbase = receivedCoinbase
			receivedBlk := honestBlk.WithSeal(receivedHdr)

			receivedBytes, err := rlp.EncodeToBytes(receivedBlk)
			require.NoError(t, err, "rlp.EncodeToBytes(received block)")
			received, err := sut.ParseBlock(ctx, receivedBytes)
			require.NoError(t, err, "sut.ParseBlock(received block)")

			err = sut.VerifyBlock(ctx, nil, received)
			require.ErrorIs(t, err, tt.wantErr, "sut.VerifyBlock(received block)")
		})
	}
}

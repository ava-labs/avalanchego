// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/vmerrors"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"
)

// withTxAllowList enables the tx allowlist at genesis, administered by admin.
func withTxAllowList(admin common.Address) sutOption {
	return withGenesisPrecompiles(extras.Precompiles{
		txallowlist.ConfigKey: &txallowlist.Config{
			AllowListConfig: allowlist.AllowListConfig{
				AdminAddresses: []common.Address{admin},
			},
			Upgrade: precompileconfig.Upgrade{
				BlockTimestamp: new(uint64(0)),
			},
		},
	})
}

// TestTxAllowListRejectsSender asserts that an enabled tx allowlist MUST reject
// txs from senders without a role.
func TestTxAllowListRejectsSender(t *testing.T) {
	// Both upgrades activate in the first block.
	reenableTime := testStartTime.Add(blockBuildAdvance)
	disableTime := reenableTime.Add(-time.Second)

	const adminIdx, userIdx = 0, 1
	// Each subtest starts a new chain, so it needs a wallet with fresh nonces
	// for the same accounts.
	keychain := saetest.NewUNSAFEKeyChain(t, 2)
	admin := keychain.Addresses()[adminIdx]

	tests := []struct {
		name     string
		upgrades []extras.PrecompileUpgrade
	}{
		{
			name: "enabled_at_genesis",
		},
		{
			name: "reenabled",
			upgrades: []extras.PrecompileUpgrade{
				{
					Config: &txallowlist.Config{
						Upgrade: precompileconfig.Upgrade{
							BlockTimestamp: new(uint64(disableTime.Unix())), //#nosec G115 -- Known non-negative
							Disable:        true,
						},
					},
				},
				{
					Config: &txallowlist.Config{
						AllowListConfig: allowlist.AllowListConfig{
							AdminAddresses: []common.Address{admin},
						},
						Upgrade: precompileconfig.Upgrade{
							BlockTimestamp: new(uint64(reenableTime.Unix())), //#nosec G115 -- Known non-negative
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wallet := saetest.NewWalletWithKeyChain(keychain, types.LatestSignerForChainID(big.NewInt(testChainID)))
			ctx, sut := newSUT(t,
				withMaxAllocFor(keychain.Addresses()...),
				withTxAllowList(admin),
				withUpgrades(extras.UpgradeConfig{PrecompileUpgrades: tt.upgrades}),
			)
			sut.buildBlock(ctx, t, blockBuildAdvance, wallet.SetNonceAndSign(t, adminIdx, transferTx(common.Address{1}, common.Big1)))

			err := sut.EthClient().SendTransaction(ctx, wallet.SetNonceAndSign(t, userIdx, transferTx(common.Address{1}, common.Big1)))
			// JSON-RPC stringifies the error, losing the sentinel.
			require.ErrorContains(t, err, vmerrors.ErrSenderAddressNotAllowListed.Error(), "user tx submission") //nolint:forbidigo // upstream error wrapped as string
		})
	}
}

// TestTxAllowListDisableAdmitsSender asserts that disabling the tx allowlist
// MUST admit txs from any sender once the disable settles.
func TestTxAllowListDisableAdmitsSender(t *testing.T) {
	disableTime := testStartTime.Add(blockBuildAdvance)

	const adminIdx, userIdx = 0, 1
	wallet := newWallet(t, 2)
	ctx, sut := newSUT(t,
		withMaxAllocFor(wallet.Addresses()...),
		withTxAllowList(wallet.Addresses()[adminIdx]),
		withUpgrades(extras.UpgradeConfig{
			PrecompileUpgrades: []extras.PrecompileUpgrade{
				{
					Config: &txallowlist.Config{
						Upgrade: precompileconfig.Upgrade{
							BlockTimestamp: new(uint64(disableTime.Unix())), //#nosec G115 -- Known non-negative
							Disable:        true,
						},
					},
				},
			},
		}),
	)
	sut.buildBlock(ctx, t, blockBuildAdvance, wallet.SetNonceAndSign(t, adminIdx, transferTx(common.Address{1}, common.Big1))) // activates the disable

	// Blocks admit txs against the last-settled state.
	tx := wallet.SetNonceAndSign(t, userIdx, transferTx(common.Address{1}, common.Big1))
	sut.buildBlock(ctx, t, settleAdvance, tx)
	sut.requireReceiptStatus(ctx, t, tx, types.ReceiptStatusSuccessful)
}

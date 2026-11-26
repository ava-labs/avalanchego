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
	"github.com/ava-labs/libevm/rpc"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/nativeminter"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
)

const nativeMinterCallGas = 100_000

// mintTx returns a tx calling mintNativeCoin to mint amount to to.
func mintTx(tb testing.TB, to common.Address, amount uint64) *types.DynamicFeeTx {
	tb.Helper()

	data, err := nativeminter.PackMintNativeCoin(to, new(big.Int).SetUint64(amount))
	require.NoErrorf(tb, err, "nativeminter.PackMintNativeCoin(%s, %d)", to, amount)
	return callTx(nativeminter.ContractAddress, data, nativeMinterCallGas)
}

// TestNativeMinterPrecompileUpgrades asserts that a native minter upgrade MUST
// apply to mints in the first block at or after its timestamp.
func TestNativeMinterPrecompileUpgrades(t *testing.T) {
	disableTime := testStartTime.Add(blockBuildAdvance)
	reenableTime := disableTime.Add(blockBuildAdvance)

	const (
		adminIdx, userIdx = 0, 1
		mintAmount        = 1_000
	)
	wallet := newWallet(t, 2)
	admin := wallet.Addresses()[adminIdx]
	user := wallet.Addresses()[userIdx]
	recipient := common.Address{'r', 'e', 'c', 'i', 'p', 'i', 'e', 'n', 't'}

	ctx, sut := newSUT(t,
		withMaxAllocFor(wallet.Addresses()...),
		withGenesisPrecompiles(extras.Precompiles{
			nativeminter.ConfigKey: &nativeminter.Config{
				AllowListConfig: allowlist.AllowListConfig{
					AdminAddresses: []common.Address{admin},
				},
				Upgrade: precompileconfig.Upgrade{
					BlockTimestamp: new(uint64(0)),
				},
			},
		}),
		withUpgrades(extras.UpgradeConfig{
			PrecompileUpgrades: []extras.PrecompileUpgrade{
				{
					Config: &nativeminter.Config{
						Upgrade: precompileconfig.Upgrade{
							BlockTimestamp: new(uint64(disableTime.Unix())), //#nosec G115 -- Known non-negative
							Disable:        true,
						},
					},
				},
				{
					Config: &nativeminter.Config{
						AllowListConfig: allowlist.AllowListConfig{
							AdminAddresses:   []common.Address{admin},
							ManagerAddresses: []common.Address{user},
						},
						Upgrade: precompileconfig.Upgrade{
							BlockTimestamp: new(uint64(reenableTime.Unix())), //#nosec G115 -- Known non-negative
						},
					},
				},
			},
		}),
	)

	type (
		mint struct {
			from       int
			wantStatus uint64
		}
		roles struct{ admin, user allowlist.Role }
	)
	steps := []struct {
		name        string
		time        time.Time
		mints       []mint
		wantEnabled bool
		wantRoles   roles
		wantBalance uint64
	}{
		{
			name: "genesis",
			time: testStartTime,
			mints: []mint{
				{from: adminIdx, wantStatus: types.ReceiptStatusSuccessful},
				{from: userIdx, wantStatus: types.ReceiptStatusFailed},
			},
			wantEnabled: true,
			wantRoles: roles{
				admin: allowlist.AdminRole,
				user:  allowlist.NoRole,
			},
			wantBalance: mintAmount,
		},
		{
			// Without the precompile, the call succeeds as a call to an
			// account with no code, so only the balance shows no mint.
			name: "disable",
			time: disableTime,
			mints: []mint{
				{from: userIdx, wantStatus: types.ReceiptStatusSuccessful},
			},
			wantEnabled: false,
			wantRoles: roles{
				admin: allowlist.NoRole,
				user:  allowlist.NoRole,
			},
			wantBalance: mintAmount,
		},
		{
			name: "reenable",
			time: reenableTime,
			mints: []mint{
				{from: userIdx, wantStatus: types.ReceiptStatusSuccessful},
			},
			wantEnabled: true,
			wantRoles: roles{
				admin: allowlist.AdminRole,
				user:  allowlist.ManagerRole,
			},
			wantBalance: 2 * mintAmount,
		},
	}

	for _, s := range steps {
		sut.clock.Set(s.time)
		txs := make([]*types.Transaction, len(s.mints))
		for i, m := range s.mints {
			txs[i] = wallet.SetNonceAndSign(t, m.from, mintTx(t, recipient, mintAmount))
			sut.send(ctx, t, txs[i])
		}
		sut.RunConsensusLoop(ctx, t)

		for i, m := range s.mints {
			sut.requireReceiptStatus(ctx, t, txs[i], m.wantStatus)
		}
		require.Equalf(t, *uint256.NewInt(s.wantBalance), sut.Balance(t, recipient), "%s: recipient balance", s.name)
		require.Equalf(t, s.wantEnabled, sut.isPrecompileEnabledAtLatest(nativeminter.ContractAddress), "%s: precompile enabled", s.name)

		got := sut.fetchAllowListRole(ctx, t, nativeminter.ContractAddress, admin, rpc.LatestBlockNumber)
		require.Equalf(t, s.wantRoles.admin, got, "%s: admin role", s.name)
		got = sut.fetchAllowListRole(ctx, t, nativeminter.ContractAddress, user, rpc.LatestBlockNumber)
		require.Equalf(t, s.wantRoles.user, got, "%s: user role", s.name)
	}
}

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
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/deployerallowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
)

// TestDeployerAllowListPrecompileUpgrades asserts that a deployer allowlist
// upgrade MUST apply to deploys in the first block at or after its timestamp.
func TestDeployerAllowListPrecompileUpgrades(t *testing.T) {
	disableTime := testStartTime.Add(blockBuildAdvance)
	reenableTime := disableTime.Add(blockBuildAdvance)

	const adminIdx, userIdx = 0, 1
	wallet := newWallet(t, 2)
	admin := wallet.Addresses()[adminIdx]
	user := wallet.Addresses()[userIdx]

	ctx, sut := newSUT(t,
		withMaxAllocFor(wallet.Addresses()...),
		withGenesisPrecompiles(extras.Precompiles{
			deployerallowlist.ConfigKey: &deployerallowlist.Config{
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
					Config: &deployerallowlist.Config{
						Upgrade: precompileconfig.Upgrade{
							BlockTimestamp: new(uint64(disableTime.Unix())), //#nosec G115 -- Known non-negative
							Disable:        true,
						},
					},
				},
				{
					Config: &deployerallowlist.Config{
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
		deploy struct {
			from       int
			wantStatus uint64
		}
		roles struct{ admin, user allowlist.Role }
	)
	steps := []struct {
		name        string
		time        time.Time
		deploys     []deploy
		wantEnabled bool
		wantRoles   roles
	}{
		{
			name: "genesis",
			time: testStartTime,
			deploys: []deploy{
				{from: adminIdx, wantStatus: types.ReceiptStatusSuccessful},
				{from: userIdx, wantStatus: types.ReceiptStatusFailed},
			},
			wantEnabled: true,
			wantRoles: roles{
				admin: allowlist.AdminRole,
				user:  allowlist.NoRole,
			},
		},
		{
			name: "disable",
			time: disableTime,
			deploys: []deploy{
				{from: userIdx, wantStatus: types.ReceiptStatusSuccessful},
			},
			wantEnabled: false,
			wantRoles: roles{
				admin: allowlist.NoRole,
				user:  allowlist.NoRole,
			},
		},
		{
			name: "reenable",
			time: reenableTime,
			deploys: []deploy{
				{from: userIdx, wantStatus: types.ReceiptStatusSuccessful},
			},
			wantEnabled: true,
			wantRoles: roles{
				admin: allowlist.AdminRole,
				user:  allowlist.ManagerRole,
			},
		},
	}

	for _, s := range steps {
		sut.clock.Set(s.time)
		txs := make([]*types.Transaction, len(s.deploys))
		for i, d := range s.deploys {
			txs[i] = wallet.SetNonceAndSign(t, d.from, deployTx())
			sut.send(ctx, t, txs[i])
		}
		sut.RunConsensusLoop(ctx, t)

		for i, d := range s.deploys {
			sut.requireDeployed(ctx, t, txs[i], d.wantStatus)
		}
		require.Equalf(t, s.wantEnabled, sut.isPrecompileEnabledAtLatest(deployerallowlist.ContractAddress), "%s: precompile enabled", s.name)

		got := sut.fetchAllowListRole(ctx, t, deployerallowlist.ContractAddress, admin, rpc.LatestBlockNumber)
		require.Equalf(t, s.wantRoles.admin, got, "%s: admin role", s.name)
		got = sut.fetchAllowListRole(ctx, t, deployerallowlist.ContractAddress, user, rpc.LatestBlockNumber)
		require.Equalf(t, s.wantRoles.user, got, "%s: user role", s.name)
	}
}

// requireDeployed asserts that the contract creation tx was included with
// status want, and deployed code if and only if it succeeded.
func (s *SUT) requireDeployed(ctx context.Context, tb testing.TB, tx *types.Transaction, want uint64) {
	tb.Helper()

	receipt := s.requireReceiptStatus(ctx, tb, tx, want)

	client := s.EthClient()
	code, err := client.CodeAt(ctx, receipt.ContractAddress, nil)
	require.NoErrorf(tb, err, "%T.CodeAt(%s)", client, receipt.ContractAddress)
	require.Equalf(tb, want == types.ReceiptStatusSuccessful, len(code) > 0, "code deployed by tx %#x", tx.Hash())
}

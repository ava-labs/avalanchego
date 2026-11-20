// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/vmerrors"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"

	saeparams "github.com/ava-labs/avalanchego/vms/evm/params"
	ethparams "github.com/ava-labs/libevm/params"
)

// TestTxAllowListPrecompileUpgradesSAE exercises mid-chain `PrecompileUpgrades`
// for the `txallowlist` precompile under SAE end-to-end through a single
// timeline: genesis-enabled (admin only) -> disable -> re-enable (admin +
// manager). At each step we assert all three properties of interest:
//  1. The precompile is correctly enabled / disabled at the scheduled
//     timestamp (via `StartExecutingBlock` -> `core.ApplyUpgrades`).
//  2. The configuration is correctly applied to state (admin / manager
//     roles observable via `txallowlist.GetTxAllowListStatus`).
//  3. Worst-case admission correctly blocks transfers from non-enabled
//     roles against the last-settled state, and unblocks them once the
//     allowlist is removed.
func TestTxAllowListPrecompileUpgradesSAE(t *testing.T) {
	const (
		adminIdx    = 0
		nonAdminIdx = 1
	)

	now := testStartTime
	disableTime := now.Add(saeparams.Tau)
	// Re-enable after the disable activation has had time to settle, so the
	// timeline only ever moves forward during the test.
	reenableTime := disableTime.Add(2 * saeparams.Tau)

	wallet := newWallet(t, 2)
	addresses := wallet.Addresses()
	admin := addresses[adminIdx]
	nonAdmin := addresses[nonAdminIdx]

	ctx, sut := newSUT(
		t,
		withMaxAllocFor(addresses...),
		withGenesisPrecompiles(extras.Precompiles{
			txallowlist.ConfigKey: txallowlist.NewConfig(
				new(uint64(0)),
				[]common.Address{admin},
				nil,
				nil,
			),
		}),
		withUpgrades(extras.UpgradeConfig{
			PrecompileUpgrades: []extras.PrecompileUpgrade{
				{
					Config: txallowlist.NewDisableConfig(new(uint64(disableTime.Unix()))), // #nosec G115 -- known positive test timestamp
				},
				{
					// Re-enable promotes `nonAdminIdx` to manager so we
					// can observe the new config land in state.
					Config: txallowlist.NewConfig(
						new(uint64(reenableTime.Unix())), // #nosec G115 -- known positive test timestamp
						[]common.Address{admin},
						nil,
						[]common.Address{nonAdmin},
					),
				},
			},
		}),
	)

	fundValue := new(big.Int).Mul(big.NewInt(ethparams.Ether), big.NewInt(1000))

	// Step 0: at genesis, the allowlist is enabled with `admin` as the only
	// allow-listed sender. Latest and finalized agree (no upgrades yet).
	// (1) Precompile is enabled per chain config at the current timestamp.
	require.True(t, sut.isPrecompileEnabledAtLatest(txallowlist.ContractAddress), "txallowlist must be enabled at genesis")
	// (2) Genesis config is applied to state in both latest & finalized.
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(ctx, t, txallowlist.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, txallowlist.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(ctx, t, txallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, txallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// (3) Mempool ingress (last-executed) rejects the non-admin sender at
	// RPC; worst-case admission (last-settled) excludes it from blocks.
	// Fund `nonAdmin` so it can pay fees later.
	fundNonAdmin := wallet.SetNonceAndSign(t, adminIdx, transferTx(nonAdmin, fundValue))
	sut.send(ctx, t, fundNonAdmin)
	droppedTx := wallet.SetNonceAndSign(t, nonAdminIdx, transferTx(admin, common.Big1))
	// JSON-RPC stringifies the error, so the sentinel chain is lost; match
	// on its message instead. See e.g. txallowlist/simulated_test.go.
	require.ErrorContains(t, //nolint:forbidigo // upstream error wrapped as string
		sut.EthClient().SendTransaction(ctx, droppedTx),
		vmerrors.ErrSenderAddressNotAllowListed.Error(),
		"non-admin tx must be rejected at mempool ingress while allowlist is enabled",
	)
	block := sut.RunConsensusLoop(ctx, t)
	require.Len(t, block.Transactions(), 1, "non-admin tx must be excluded while allowlist is enabled")
	require.Equal(t, fundNonAdmin.Hash(), block.Transactions()[0].Hash())

	// Step 1: advance to `disableTime` and produce the activation block.
	sut.clock.Set(disableTime)
	disableActivationTx := wallet.SetNonceAndSign(t, adminIdx, transferTx(nonAdmin, common.Big1))
	sut.send(ctx, t, disableActivationTx)
	block = sut.RunConsensusLoop(ctx, t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, disableActivationTx.Hash(), block.Transactions()[0].Hash())

	// (1) Disable upgrade fires inside `StartExecutingBlock`; precompile is
	// no longer enabled per chain config at the current timestamp.
	require.False(t, sut.isPrecompileEnabledAtLatest(txallowlist.ContractAddress), "txallowlist must be disabled after activation")
	// (2) State diverges between latest and finalized: latest reflects the
	// disable just applied, but finalized still observes the pre-disable
	// admin role because settlement lags by Tau.
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, txallowlist.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, txallowlist.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(ctx, t, txallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized state must still show admin role until disable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, txallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// (3) Ingress (last-executed) accepts `droppedTx` immediately on
	// re-submit; worst-case admission (last-settled) only includes it after
	// the disable activation settles.
	require.NoError(t, sut.EthClient().SendTransaction(ctx, droppedTx),
		"previously-blocked tx must be admitted at mempool ingress once disable executes")
	sut.clock.Advance(saeparams.Tau + time.Second)
	block = sut.RunConsensusLoop(ctx, t)
	require.Len(t, block.Transactions(), 1, "previously-blocked tx must be included once disable settles")
	require.Equal(t, droppedTx.Hash(), block.Transactions()[0].Hash())

	// (2) After settlement, finalized catches up with latest: both observe
	// the disabled allowlist (no roles).
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, txallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized must reflect disable once it has settled")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, txallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// Step 2: advance to `reenableTime` and produce the re-enable activation
	// block.
	sut.clock.Set(reenableTime)
	reenableActivationTx := wallet.SetNonceAndSign(t, adminIdx, transferTx(nonAdmin, common.Big1))
	sut.send(ctx, t, reenableActivationTx)
	block = sut.RunConsensusLoop(ctx, t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, reenableActivationTx.Hash(), block.Transactions()[0].Hash())

	// (1) Re-enable upgrade fires inside `StartExecutingBlock`; precompile
	// is enabled again per chain config at the current timestamp.
	require.True(t, sut.isPrecompileEnabledAtLatest(txallowlist.ContractAddress), "txallowlist must be re-enabled after activation")
	// (2) Latest reflects the re-enable (admin + manager promotion); the
	// finalized snapshot is still pre-re-enable (no roles).
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(ctx, t, txallowlist.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.ManagerRole, sut.fetchAllowListRole(ctx, t, txallowlist.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, txallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized state must still reflect disabled allowlist until re-enable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, txallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// (3) `nonAdmin` is now a manager and must be admissible
	sut.clock.Advance(saeparams.Tau + time.Second)
	managerTx := wallet.SetNonceAndSign(t, nonAdminIdx, transferTx(admin, common.Big1))
	sut.send(ctx, t, managerTx)
	block = sut.RunConsensusLoop(ctx, t)
	require.Len(t, block.Transactions(), 1, "manager tx must be admitted after re-enable settles")
	require.Equal(t, managerTx.Hash(), block.Transactions()[0].Hash())

	// (2) Finalized has caught up with latest after re-enable settled.
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(ctx, t, txallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized must reflect re-enable once it has settled")
	require.Equal(t, allowlist.ManagerRole, sut.fetchAllowListRole(ctx, t, txallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))
}

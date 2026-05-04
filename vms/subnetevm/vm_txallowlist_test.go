// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/vmerrors"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

// TestTxAllowListPrecompileUpgradesSAE exercises mid-chain `PrecompileUpgrades`
// for the `txallowlist` precompile under SAE end-to-end through a single
// timeline: genesis-enabled (admin only) -> disable -> re-enable (admin +
// manager). At each step we assert all three properties of interest:
//  1. The precompile is correctly enabled / disabled at the scheduled
//     timestamp (via `BeforeExecutingBlock` -> `core.ApplyUpgrades`).
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

	now := postHeliconStartTime(t)
	disableTime := now.Add(saeparams.Tau)
	// Re-enable after the disable activation has had time to settle, so the
	// timeline only ever moves forward during the test.
	reenableTime := disableTime.Add(2 * saeparams.Tau)

	sut := newSUT(
		t,
		withFork(upgradetest.Helicon),
		withNumAccounts(2),
		withNow(now),
		withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
			subnetevmparams.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
				txallowlist.ConfigKey: txallowlist.NewConfig(
					utils.PointerTo[uint64](0),
					[]common.Address{addresses[adminIdx]},
					nil,
					nil,
				),
			}
		}),
		withUpgradeConfig(func(addresses []common.Address) []byte {
			return mustMarshalJSON(t, &extras.UpgradeConfig{
				PrecompileUpgrades: []extras.PrecompileUpgrade{
					{
						Config: txallowlist.NewDisableConfig(utils.PointerTo(uint64(disableTime.Unix()))),
					},
					{
						// Re-enable promotes `nonAdminIdx` to manager so we
						// can observe the new config land in state.
						Config: txallowlist.NewConfig(
							utils.PointerTo(uint64(reenableTime.Unix())),
							[]common.Address{addresses[adminIdx]},
							nil,
							[]common.Address{addresses[nonAdminIdx]},
						),
					},
				},
			})
		}),
	)

	addresses := sut.ethWallet.Addresses()
	admin := addresses[adminIdx]
	nonAdmin := addresses[nonAdminIdx]
	fundValue := new(big.Int).Mul(big.NewInt(subnetevmparams.Ether), big.NewInt(1000))

	// Step 0: at genesis, the allowlist is enabled with `admin` as the only
	// allow-listed sender. Latest and finalized agree (no upgrades yet).
	// (1) Precompile is enabled per chain config at the current timestamp.
	require.True(t, sut.isPrecompileEnabledAtLatest(txallowlist.ContractAddress), "txallowlist must be enabled at genesis")
	// (2) Genesis config is applied to state in both latest & finalized.
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(t, txallowlist.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, txallowlist.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(t, txallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, txallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// (3) Mempool ingress (last-executed) rejects the non-admin sender at
	// RPC; worst-case admission (last-settled) excludes it from blocks.
	// Fund `nonAdmin` so it can pay fees later.
	fundNonAdmin := sut.sendTransferTx(t, adminIdx, nonAdminIdx, fundValue)
	droppedTx := sut.signTransferTx(t, nonAdminIdx, adminIdx, common.Big1)
	// JSON-RPC stringifies the error, so the sentinel chain is lost; match
	// on its message instead. See e.g. txallowlist/simulated_test.go.
	require.ErrorContains(t, //nolint:forbidigo // upstream error wrapped as string
		sut.client.SendTransaction(sut.ctx, droppedTx),
		vmerrors.ErrSenderAddressNotAllowListed.Error(),
		"non-admin tx must be rejected at mempool ingress while allowlist is enabled",
	)
	block := sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1, "non-admin tx must be excluded while allowlist is enabled")
	require.Equal(t, fundNonAdmin.Hash(), block.Transactions()[0].Hash())

	// Step 1: advance to `disableTime` and produce the activation block.
	sut.setTime(t, disableTime)
	disableActivationTx := sut.sendTransferTx(t, adminIdx, nonAdminIdx, common.Big1)
	block = sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, disableActivationTx.Hash(), block.Transactions()[0].Hash())

	// (1) Disable upgrade fires inside `BeforeExecutingBlock`; precompile is
	// no longer enabled per chain config at the current timestamp.
	require.False(t, sut.isPrecompileEnabledAtLatest(txallowlist.ContractAddress), "txallowlist must be disabled after activation")
	// (2) State diverges between latest and finalized: latest reflects the
	// disable just applied, but finalized still observes the pre-disable
	// admin role because settlement lags by Tau.
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, txallowlist.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, txallowlist.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(t, txallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized state must still show admin role until disable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, txallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// (3) Ingress (last-executed) accepts `droppedTx` immediately on
	// re-submit; worst-case admission (last-settled) only includes it after
	// the disable activation settles.
	require.NoError(t, sut.client.SendTransaction(sut.ctx, droppedTx),
		"previously-blocked tx must be admitted at mempool ingress once disable executes")
	sut.advanceTime(t, saeparams.Tau+time.Second)
	block = sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1, "previously-blocked tx must be included once disable settles")
	require.Equal(t, droppedTx.Hash(), block.Transactions()[0].Hash())

	// (2) After settlement, finalized catches up with latest: both observe
	// the disabled allowlist (no roles).
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, txallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized must reflect disable once it has settled")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, txallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// Step 2: advance to `reenableTime` and produce the re-enable activation
	// block.
	sut.setTime(t, reenableTime)
	reenableActivationTx := sut.sendTransferTx(t, adminIdx, nonAdminIdx, common.Big1)
	block = sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, reenableActivationTx.Hash(), block.Transactions()[0].Hash())

	// (1) Re-enable upgrade fires inside `BeforeExecutingBlock`; precompile
	// is enabled again per chain config at the current timestamp.
	require.True(t, sut.isPrecompileEnabledAtLatest(txallowlist.ContractAddress), "txallowlist must be re-enabled after activation")
	// (2) Latest reflects the re-enable (admin + manager promotion); the
	// finalized snapshot is still pre-re-enable (no roles).
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(t, txallowlist.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.ManagerRole, sut.fetchAllowListRole(t, txallowlist.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, txallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized state must still reflect disabled allowlist until re-enable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, txallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// (3) `nonAdmin` is now a manager and must be admissible
	sut.advanceTime(t, saeparams.Tau+time.Second)
	managerTx := sut.sendTransferTx(t, nonAdminIdx, adminIdx, common.Big1)
	block = sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1, "manager tx must be admitted after re-enable settles")
	require.Equal(t, managerTx.Hash(), block.Transactions()[0].Hash())

	// (2) Finalized has caught up with latest after re-enable settled.
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(t, txallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized must reflect re-enable once it has settled")
	require.Equal(t, allowlist.ManagerRole, sut.fetchAllowListRole(t, txallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))
}

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

	saeparams "github.com/ava-labs/avalanchego/vms/evm/params"
)

// TestDeployerAllowListPrecompileUpgradesSAE exercises mid-chain
// `PrecompileUpgrades` for the `deployerallowlist` precompile under SAE
// end-to-end through the same enable -> disable -> re-enable timeline used
// by [TestTxAllowListPrecompileUpgradesSAE]. The execution-semantics step
// (3) is materially different because deployer allowlist is enforced by
// libevm's frame-local [params.RulesExtra.CanCreateContract], not by SAE
// admission.
//
//  1. The precompile is correctly enabled / disabled at the scheduled
//     timestamp (via `StartExecutingBlock` -> `core.ApplyUpgrades`).
//  2. Admin / manager roles are observable via
//     `deployerallowlist.GetContractDeployerAllowListStatus` and follow the
//     last-executed vs last-settled lag exactly as for txallowlist.
//  3. Deploy txs from non-allow-listed senders are ADMITTED at RPC, but
//     produce a `status=failed` receipt with no contract code at the deploy
//     address (frame-local revert from `CanCreateContract`). Once disabled,
//     the same sender's deploy succeeds (`status=success`, deployed code
//     present).
func TestDeployerAllowListPrecompileUpgradesSAE(t *testing.T) {
	const (
		adminIdx    = 0
		nonAdminIdx = 1
	)

	now := testStartTime
	disableTime := now.Add(saeparams.Tau)
	reenableTime := disableTime.Add(2 * saeparams.Tau)

	wallet := newWallet(t, 2)
	addresses := wallet.Addresses()
	admin := addresses[adminIdx]
	nonAdmin := addresses[nonAdminIdx]

	ctx, sut := newSUT(
		t,
		withMaxAllocFor(addresses...),
		withGenesisPrecompiles(extras.Precompiles{
			deployerallowlist.ConfigKey: deployerallowlist.NewConfig(
				new(uint64(0)),
				[]common.Address{admin},
				nil,
				nil,
			),
		}),
		withUpgrades(extras.UpgradeConfig{
			PrecompileUpgrades: []extras.PrecompileUpgrade{
				{
					Config: deployerallowlist.NewDisableConfig(new(uint64(disableTime.Unix()))), // #nosec G115 -- known positive test timestamp
				},
				{
					// Re-enable promotes `nonAdminIdx` to manager so we
					// can observe the new config land in state.
					Config: deployerallowlist.NewConfig(
						new(uint64(reenableTime.Unix())), // #nosec G115 -- known positive test timestamp
						[]common.Address{admin},
						nil,
						[]common.Address{nonAdmin},
					),
				},
			},
		}),
	)

	// Step 0: at genesis, deployer allowlist is enabled with `admin` as the
	// only allow-listed deployer. Latest and finalized agree (no upgrades
	// yet).
	// (1) Precompile is enabled per chain config at the current timestamp.
	require.True(t, sut.isPrecompileEnabledAtLatest(deployerallowlist.ContractAddress),
		"deployerallowlist must be enabled at genesis")
	// (2) Genesis config is applied to state in both latest & finalized.
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(ctx, t, deployerallowlist.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, deployerallowlist.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(ctx, t, deployerallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, deployerallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// (3) Both deploys are admitted at RPC because the deployer allowlist is
	// not enforced at admission. The admin deploy succeeds, while the
	// non-admin's deploy reverts inside the EVM via libevm's
	// `CanCreateContract` hook, producing a status=failed receipt with no
	// code at the deploy address.
	adminDeploy := sut.send(ctx, t, wallet, adminIdx, deployTx())
	block := sut.RunConsensusLoop(ctx, t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, adminDeploy.Hash(), block.Transactions()[0].Hash())
	sut.requireDeploySucceeded(ctx, t, adminDeploy)

	sut.clock.Advance(blockBuildAdvance)
	nonAdminDeploy := sut.send(ctx, t, wallet, nonAdminIdx, deployTx())
	block = sut.RunConsensusLoop(ctx, t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, nonAdminDeploy.Hash(), block.Transactions()[0].Hash())
	sut.requireDeployFailed(ctx, t, nonAdminDeploy)

	// Step 1: advance past `disableTime` and produce the activation block.
	// `StartExecutingBlock` runs the disable upgrade BEFORE tx execution
	// in that block, so `nonAdmin`'s deploy in the same block sees the
	// precompile already disabled and `CanCreateContract` is a no-op.
	sut.clock.Set(disableTime.Add(time.Second))
	postDisableDeploy := sut.send(ctx, t, wallet, nonAdminIdx, deployTx())
	block = sut.RunConsensusLoop(ctx, t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, postDisableDeploy.Hash(), block.Transactions()[0].Hash())
	sut.requireDeploySucceeded(ctx, t, postDisableDeploy)

	// (1) Disable upgrade fired inside `StartExecutingBlock`; precompile
	// is no longer enabled per chain config at the current timestamp.
	require.False(t, sut.isPrecompileEnabledAtLatest(deployerallowlist.ContractAddress),
		"deployerallowlist must be disabled after activation")
	// (2) Latest reflects the disable; finalized still observes the
	// pre-disable admin role (settlement lags by Tau).
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, deployerallowlist.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, deployerallowlist.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(ctx, t, deployerallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized state must still show admin role until disable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, deployerallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// Allow the disable activation to settle so finalized catches up.
	sut.clock.Advance(saeparams.Tau + time.Second)
	settleTx := sut.send(ctx, t, wallet, adminIdx, transferTx(nonAdmin, common.Big1))
	block = sut.RunConsensusLoop(ctx, t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, settleTx.Hash(), block.Transactions()[0].Hash())
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, deployerallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized must reflect disable once it has settled")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, deployerallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// Step 2: advance to `reenableTime` and produce the re-enable
	// activation block. Same as step 1: the upgrade fires in
	// `StartExecutingBlock`, so a deploy from the now-promoted manager
	// (`nonAdmin`) in this block sees the precompile already enabled with
	// the new roles and succeeds.
	sut.clock.Set(reenableTime)
	managerDeploy := sut.send(ctx, t, wallet, nonAdminIdx, deployTx())
	block = sut.RunConsensusLoop(ctx, t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, managerDeploy.Hash(), block.Transactions()[0].Hash())
	sut.requireDeploySucceeded(ctx, t, managerDeploy)

	// (1) Re-enable upgrade fired; precompile is enabled per chain config
	// at the current timestamp.
	require.True(t, sut.isPrecompileEnabledAtLatest(deployerallowlist.ContractAddress),
		"deployerallowlist must be re-enabled after activation")
	// (2) Latest reflects the re-enable (admin + manager promotion);
	// finalized is still pre-re-enable (no roles).
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(ctx, t, deployerallowlist.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.ManagerRole, sut.fetchAllowListRole(ctx, t, deployerallowlist.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, deployerallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized state must still reflect disabled allowlist until re-enable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(ctx, t, deployerallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))
}

// requireDeploySucceeded asserts that tx deployed code.
func (s *SUT) requireDeploySucceeded(ctx context.Context, tb testing.TB, tx *types.Transaction) {
	tb.Helper()
	require.Nil(tb, tx.To(), "test setup: tx must be a contract creation")

	receipt := s.requireReceiptStatus(ctx, tb, tx, types.ReceiptStatusSuccessful)
	require.NotEqualf(tb, (common.Address{}), receipt.ContractAddress,
		"successful deploy must populate ContractAddress")

	code, err := s.EthClient().CodeAt(ctx, receipt.ContractAddress, nil)
	require.NoErrorf(tb, err, "CodeAt(%s)", receipt.ContractAddress)
	require.NotEmptyf(tb, code, "successful deploy at %s must leave code on chain", receipt.ContractAddress)
}

// requireDeployFailed asserts that tx was included but deployed no code.
func (s *SUT) requireDeployFailed(ctx context.Context, tb testing.TB, tx *types.Transaction) {
	tb.Helper()
	require.Nil(tb, tx.To(), "test setup: tx must be a contract creation")

	receipt := s.requireReceiptStatus(ctx, tb, tx, types.ReceiptStatusFailed)
	// Failed deploys still report the derived address.
	code, err := s.EthClient().CodeAt(ctx, receipt.ContractAddress, nil)
	require.NoErrorf(tb, err, "CodeAt(%s)", receipt.ContractAddress)
	require.Emptyf(tb, code, "failed deploy must leave no code at %s", receipt.ContractAddress)
}

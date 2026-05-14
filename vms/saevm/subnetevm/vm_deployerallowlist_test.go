// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/deployerallowlist"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"

	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

// TestDeployerAllowListPrecompileUpgradesSAE exercises mid-chain
// `PrecompileUpgrades` for the `deployerallowlist` precompile under SAE
// end-to-end through the same enable -> disable -> re-enable timeline used
// by [TestTxAllowListPrecompileUpgradesSAE]. The execution-semantics step
// (3) is materially different because deployer allowlist is enforced by
// libevm's frame-local [params.RulesExtra.CanCreateContract] (not by SAE
// admission); see the design note on [hook.Points.CanExecuteTransaction].
//
//  1. The precompile is correctly enabled / disabled at the scheduled
//     timestamp (via `BeforeExecutingBlock` -> `core.ApplyUpgrades`).
//  2. Admin / manager roles are observable via
//     `deployerallowlist.GetContractDeployerAllowListStatus` and follow the
//     last-executed vs last-settled lag exactly as for txallowlist.
//  3. Deploy txs from non-allow-listed senders are ADMITTED at RPC and
//     INCLUDED in the block, but produce a `status=failed` receipt with
//     no contract code at the deploy address (frame-local revert from
//     `CanCreateContract`). Once disabled, the same sender's deploy
//     succeeds (`status=success`, deployed code present).
func TestDeployerAllowListPrecompileUpgradesSAE(t *testing.T) {
	const (
		adminIdx    = 0
		nonAdminIdx = 1
	)

	now := postHeliconStartTime(t)
	disableTime := now.Add(saeparams.Tau)
	reenableTime := disableTime.Add(2 * saeparams.Tau)

	sut := newSUT(
		t,
		withFork(upgradetest.Helicon),
		withNumAccounts(2),
		withNow(now),
		withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
			subnetevmparams.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
				deployerallowlist.ConfigKey: deployerallowlist.NewConfig(
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
						Config: deployerallowlist.NewDisableConfig(utils.PointerTo(uint64(disableTime.Unix()))),
					},
					{
						// Re-enable promotes `nonAdminIdx` to manager so we
						// can observe the new config land in state.
						Config: deployerallowlist.NewConfig(
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

	// Step 0: at genesis, deployer allowlist is enabled with `admin` as the
	// only allow-listed deployer. Latest and finalized agree (no upgrades
	// yet).
	// (1) Precompile is enabled per chain config at the current timestamp.
	require.True(t, sut.isPrecompileEnabledAtLatest(deployerallowlist.ContractAddress),
		"deployerallowlist must be enabled at genesis")
	// (2) Genesis config is applied to state in both latest & finalized.
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(t, deployerallowlist.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, deployerallowlist.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(t, deployerallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, deployerallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// (3) Both deploys are admitted at RPC (deployer allowlist is NOT
	// enforced at admission; see `hook.Points.CanExecuteTransaction`
	// design note). They are both included in the next block, but the
	// non-admin's deploy reverts inside the EVM via libevm's
	// `CanCreateContract` hook, producing a status=failed receipt with no
	// code at the deploy address.
	adminDeploy := sut.sendDeployTx(t, adminIdx)
	nonAdminDeploy := sut.sendDeployTx(t, nonAdminIdx)
	block := sut.buildAndAcceptBlock(t)
	requireBlockContainsTxs(t, block, adminDeploy.Hash(), nonAdminDeploy.Hash())
	sut.requireDeploySucceeded(t, adminDeploy)
	sut.requireDeployFailed(t, nonAdminDeploy)

	// Step 1: advance to `disableTime` and produce the activation block.
	// `BeforeExecutingBlock` runs the disable upgrade BEFORE tx execution
	// in that block, so `nonAdmin`'s deploy in the same block sees the
	// precompile already disabled and `CanCreateContract` is a no-op.
	sut.setTime(t, disableTime)
	postDisableDeploy := sut.sendDeployTx(t, nonAdminIdx)
	block = sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, postDisableDeploy.Hash(), block.Transactions()[0].Hash())
	sut.requireDeploySucceeded(t, postDisableDeploy)

	// (1) Disable upgrade fired inside `BeforeExecutingBlock`; precompile
	// is no longer enabled per chain config at the current timestamp.
	require.False(t, sut.isPrecompileEnabledAtLatest(deployerallowlist.ContractAddress),
		"deployerallowlist must be disabled after activation")
	// (2) Latest reflects the disable; finalized still observes the
	// pre-disable admin role (settlement lags by Tau).
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, deployerallowlist.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, deployerallowlist.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(t, deployerallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized state must still show admin role until disable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, deployerallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// Allow the disable activation to settle so finalized catches up.
	sut.advanceTime(t, saeparams.Tau+time.Second)
	settleTx := sut.sendTransferTx(t, adminIdx, nonAdminIdx, common.Big1)
	block = sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, settleTx.Hash(), block.Transactions()[0].Hash())
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, deployerallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized must reflect disable once it has settled")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, deployerallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))

	// Step 2: advance to `reenableTime` and produce the re-enable
	// activation block. Same as step 1: the upgrade fires in
	// `BeforeExecutingBlock`, so a deploy from the now-promoted manager
	// (`nonAdmin`) in this block sees the precompile already enabled with
	// the new roles and succeeds.
	sut.setTime(t, reenableTime)
	managerDeploy := sut.sendDeployTx(t, nonAdminIdx)
	block = sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, managerDeploy.Hash(), block.Transactions()[0].Hash())
	sut.requireDeploySucceeded(t, managerDeploy)

	// (1) Re-enable upgrade fired; precompile is enabled per chain config
	// at the current timestamp.
	require.True(t, sut.isPrecompileEnabledAtLatest(deployerallowlist.ContractAddress),
		"deployerallowlist must be re-enabled after activation")
	// (2) Latest reflects the re-enable (admin + manager promotion);
	// finalized is still pre-re-enable (no roles).
	require.Equal(t, allowlist.AdminRole, sut.fetchAllowListRole(t, deployerallowlist.ContractAddress, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.ManagerRole, sut.fetchAllowListRole(t, deployerallowlist.ContractAddress, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, deployerallowlist.ContractAddress, admin, rpc.FinalizedBlockNumber),
		"finalized state must still reflect disabled allowlist until re-enable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchAllowListRole(t, deployerallowlist.ContractAddress, nonAdmin, rpc.FinalizedBlockNumber))
}

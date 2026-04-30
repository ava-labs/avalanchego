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
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/vmerrors"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/deployerallowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

// minimalDeployInitCode returns 1 byte of deployed code (0x01) so that
// presence/absence of code at the deploy address cleanly distinguishes a
// successful deploy from a reverted (or never-executed) one. Equivalent to:
//
//	PUSH1 1; PUSH1 0; MSTORE8 // mem[0] = 0x01
//	PUSH1 1; PUSH1 0; RETURN  // return mem[0..1]
var minimalDeployInitCode = []byte{0x60, 0x01, 0x60, 0x00, 0x53, 0x60, 0x01, 0x60, 0x00, 0xF3}

func (s *SUT) fetchTxAllowListRole(t *testing.T, address common.Address, blockNumber rpc.BlockNumber) allowlist.Role {
	t.Helper()

	stateDB, _, err := s.vm.GethRPCBackends().StateAndHeaderByNumber(s.ctx, blockNumber)
	require.NoError(t, err)
	return txallowlist.GetTxAllowListStatus(stateDB, address)
}

func (s *SUT) fetchDeployerAllowListRole(t *testing.T, address common.Address, blockNumber rpc.BlockNumber) allowlist.Role {
	t.Helper()

	stateDB, _, err := s.vm.GethRPCBackends().StateAndHeaderByNumber(s.ctx, blockNumber)
	require.NoError(t, err)
	return deployerallowlist.GetContractDeployerAllowListStatus(stateDB, address)
}

func (s *SUT) signDeployTx(t *testing.T, from int) *types.Transaction {
	t.Helper()

	return s.ethWallet.SetNonceAndSign(t, from, &types.DynamicFeeTx{
		To:        nil, // contract creation
		Gas:       100_000,
		GasFeeCap: big.NewInt(225 * subnetevmparams.GWei),
		Data:      minimalDeployInitCode,
	})
}

func (s *SUT) sendDeployTx(t *testing.T, from int) *types.Transaction {
	t.Helper()

	tx := s.signDeployTx(t, from)
	require.NoError(t, s.client.SendTransaction(s.ctx, tx))
	return tx
}

func (s *SUT) isPrecompileEnabledAtLatest(precompile common.Address) bool {
	chainConfig := s.vm.GethRPCBackends().ChainConfig()
	timestamp := uint64(s.now.Unix())
	return subnetevmparams.GetExtra(chainConfig).IsPrecompileEnabled(precompile, timestamp)
}

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

	now := allowListTestStartTime(t)
	disableTime := now.Add(saeparams.Tau)
	// Re-enable after the disable activation has had time to settle, so the
	// timeline only ever moves forward during the test.
	reenableTime := disableTime.Add(2 * saeparams.Tau)

	sut := newSUT(
		t,
		withFork(upgradetest.Helicon),
		withNumAccounts(2),
		withNow(&now),
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
	require.Equal(t, allowlist.AdminRole, sut.fetchTxAllowListRole(t, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchTxAllowListRole(t, admin, rpc.FinalizedBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, nonAdmin, rpc.FinalizedBlockNumber))

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
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchTxAllowListRole(t, admin, rpc.FinalizedBlockNumber),
		"finalized state must still show admin role until disable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, nonAdmin, rpc.FinalizedBlockNumber))

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
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, admin, rpc.FinalizedBlockNumber),
		"finalized must reflect disable once it has settled")
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, nonAdmin, rpc.FinalizedBlockNumber))

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
	require.Equal(t, allowlist.AdminRole, sut.fetchTxAllowListRole(t, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.ManagerRole, sut.fetchTxAllowListRole(t, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, admin, rpc.FinalizedBlockNumber),
		"finalized state must still reflect disabled allowlist until re-enable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, nonAdmin, rpc.FinalizedBlockNumber))

	// (3) `nonAdmin` is now a manager and must be admissible
	sut.advanceTime(t, saeparams.Tau+time.Second)
	managerTx := sut.sendTransferTx(t, nonAdminIdx, adminIdx, common.Big1)
	block = sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1, "manager tx must be admitted after re-enable settles")
	require.Equal(t, managerTx.Hash(), block.Transactions()[0].Hash())

	// (2) Finalized has caught up with latest after re-enable settled.
	require.Equal(t, allowlist.AdminRole, sut.fetchTxAllowListRole(t, admin, rpc.FinalizedBlockNumber),
		"finalized must reflect re-enable once it has settled")
	require.Equal(t, allowlist.ManagerRole, sut.fetchTxAllowListRole(t, nonAdmin, rpc.FinalizedBlockNumber))
}

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

	now := allowListTestStartTime(t)
	disableTime := now.Add(saeparams.Tau)
	reenableTime := disableTime.Add(2 * saeparams.Tau)

	sut := newSUT(
		t,
		withFork(upgradetest.Helicon),
		withNumAccounts(2),
		withNow(&now),
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
	require.Equal(t, allowlist.AdminRole, sut.fetchDeployerAllowListRole(t, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchDeployerAllowListRole(t, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchDeployerAllowListRole(t, admin, rpc.FinalizedBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchDeployerAllowListRole(t, nonAdmin, rpc.FinalizedBlockNumber))

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
	require.Equal(t, allowlist.NoRole, sut.fetchDeployerAllowListRole(t, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchDeployerAllowListRole(t, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchDeployerAllowListRole(t, admin, rpc.FinalizedBlockNumber),
		"finalized state must still show admin role until disable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchDeployerAllowListRole(t, nonAdmin, rpc.FinalizedBlockNumber))

	// Allow the disable activation to settle so finalized catches up.
	sut.advanceTime(t, saeparams.Tau+time.Second)
	settleTx := sut.sendTransferTx(t, adminIdx, nonAdminIdx, common.Big1)
	block = sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, settleTx.Hash(), block.Transactions()[0].Hash())
	require.Equal(t, allowlist.NoRole, sut.fetchDeployerAllowListRole(t, admin, rpc.FinalizedBlockNumber),
		"finalized must reflect disable once it has settled")
	require.Equal(t, allowlist.NoRole, sut.fetchDeployerAllowListRole(t, nonAdmin, rpc.FinalizedBlockNumber))

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
	require.Equal(t, allowlist.AdminRole, sut.fetchDeployerAllowListRole(t, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.ManagerRole, sut.fetchDeployerAllowListRole(t, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchDeployerAllowListRole(t, admin, rpc.FinalizedBlockNumber),
		"finalized state must still reflect disabled allowlist until re-enable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchDeployerAllowListRole(t, nonAdmin, rpc.FinalizedBlockNumber))
}

// requireBlockContainsTxs asserts that `block` contains exactly the supplied
// tx hashes, ignoring order.
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

// requireDeploySucceeded fetches `tx`'s receipt and asserts a successful
// contract creation (status=1; deployed code present at receipt's contract
// address).
func (s *SUT) requireDeploySucceeded(t *testing.T, tx *types.Transaction) {
	t.Helper()
	require.Nil(t, tx.To(), "test setup: tx must be a contract creation")

	receipt, err := s.client.TransactionReceipt(s.ctx, tx.Hash())
	require.NoErrorf(t, err, "TransactionReceipt(%#x)", tx.Hash())
	require.Equalf(t, types.ReceiptStatusSuccessful, receipt.Status,
		"deploy %#x must succeed (status=1) when sender is allow-listed", tx.Hash())
	require.NotEqualf(t, (common.Address{}), receipt.ContractAddress,
		"successful deploy must populate ContractAddress")

	code, err := s.client.CodeAt(s.ctx, receipt.ContractAddress, nil)
	require.NoErrorf(t, err, "CodeAt(%s)", receipt.ContractAddress)
	require.NotEmptyf(t, code, "successful deploy at %s must leave code on chain", receipt.ContractAddress)
}

// requireDeployFailed fetches `tx`'s receipt and asserts a failed contract
// creation (status=0; no code at the derived deploy address).
//
// `CanCreateContract` returns its error as `vmerr` inside the EVM, which
// surfaces as a failed receipt rather than excluding the tx from the block.
// See the design note on [hook.Points.CanExecuteTransaction].
func (s *SUT) requireDeployFailed(t *testing.T, tx *types.Transaction) {
	t.Helper()
	require.Nil(t, tx.To(), "test setup: tx must be a contract creation")

	receipt, err := s.client.TransactionReceipt(s.ctx, tx.Hash())
	require.NoErrorf(t, err, "TransactionReceipt(%#x)", tx.Hash())
	require.Equalf(t, types.ReceiptStatusFailed, receipt.Status,
		"deploy %#x must revert (status=0) when sender is not allow-listed", tx.Hash())
	// receipt.ContractAddress is still derived from (sender, nonce) even
	// for failed deploys; assert the address has no code.
	code, err := s.client.CodeAt(s.ctx, receipt.ContractAddress, nil)
	require.NoErrorf(t, err, "CodeAt(%s)", receipt.ContractAddress)
	require.Emptyf(t, code, "failed deploy must leave no code at %s", receipt.ContractAddress)
}

// allowListTestStartTime returns a deterministic start time well past the
// Helicon activation, used by both [TestTxAllowListPrecompileUpgradesSAE] and
// [TestDeployerAllowListPrecompileUpgradesSAE] so their pre-Tau windows do not
// dip below the activation timestamp.
func allowListTestStartTime(t *testing.T) time.Time {
	t.Helper()

	networkUpgrades := extras.GetNetworkUpgrades(upgradetest.GetConfig(upgradetest.Helicon))
	require.NotNil(t, networkUpgrades.HeliconTimestamp)
	return time.Unix(int64(*networkUpgrades.HeliconTimestamp), 0).Add(saeparams.Tau)
}
